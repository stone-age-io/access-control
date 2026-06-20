// Package i2c drives portal lock relays and DPS/REX door inputs that hang off an
// MCP23017 I2C GPIO expander, the topology used by the KinCony Pi5R8 (Raspberry
// Pi CM5). It resolves a portal's logical relay/input indices through the
// controller model's hardware.Profile to (bus, addr, pin) descriptors, the same
// way internal/drivers/gpio resolves them to GPIO lines.
//
// Inputs are read by polling: the MCP23017 has no edge delivery to the host
// without wiring its INT line to a GPIO, so a single goroutine samples the input
// ports on a fixed interval and emits a drivers.InputEvent on each change. The
// bus is accessed through the pure-Go periph.io stack (no cgo), and all chip
// access is funneled through a small Bus interface so the register logic is
// unit-testable against an in-memory fake.
//
// Like the GPIO backend it is effectively Linux-only at runtime: opening
// /dev/i2c-N succeeds only on a Linux host with the bus enabled. It compiles on
// other platforms (periph is cross-platform) but New fails when no bus is found.
package i2c

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	periphi2c "periph.io/x/conn/v3/i2c"
	"periph.io/x/conn/v3/i2c/i2creg"
	"periph.io/x/host/v3"

	"github.com/stone-age-io/access-control/internal/drivers"
	"github.com/stone-age-io/access-control/internal/drivers/hardware"
	"github.com/stone-age-io/access-control/internal/logger"
)

const (
	// defaultPulseSeconds holds the strike when a portal's pulse value is unset.
	defaultPulseSeconds = 5
	// pollInterval is how often the input ports are sampled for DPS/REX/aux edges.
	// 50ms keeps request-to-exit and forced-open detection responsive while staying
	// well within the MCP23017's I2C throughput; KinCony's reference flow polls at
	// 100-200ms.
	pollInterval = 50 * time.Millisecond
	// inputQueue buffers door-input events between the poll goroutine and the
	// runtime's consume loop.
	inputQueue = 64
)

// Bus is the minimal I2C surface the MCP23017 driver needs: single-byte register
// reads and writes against a device address. It abstracts periph.io so the chip
// register logic is unit-testable against an in-memory fake.
type Bus interface {
	WriteReg(addr uint16, reg, val byte) error
	ReadReg(addr uint16, reg byte) (byte, error)
	Close() error
}

// periphBus adapts a periph.io I2C bus to Bus. A single mutex serializes every
// transaction: periph buses are not safe for concurrent use, and serializing also
// keeps the shadow-register writes from different chips on the bus ordered.
type periphBus struct {
	mu  sync.Mutex
	bus periphi2c.BusCloser
}

func openPeriphBus(name string) (Bus, error) {
	if _, err := host.Init(); err != nil {
		return nil, fmt.Errorf("periph host init: %w", err)
	}
	b, err := i2creg.Open(name)
	if err != nil {
		return nil, fmt.Errorf("open i2c bus %q: %w", name, err)
	}
	return &periphBus{bus: b}, nil
}

func (p *periphBus) WriteReg(addr uint16, reg, val byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	d := &periphi2c.Dev{Bus: p.bus, Addr: addr}
	return d.Tx([]byte{reg, val}, nil)
}

func (p *periphBus) ReadReg(addr uint16, reg byte) (byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	d := &periphi2c.Dev{Bus: p.bus, Addr: addr}
	r := make([]byte, 1)
	if err := d.Tx([]byte{reg}, r); err != nil {
		return 0, err
	}
	return r[0], nil
}

func (p *periphBus) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.bus.Close()
}

// inputKey identifies one physical input pin on the bus (addr + expander pin).
type inputKey struct {
	addr uint16
	pin  int
}

// inputBinding is a polled input: which chip/pin to read and which portal/kind to
// report it as. last/seen track the poll-diff state (seen=false until the first
// sample establishes the baseline).
type inputBinding struct {
	chip *mcp23017
	pin  int
	code string
	kind string // drivers.InputDPS / InputREX / InputAux
	last bool
	seen bool
}

// portLines is the set of lines armed for one portal (or aux point): its lock
// relay (nil for an aux input) and the input keys to drop on disarm.
type portLines struct {
	lock   *i2cLock
	inputs []inputKey
}

