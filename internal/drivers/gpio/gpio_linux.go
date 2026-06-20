//go:build linux

package gpio

import (
	"fmt"
	"sync"
	"time"

	gpiocdev "github.com/warthog618/go-gpiocdev"

	"github.com/stone-age-io/access-control/internal/drivers"
	"github.com/stone-age-io/access-control/internal/drivers/hardware"
	"github.com/stone-age-io/access-control/internal/logger"
)

const (
	// defaultPulseSeconds holds the strike when a portal's pulse value is unset.
	defaultPulseSeconds = 5
	// inputDebounce is the kernel debounce applied to DPS/REX lines (mechanical
	// contacts bounce; the kernel coalesces transitions within this window).
	inputDebounce = 5 * time.Millisecond
	// inputQueue buffers door-input events between the gpiocdev handler goroutine
	// and the runtime's consume loop.
	inputQueue = 64
)

// Hardware is the GPIO backend: it arms per-portal lock/inputs against the
// controller model's profile and is the runtime's DoorInput source. It satisfies
// both controller.PortalHardware (Arm/Disarm) and drivers.DoorInput (Inputs).
type Hardware struct {
	profile hardware.Profile
	log     *logger.Logger
	ch      chan drivers.InputEvent

	mu     sync.Mutex
	ports  map[string]*portLines
	closed bool
}

// portLines is the set of lines requested for one portal (or one aux point). For
// an aux output only lock is set; for an aux input only aux is set.
type portLines struct {
	lock *gpioLock
	dps  *gpiocdev.Line
	rex  *gpiocdev.Line
	aux  *gpiocdev.Line
}

// New creates a GPIO hardware backend for the given model profile. It allocates
// no lines until Arm, so it succeeds even before any gpiochip is touched.
func New(profile hardware.Profile, log *logger.Logger) (Driver, error) {
	return &Hardware{
		profile: profile,
		log:     log.With("component", "gpio", "model", profile.Model),
		ch:      make(chan drivers.InputEvent, inputQueue),
		ports:   make(map[string]*portLines),
	}, nil
}

// Inputs implements drivers.DoorInput.
func (h *Hardware) Inputs() <-chan drivers.InputEvent { return h.ch }

// Arm requests the lock relay (required) and DPS/REX input lines (optional) for a
// portal and returns the lock driver. On any partial failure it releases what it
// had already requested, so a retry on the next policy change starts clean.
func (h *Hardware) Arm(code string, lockRelay, dpsInput, rexInput int) (drivers.LockDriver, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, fmt.Errorf("gpio hardware closed")
	}
	if _, ok := h.ports[code]; ok {
		return nil, fmt.Errorf("portal %q already armed", code)
	}

	relaySpec, ok := h.profile.Relay(lockRelay)
	if !ok {
		return nil, fmt.Errorf("portal %q: relay index %d not defined for model %q", code, lockRelay, h.profile.Model)
	}
	lock, err := h.newLock(code, relaySpec)
	if err != nil {
		return nil, fmt.Errorf("portal %q: arm relay %d: %w", code, lockRelay, err)
	}
	p := &portLines{lock: lock}

	if dpsInput > 0 {
		line, err := h.armInput(code, drivers.InputDPS, dpsInput)
		if err != nil {
			h.release(p)
			return nil, fmt.Errorf("portal %q: arm DPS input %d: %w", code, dpsInput, err)
		}
		p.dps = line
	}
	if rexInput > 0 {
		line, err := h.armInput(code, drivers.InputREX, rexInput)
		if err != nil {
			h.release(p)
			return nil, fmt.Errorf("portal %q: arm REX input %d: %w", code, rexInput, err)
		}
		p.rex = line
	}

	h.ports[code] = p
	h.log.Info("portal hardware armed", "portal", code, "relay", lockRelay, "dps", dpsInput, "rex", rexInput)
	return lock, nil
}

// Disarm releases every line requested for a portal. Unknown portals are a no-op.
func (h *Hardware) Disarm(code string) {
	h.disarmKey(code, "portal")
}

