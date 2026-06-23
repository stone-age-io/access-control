package i2c

import (
	"errors"
	"sync"
	"testing"

	"github.com/stone-age-io/access-control/internal/drivers"
	"github.com/stone-age-io/access-control/internal/drivers/hardware"
	"github.com/stone-age-io/access-control/internal/logger"
)

// fakeBus is an in-memory I2C bus: writes land in a register map (and a write
// log, for ordering checks); reads return whatever was last written, so a test
// can seed an input register to simulate pin levels. Injectable errors exercise
// the fail-safe paths.
type fakeBus struct {
	mu       sync.Mutex
	reg      map[uint32]byte
	writes   []byte // registers written, in order
	readErr  error
	writeErr error
	closed   bool
}

func newFakeBus() *fakeBus { return &fakeBus{reg: make(map[uint32]byte)} }

func rkey(addr uint16, reg byte) uint32 { return uint32(addr)<<8 | uint32(reg) }

func (f *fakeBus) WriteReg(addr uint16, reg, val byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.writeErr != nil {
		return f.writeErr
	}
	f.reg[rkey(addr, reg)] = val
	f.writes = append(f.writes, reg)
	return nil
}

func (f *fakeBus) ReadReg(addr uint16, reg byte) (byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readErr != nil {
		return 0, f.readErr
	}
	return f.reg[rkey(addr, reg)], nil
}

func (f *fakeBus) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeBus) get(addr uint16, reg byte) byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reg[rkey(addr, reg)]
}

func (f *fakeBus) setPins(addr uint16, reg, val byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reg[rkey(addr, reg)] = val
}

func (f *fakeBus) writeOrder() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]byte(nil), f.writes...)
}

func drain(ch <-chan drivers.InputEvent) []drivers.InputEvent {
	var out []drivers.InputEvent
	for {
		select {
		case e := <-ch:
			out = append(out, e)
		default:
			return out
		}
	}
}

// --- chip register layer ---

// configureOutput must drive the latch to de-energized BEFORE flipping the pin to
// an output, so a relay never glitches energized during arming.
func TestConfigureOutputNoGlitch(t *testing.T) {
	f := newFakeBus()
	c := newChip(f, 0x20)
	if err := c.configureOutput(8, false); err != nil { // port B, bit 0, active-high
		t.Fatalf("configureOutput: %v", err)
	}
	order := f.writeOrder()
	if len(order) != 2 || order[0] != regOLATB || order[1] != regIODIRB {
		t.Fatalf("write order = %v, want [OLATB, IODIRB]", order)
	}
	if got := f.get(0x20, regOLATB); got&0x01 != 0 {
		t.Errorf("OLATB bit0 = 1, want 0 (de-energized, active-high)")
	}
	if got := f.get(0x20, regIODIRB); got&0x01 != 0 {
		t.Errorf("IODIRB bit0 = 1 (input), want 0 (output)")
	}
}

func TestConfigureInputRegisters(t *testing.T) {
	f := newFakeBus()
	c := newChip(f, 0x20)
	if err := c.configureInput(0, true, true); err != nil { // port A, bit 0, pull-up, active-low
		t.Fatalf("configureInput: %v", err)
	}
	if f.get(0x20, regGPPUA)&0x01 == 0 {
		t.Errorf("GPPUA bit0 not set (pull-up)")
	}
	if f.get(0x20, regIPOLA)&0x01 == 0 {
		t.Errorf("IPOLA bit0 not set (active-low invert)")
	}
	if f.get(0x20, regIODIRA)&0x01 == 0 {
		t.Errorf("IODIRA bit0 = 0 (output), want 1 (input)")
	}
}

func TestWriteOutputPolarity(t *testing.T) {
	f := newFakeBus()
	c := newChip(f, 0x20)
	c.writeOutput(8, true, false) // active-high energize -> high
	if f.get(0x20, regOLATB)&0x01 == 0 {
		t.Errorf("active-high energize: OLATB bit0 not high")
	}
	c.writeOutput(8, false, false)
	if f.get(0x20, regOLATB)&0x01 != 0 {
		t.Errorf("active-high de-energize: OLATB bit0 not low")
	}
	c.writeOutput(9, true, true) // active-low energize -> low
	if f.get(0x20, regOLATB)&0x02 != 0 {
		t.Errorf("active-low energize: OLATB bit1 not low")
	}
	c.writeOutput(9, false, true)
	if f.get(0x20, regOLATB)&0x02 == 0 {
		t.Errorf("active-low de-energize: OLATB bit1 not high")
	}
}