// Hardware is the MCP23017 backend. It arms per-portal lock/inputs against the
// controller model's profile and is the runtime's DoorInput source, satisfying
// controller.PortalHardware (Arm/Disarm), controller.AuxHardware (aux out/in),
// and drivers.DoorInput (Inputs).
type Hardware struct {
	profile hardware.Profile
	log     *logger.Logger
	bus     Bus
	ch      chan drivers.InputEvent

	mu      sync.Mutex
	chips   map[uint16]*mcp23017
	ports   map[string]*portLines
	inputs  map[inputKey]*inputBinding
	closed  bool
	started bool

	stop chan struct{}
	done chan struct{}
}

// New opens the I2C bus the model's lines share and starts the input poller. The
// profile must be homogeneously I2C and reference a single bus.
func New(profile hardware.Profile, log *logger.Logger) (*Hardware, error) {
	busNum, err := profileBus(profile)
	if err != nil {
		return nil, err
	}
	bus, err := openPeriphBus(strconv.Itoa(busNum))
	if err != nil {
		return nil, err
	}
	h := newHardware(profile, bus, log)
	h.startPolling()
	return h, nil
}

// newHardware builds the backend around an injected bus (periph in production, a
// fake in tests) without starting the poll goroutine — tests drive pollOnce
// directly. Call startPolling to begin sampling inputs on the interval.
func newHardware(profile hardware.Profile, bus Bus, log *logger.Logger) *Hardware {
	return &Hardware{
		profile: profile,
		log:     log.With("component", "i2c", "model", profile.Model),
		bus:     bus,
		ch:      make(chan drivers.InputEvent, inputQueue),
		chips:   make(map[uint16]*mcp23017),
		ports:   make(map[string]*portLines),
		inputs:  make(map[inputKey]*inputBinding),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
}

// startPolling launches the input poll goroutine.
func (h *Hardware) startPolling() {
	h.started = true
	go h.pollLoop()
}

// profileBus returns the single I2C bus number the profile's lines share, or an
// error if the profile has a non-I2C line or spans multiple buses.
func profileBus(p hardware.Profile) (int, error) {
	bus := -1
	for _, s := range p.Lines() {
		if s.Backend != hardware.BackendI2C {
			return 0, fmt.Errorf("i2c driver: model %q has a non-I2C line (backend %q)", p.Model, s.Backend)
		}
		switch {
		case bus == -1:
			bus = s.Bus
		case bus != s.Bus:
			return 0, fmt.Errorf("i2c driver: model %q spans multiple I2C buses (%d and %d)", p.Model, bus, s.Bus)
		}
	}
	if bus == -1 {
		return 0, fmt.Errorf("i2c driver: model %q defines no lines", p.Model)
	}
	return bus, nil
}

// Inputs implements drivers.DoorInput.
func (h *Hardware) Inputs() <-chan drivers.InputEvent { return h.ch }

// chipFor returns the chip at addr, creating it on first use. Caller holds h.mu.
func (h *Hardware) chipFor(addr uint16) *mcp23017 {
	c, ok := h.chips[addr]
	if !ok {
		c = newChip(h.bus, addr)
		h.chips[addr] = c
	}
	return c
}

// Arm requests the lock relay (required) and DPS/REX input lines (optional) for a
// portal and returns the lock driver. On any partial failure it releases what it
// had already configured, so a retry on the next policy change starts clean.
func (h *Hardware) Arm(code string, lockRelay, dpsInput, rexInput int) (drivers.LockDriver, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, fmt.Errorf("i2c hardware closed")
	}
	if _, ok := h.ports[code]; ok {
		return nil, fmt.Errorf("portal %q already armed", code)
	}

	relaySpec, ok := h.profile.Relay(lockRelay)
	if !ok {
		return nil, fmt.Errorf("portal %q: relay index %d not defined for model %q", code, lockRelay, h.profile.Model)
	}
	lock, err := h.newLock(relaySpec)
	if err != nil {
		return nil, fmt.Errorf("portal %q: arm relay %d: %w", code, lockRelay, err)
	}
	p := &portLines{lock: lock}

	if dpsInput > 0 {
		key, err := h.armInput(code, drivers.InputDPS, dpsInput)
		if err != nil {
			h.release(p)
			return nil, fmt.Errorf("portal %q: arm DPS input %d: %w", code, dpsInput, err)
		}
		p.inputs = append(p.inputs, key)
	}
	if rexInput > 0 {
		key, err := h.armInput(code, drivers.InputREX, rexInput)
		if err != nil {
			h.release(p)
			return nil, fmt.Errorf("portal %q: arm REX input %d: %w", code, rexInput, err)
		}
		p.inputs = append(p.inputs, key)
	}

	h.ports[code] = p
	h.log.Info("portal hardware armed", "portal", code, "relay", lockRelay, "dps", dpsInput, "rex", rexInput)
	return lock, nil
}