// ArmOutput requests a relay line for an aux output and returns its lock driver.
// Keyed under an "auxout:" namespace so an aux code can't collide with a portal.
func (h *Hardware) ArmOutput(code string, relayIndex int) (drivers.LockDriver, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, fmt.Errorf("gpio hardware closed")
	}
	key := "auxout:" + code
	if _, ok := h.ports[key]; ok {
		return nil, fmt.Errorf("aux output %q already armed", code)
	}
	spec, ok := h.profile.Relay(relayIndex)
	if !ok {
		return nil, fmt.Errorf("aux output %q: relay index %d not defined for model %q", code, relayIndex, h.profile.Model)
	}
	lock, err := h.newLock(code, spec)
	if err != nil {
		return nil, fmt.Errorf("aux output %q: arm relay %d: %w", code, relayIndex, err)
	}
	h.ports[key] = &portLines{lock: lock}
	h.log.Info("aux output hardware armed", "code", code, "relay", relayIndex)
	return lock, nil
}

// DisarmOutput releases an aux output's relay line.
func (h *Hardware) DisarmOutput(code string) {
	h.disarmKey("auxout:"+code, "aux-output")
}

// ArmInput requests an input line for an aux input; its edges flow into the same
// Inputs() channel with kind InputAux.
func (h *Hardware) ArmInput(code string, inputIndex int) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return fmt.Errorf("gpio hardware closed")
	}
	key := "auxin:" + code
	if _, ok := h.ports[key]; ok {
		return fmt.Errorf("aux input %q already armed", code)
	}
	line, err := h.armInput(code, drivers.InputAux, inputIndex)
	if err != nil {
		return fmt.Errorf("aux input %q: arm input %d: %w", code, inputIndex, err)
	}
	h.ports[key] = &portLines{aux: line}
	h.log.Info("aux input hardware armed", "code", code, "input", inputIndex)
	return nil
}

// DisarmInput releases an aux input's line.
func (h *Hardware) DisarmInput(code string) {
	h.disarmKey("auxin:"+code, "aux-input")
}

// disarmKey releases the lines held under one ports key. Unknown keys are a no-op.
func (h *Hardware) disarmKey(key, what string) {
	h.mu.Lock()
	p := h.ports[key]
	delete(h.ports, key)
	h.mu.Unlock()
	if p == nil {
		return
	}
	h.release(p)
	h.log.Info(what+" hardware disarmed", "key", key)
}

// Close releases all lines and closes the input stream. Releasing the input lines
// first guarantees no edge handler is in flight, so closing the channel can never
// race a send.
func (h *Hardware) Close() error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	ports := h.ports
	h.ports = make(map[string]*portLines)
	h.mu.Unlock()

	for _, p := range ports {
		h.release(p)
	}
	close(h.ch)
	return nil
}

// release closes whatever lines a port holds. Safe to call on a partially-armed
// port (nil lines are skipped). Edge handlers use a non-blocking send, so a line
// Close (which waits for the handler to return) never deadlocks.
func (h *Hardware) release(p *portLines) {
	if p.lock != nil {
		p.lock.Close()
	}
	if p.dps != nil {
		_ = p.dps.Close()
	}
	if p.rex != nil {
		_ = p.rex.Close()
	}
	if p.aux != nil {
		_ = p.aux.Close()
	}
}

// armInput requests one DPS/REX input line with edge detection feeding onEdge.
func (h *Hardware) armInput(code, kind string, idx int) (*gpiocdev.Line, error) {
	spec, ok := h.profile.Input(idx)
	if !ok {
		return nil, fmt.Errorf("input index %d not defined for model %q", idx, h.profile.Model)
	}
	if spec.Backend != hardware.BackendGPIO {
		return nil, fmt.Errorf("input backend %q not supported by the GPIO driver", spec.Backend)
	}
	opts := []gpiocdev.LineReqOption{
		gpiocdev.WithConsumer("stone-access:" + code + ":" + kind),
		gpiocdev.AsInput,
		gpiocdev.WithPullUp,
		gpiocdev.WithBothEdges,
		gpiocdev.WithDebounce(inputDebounce),
		gpiocdev.WithEventHandler(h.onEdge(code, kind)),
	}
	if spec.ActiveLow {
		opts = append(opts, gpiocdev.AsActiveLow)
	}
	return gpiocdev.RequestLine(spec.Chip, spec.Offset, opts...)
}