// Relays sharing a port write the same OLAT byte: the shadow register must
// preserve the other relays' bits on each single-relay change.
func TestSharedRelayRegister(t *testing.T) {
	f := newFakeBus()
	c := newChip(f, 0x20)
	c.writeOutput(8, true, false)
	c.writeOutput(9, true, false)
	if got := f.get(0x20, regOLATB); got&0x03 != 0x03 {
		t.Errorf("OLATB = %#02x, want bit0 and bit1 both set", got)
	}
	c.writeOutput(8, false, false)
	if got := f.get(0x20, regOLATB); got&0x03 != 0x02 {
		t.Errorf("OLATB = %#02x, want only bit1 set after clearing bit0", got)
	}
}

// --- hardware backend ---

func TestProfileBus(t *testing.T) {
	p, _ := hardware.ProfileFor("kincony-pi5r8")
	bus, err := profileBus(p)
	if err != nil || bus != 1 {
		t.Fatalf("pi5r8 bus = %d, err = %v; want 1, nil", bus, err)
	}
	g, _ := hardware.ProfileFor("kincony-server-mini")
	if _, err := profileBus(g); err == nil {
		t.Error("profileBus accepted a non-I2C (GPIO) model")
	}
}

func TestArmConfiguresAndEnergizes(t *testing.T) {
	p, _ := hardware.ProfileFor("kincony-pi5r8")
	f := newFakeBus()
	h := newHardware(p, f, logger.NewNopLogger())
	defer h.Close()

	lock, err := h.Arm("d1", drivers.PortalIO{LockRelay: 1, DpsInput: 1, RexInput: 2}) // relay1=pin8, dps=in1(pin0), rex=in2(pin1)
	if err != nil {
		t.Fatalf("Arm: %v", err)
	}
	if f.get(0x20, regIODIRB)&0x01 != 0 {
		t.Errorf("relay pin not configured as output")
	}
	if f.get(0x20, regOLATB)&0x01 != 0 {
		t.Errorf("relay energized immediately after arm (want de-energized)")
	}
	if f.get(0x20, regGPPUA)&0x03 != 0x03 {
		t.Errorf("input pull-ups not enabled for pins 0,1")
	}
	if f.get(0x20, regIPOLA)&0x03 != 0x03 {
		t.Errorf("input polarity not inverted for pins 0,1")
	}

	if err := lock.Pulse(1); err != nil {
		t.Fatalf("Pulse: %v", err)
	}
	if f.get(0x20, regOLATB)&0x01 == 0 {
		t.Errorf("relay not energized after Pulse")
	}
	if err := lock.SetHeld(true); err != nil {
		t.Fatalf("SetHeld: %v", err)
	}
	if f.get(0x20, regOLATB)&0x01 == 0 {
		t.Errorf("relay not energized while held")
	}
}

func TestDisarmFailSafe(t *testing.T) {
	p, _ := hardware.ProfileFor("kincony-pi5r8")
	f := newFakeBus()
	h := newHardware(p, f, logger.NewNopLogger())
	defer h.Close()

	lock, _ := h.Arm("d1", drivers.PortalIO{LockRelay: 1, DpsInput: 1, RexInput: 2})
	_ = lock.SetHeld(true)
	if f.get(0x20, regOLATB)&0x01 == 0 {
		t.Fatal("precondition: relay should be energized")
	}
	h.Disarm("d1")
	if f.get(0x20, regOLATB)&0x01 != 0 {
		t.Errorf("relay not de-energized after Disarm")
	}
	if f.get(0x20, regIODIRB)&0x01 == 0 {
		t.Errorf("relay pin not returned to high-Z input after Disarm")
	}
	h.mu.Lock()
	n := len(h.inputs)
	h.mu.Unlock()
	if n != 0 {
		t.Errorf("inputs still polled after Disarm: %d", n)
	}
}