// Disarm releases every line requested for a portal. Unknown portals are a no-op.
func (h *Hardware) Disarm(code string) { h.disarmKey(code, "portal") }

// ArmOutput requests a relay line for an aux output and returns its lock driver.
// Keyed under an "auxout:" namespace so an aux code can't collide with a portal.
func (h *Hardware) ArmOutput(code string, relayIndex int) (drivers.LockDriver, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, fmt.Errorf("i2c hardware closed")
	}
	key := "auxout:" + code
	if _, ok := h.ports[key]; ok {
		return nil, fmt.Errorf("aux output %q already armed", code)
	}
	spec, ok := h.profile.Relay(relayIndex)
	if !ok {
		return nil, fmt.Errorf("aux output %q: relay index %d not defined for model %q", code, relayIndex, h.profile.Model)
	}
	lock, err := h.newLock(spec)
	if err != nil {
		return nil, fmt.Errorf("aux output %q: arm relay %d: %w", code, relayIndex, err)
	}
	h.ports[key] = &portLines{lock: lock}
	h.log.Info("aux output hardware armed", "code", code, "relay", relayIndex)
	return lock, nil
}

// DisarmOutput releases an aux output's relay line.
func (h *Hardware) DisarmOutput(code string) { h.disarmKey("auxout:"+code, "aux-output") }

// ArmInput requests an input line for an aux input; its edges flow into the same
// Inputs() channel with kind InputAux.
func (h *Hardware) ArmInput(code string, inputIndex int) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return fmt.Errorf("i2c hardware closed")
	}
	key := "auxin:" + code
	if _, ok := h.ports[key]; ok {
		return fmt.Errorf("aux input %q already armed", code)
	}
	k, err := h.armInput(code, drivers.InputAux, inputIndex)
	if err != nil {
		return fmt.Errorf("aux input %q: arm input %d: %w", code, inputIndex, err)
	}
	h.ports[key] = &portLines{inputs: []inputKey{k}}
	h.log.Info("aux input hardware armed", "code", code, "input", inputIndex)
	return nil
}

// DisarmInput releases an aux input's line.
func (h *Hardware) DisarmInput(code string) { h.disarmKey("auxin:"+code, "aux-input") }

// newLock configures a relay line as a de-energized output and returns its lock.
// Caller holds h.mu.
func (h *Hardware) newLock(spec hardware.LineSpec) (*i2cLock, error) {
	if spec.Backend != hardware.BackendI2C {
		return nil, fmt.Errorf("relay backend %q not supported by the I2C driver", spec.Backend)
	}
	chip := h.chipFor(uint16(spec.Addr))
	if err := chip.configureOutput(spec.Pin, spec.ActiveLow); err != nil {
		return nil, err
	}
	return &i2cLock{chip: chip, pin: spec.Pin, activeLow: spec.ActiveLow, log: h.log}, nil
}

// armInput configures one input line (pull-up + active-low) and registers it for
// polling. Caller holds h.mu.
func (h *Hardware) armInput(code, kind string, idx int) (inputKey, error) {
	spec, ok := h.profile.Input(idx)
	if !ok {
		return inputKey{}, fmt.Errorf("input index %d not defined for model %q", idx, h.profile.Model)
	}
	if spec.Backend != hardware.BackendI2C {
		return inputKey{}, fmt.Errorf("input backend %q not supported by the I2C driver", spec.Backend)
	}
	chip := h.chipFor(uint16(spec.Addr))
	if err := chip.configureInput(spec.Pin, true, spec.ActiveLow); err != nil {
		return inputKey{}, err
	}
	k := inputKey{addr: uint16(spec.Addr), pin: spec.Pin}
	h.inputs[k] = &inputBinding{chip: chip, pin: spec.Pin, code: code, kind: kind}
	return k, nil
}