// onEdge turns a line edge into a door InputEvent. With AsActiveLow already
// applied per the line spec, a rising edge is an inactive→active transition
// (contact made / button pressed). At is stamped with wall-clock now because the
// runtime compares it against wall-clock grant/REX windows (the gpiocdev event
// timestamp is monotonic).
func (h *Hardware) onEdge(code, kind string) gpiocdev.EventHandler {
	return func(ev gpiocdev.LineEvent) {
		active := ev.Type == gpiocdev.LineEventRisingEdge
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
}

// gpioLock energizes a relay line for a portal. The line is energized while a
// momentary Pulse is in flight OR a standing SetHeld is set — Pulse's one-shot
// timer drops back to the held state, not unconditionally off, so the two
// compose (a tap during a scheduled-unlock window pulses harmlessly). Both are
// hardware-local strike timing, not a policy ticker. AsActiveLow (set at request
// time per the line spec) means SetValue(1) energizes regardless of wiring
// polarity.
type gpioLock struct {
	code string
	line *gpiocdev.Line
	log  *logger.Logger

	mu     sync.Mutex
	timer  *time.Timer
	held   bool // standing hold (posture unlocked / auto-unlock)
	closed bool
}

func (h *Hardware) newLock(code string, spec hardware.LineSpec) (*gpioLock, error) {
	if spec.Backend != hardware.BackendGPIO {
		return nil, fmt.Errorf("relay backend %q not supported by the GPIO driver", spec.Backend)
	}
	opts := []gpiocdev.LineReqOption{
		gpiocdev.WithConsumer("stone-access:" + code + ":lock"),
		gpiocdev.AsOutput(0), // de-energized initial state (0 = inactive)
	}
	if spec.ActiveLow {
		opts = append(opts, gpiocdev.AsActiveLow)
	}
	line, err := gpiocdev.RequestLine(spec.Chip, spec.Offset, opts...)
	if err != nil {
		return nil, err
	}
	return &gpioLock{code: code, line: line, log: h.log}, nil
}

// Pulse implements drivers.LockDriver: energize the relay, then release it after
// seconds. A non-positive seconds uses defaultPulseSeconds.
func (l *gpioLock) Pulse(seconds int) error {
	if seconds <= 0 {
		seconds = defaultPulseSeconds
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return fmt.Errorf("lock for %q is closed", l.code)
	}
	if l.timer != nil {
		l.timer.Stop()
	}
	if err := l.line.SetValue(1); err != nil { // 1 = active = energized
		return err
	}
	l.timer = time.AfterFunc(time.Duration(seconds)*time.Second, func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.closed {
			return
		}
		// Drop back to the standing hold state, not unconditionally off: a pulse
		// over a held-open door must leave the door held when it expires.
		if err := l.line.SetValue(boolToVal(l.held)); err != nil {
			l.log.Error("failed to de-energize relay", "portal", l.code, "error", err)
		}
		l.timer = nil
	})
	return nil
}

// SetHeld implements drivers.LockDriver: set or clear the standing hold. Energizes
// immediately when held; when released, de-energizes unless a pulse is still in
// flight (that pulse's timer will then drop to the now-false held state).
// Idempotent.
func (l *gpioLock) SetHeld(held bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return fmt.Errorf("lock for %q is closed", l.code)
	}
	if l.held == held {
		return nil
	}
	l.held = held
	if held {
		return l.line.SetValue(1)
	}
	if l.timer != nil {
		return nil // a momentary pulse is still energizing the line; let it expire
	}
	return l.line.SetValue(0)
}

// boolToVal maps a hold/energize flag to a GPIO line value (1 = active).
func boolToVal(on bool) int {
	if on {
		return 1
	}
	return 0
}

// Close stops any pending release, de-energizes, and releases the line.
func (l *gpioLock) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	l.closed = true
	if l.timer != nil {
		l.timer.Stop()
		l.timer = nil
	}
	_ = l.line.SetValue(0) // de-energize on release (fail-safe)
	_ = l.line.Close()
}