func TestPollEmitsOnChange(t *testing.T) {
	p, _ := hardware.ProfileFor("kincony-pi5r8")
	f := newFakeBus()
	h := newHardware(p, f, logger.NewNopLogger())
	defer h.Close()

	if _, err := h.Arm("d1", drivers.PortalIO{LockRelay: 1, DpsInput: 1, RexInput: 2}); err != nil {
		t.Fatalf("Arm: %v", err)
	}

	h.pollOnce() // baseline (GPIOA=0): all inputs inactive, no events
	if e := drain(h.Inputs()); len(e) != 0 {
		t.Fatalf("baseline poll emitted %d events, want 0", len(e))
	}

	f.setPins(0x20, regGPIOA, 0x01) // DPS (pin0) active = door shut
	h.pollOnce()
	ev := drain(h.Inputs())
	if len(ev) != 1 || ev[0].Kind != drivers.InputDPS || !ev[0].Closed || ev[0].Portal != "d1" {
		t.Fatalf("after DPS active: %+v, want one DPS Closed=true for d1", ev)
	}

	h.pollOnce() // unchanged
	if e := drain(h.Inputs()); len(e) != 0 {
		t.Fatalf("unchanged poll emitted %d events, want 0", len(e))
	}

	f.setPins(0x20, regGPIOA, 0x03) // REX (pin1) pressed, DPS still active
	h.pollOnce()
	ev = drain(h.Inputs())
	if len(ev) != 1 || ev[0].Kind != drivers.InputREX || !ev[0].Active {
		t.Fatalf("after REX press: %+v, want one REX Active=true", ev)
	}
}

func TestAuxInputEmitsActive(t *testing.T) {
	p, _ := hardware.ProfileFor("kincony-pi5r8")
	f := newFakeBus()
	h := newHardware(p, f, logger.NewNopLogger())
	defer h.Close()

	if err := h.ArmInput("aux1", 3, false); err != nil { // input3 = pin2
		t.Fatalf("ArmInput: %v", err)
	}
	h.pollOnce()
	drain(h.Inputs())

	f.setPins(0x20, regGPIOA, 0x04) // pin2
	h.pollOnce()
	ev := drain(h.Inputs())
	if len(ev) != 1 || ev[0].Kind != drivers.InputAux || !ev[0].Active || ev[0].Portal != "aux1" {
		t.Fatalf("aux events = %+v, want one InputAux Active=true for aux1", ev)
	}
}

func TestCloseDeenergizes(t *testing.T) {
	p, _ := hardware.ProfileFor("kincony-pi5r8")
	f := newFakeBus()
	h := newHardware(p, f, logger.NewNopLogger())

	lock, _ := h.Arm("d1", drivers.PortalIO{LockRelay: 1})
	_ = lock.SetHeld(true)
	if f.get(0x20, regOLATB)&0x01 == 0 {
		t.Fatal("precondition: relay energized")
	}
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if f.get(0x20, regOLATB)&0x01 != 0 {
		t.Errorf("relay not de-energized after Close")
	}
	if !f.closed {
		t.Errorf("bus not closed")
	}
}

// A bus read failure during polling must not panic or emit a spurious event
// (fail-safe: keep the previous state).
func TestPollReadFailureIsHarmless(t *testing.T) {
	p, _ := hardware.ProfileFor("kincony-pi5r8")
	f := newFakeBus()
	h := newHardware(p, f, logger.NewNopLogger())
	defer h.Close()

	if _, err := h.Arm("d1", drivers.PortalIO{LockRelay: 1, DpsInput: 1}); err != nil {
		t.Fatalf("Arm: %v", err)
	}
	h.pollOnce() // baseline ok
	drain(h.Inputs())

	f.mu.Lock()
	f.readErr = errors.New("bus error")
	f.mu.Unlock()
	h.pollOnce()
	if e := drain(h.Inputs()); len(e) != 0 {
		t.Errorf("read failure emitted events: %+v", e)
	}
}