// disarmKey releases the lines held under one ports key. Unknown keys are a no-op.
func (h *Hardware) disarmKey(key, what string) {
	h.mu.Lock()
	p := h.ports[key]
	delete(h.ports, key)
	if p != nil {
		h.release(p)
	}
	h.mu.Unlock()
	if p != nil {
		h.log.Info(what+" hardware disarmed", "key", key)
	}
}

// release de-energizes a port's relay and stops polling its inputs. Caller holds
// h.mu. Safe on a partially-armed port (nil lock skipped).
func (h *Hardware) release(p *portLines) {
	if p.lock != nil {
		p.lock.Close()
	}
	for _, k := range p.inputs {
		delete(h.inputs, k)
	}
}

// Close stops the poller, releases all lines (relays de-energized), and closes the
// bus. Stopping the poller before closing the channel guarantees no send races the
// close.
func (h *Hardware) Close() error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	ports := h.ports
	h.ports = make(map[string]*portLines)
	h.inputs = make(map[inputKey]*inputBinding)
	h.mu.Unlock()

	close(h.stop)
	if h.started {
		<-h.done // wait for the poll goroutine to exit before touching the channel/bus
	}

	for _, p := range ports {
		if p.lock != nil {
			p.lock.Close()
		}
	}
	close(h.ch)
	return h.bus.Close()
}

// pollLoop samples the input ports on a fixed interval until Close.
func (h *Hardware) pollLoop() {
	defer close(h.done)
	t := time.NewTicker(pollInterval)
	defer t.Stop()
	for {
		select {
		case <-h.stop:
			return
		case <-t.C:
			h.pollOnce()
		}
	}
}

// pollOnce reads every chip/port that has at least one armed input and emits an
// event for each input whose active state changed since the last sample. The
// first sample establishes a baseline silently (matching the GPIO backend's
// edge-only contract — the runtime seeds its own initial DPS/aux state on arm, so
// emitting the baseline would risk a spurious startup alarm).
func (h *Hardware) pollOnce() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	bindings := make([]*inputBinding, 0, len(h.inputs))
	for _, b := range h.inputs {
		bindings = append(bindings, b)
	}
	h.mu.Unlock()
	if len(bindings) == 0 {
		return
	}

	// Read each (chip, port) at most once per tick.
	type portKey struct {
		chip *mcp23017
		port int
	}
	vals := make(map[portKey]byte)
	failed := make(map[portKey]bool)
	for _, b := range bindings {
		port, bit := portBit(b.pin)
		pk := portKey{b.chip, port}
		if failed[pk] {
			continue
		}
		val, ok := vals[pk]
		if !ok {
			v, err := b.chip.readPort(port)
			if err != nil {
				failed[pk] = true
				h.log.Warn("i2c input read failed", "addr", b.chip.addr, "port", port, "error", err)
				continue
			}
			vals[pk] = v
			val = v
		}
		h.emitIfChanged(b, getBit(val, bit))
	}
}

// emitIfChanged updates a binding's poll state under the lock and emits an event
// if its active state changed (after the first, baseline-establishing sample). It
// re-checks that the binding is still armed, so a disarm mid-poll can't emit a
// stale event.
func (h *Hardware) emitIfChanged(b *inputBinding, active bool) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	if cur, ok := h.inputs[inputKey{b.chip.addr, b.pin}]; !ok || cur != b {
		h.mu.Unlock()
		return
	}
	first := !b.seen
	prev := b.last
	b.seen = true
	b.last = active
	code, kind := b.code, b.kind
	h.mu.Unlock()

	if first || active == prev {
		return
	}

	e := drivers.InputEvent{Portal: code, Kind: kind, At: time.Now().UTC()}
	switch kind {
	case drivers.InputDPS:
		e.Closed = active // DPS active = contact closed = door shut
	case drivers.InputREX:
		e.Active = active // REX active = egress pressed
	case drivers.InputAux:
		e.Active = active // aux active = observed line asserted
	}
	select {
	case h.ch <- e:
	default:
		h.log.Warn("door-input queue full; dropping event", "portal", code, "kind", kind)
	}
}
