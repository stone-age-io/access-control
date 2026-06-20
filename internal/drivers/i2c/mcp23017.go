package i2c

import "sync"

// MCP23017 register addresses in the power-on default IOCON.BANK=0 layout, where
// the A/B port registers are interleaved by a single offset. We only touch the
// registers the access controller needs: direction, pull-up, input polarity, and
// the output latch / input register.
const (
	regIODIRA = 0x00 // I/O direction: 1 = input (power-on default 0xFF)
	regIODIRB = 0x01
	regIPOLA  = 0x02 // input polarity: 1 = read inverted
	regIPOLB  = 0x03
	regGPPUA  = 0x0C // pull-up enable: 1 = 100k pull-up on (input pins)
	regGPPUB  = 0x0D
	regGPIOA  = 0x12 // input register (reading reflects pin state, IPOL-adjusted)
	regGPIOB  = 0x13
	regOLATA  = 0x14 // output latch (writing drives output pins)
	regOLATB  = 0x15
)

// Per-port register lookups indexed by port (0 = A, 1 = B).
var (
	iodirReg = [2]byte{regIODIRA, regIODIRB}
	ipolReg  = [2]byte{regIPOLA, regIPOLB}
	gppuReg  = [2]byte{regGPPUA, regGPPUB}
	gpioReg  = [2]byte{regGPIOA, regGPIOB}
	olatReg  = [2]byte{regOLATA, regOLATB}
)

// portBit splits a 0-15 expander pin into its port (0 = A, 1 = B) and bit (0-7).
func portBit(pin int) (port, bit int) {
	if pin < 8 {
		return 0, pin
	}
	return 1, pin - 8
}

func setBit(b *byte, bit int, on bool) {
	if on {
		*b |= 1 << uint(bit)
	} else {
		*b &^= 1 << uint(bit)
	}
}

func getBit(b byte, bit int) bool { return b&(1<<uint(bit)) != 0 }

// mcp23017 is one MCP23017 expander at a fixed bus address. It caches the output
// and configuration registers in shadow bytes, so changing one pin is a single
// register write (no read-modify-write round trip) and relays sharing a port can
// never race a read. All access is serialized by mu.
//
// The shadow starts at the chip's power-on state — every pin an input (IODIR
// 0xFF), latches low, no pull-ups, no inversion — so an un-armed relay sits as a
// high-impedance input, the fail-safe (de-energized) condition.
type mcp23017 struct {
	bus  Bus
	addr uint16

	mu    sync.Mutex
	iodir [2]byte // 1 = input
	gppu  [2]byte // 1 = pull-up enabled
	ipol  [2]byte // 1 = read inverted
	olat  [2]byte // last written output latch
}

func newChip(bus Bus, addr uint16) *mcp23017 {
	return &mcp23017{
		bus:   bus,
		addr:  addr,
		iodir: [2]byte{0xFF, 0xFF}, // mirror power-on: all inputs (fail-safe)
	}
}

// configureInput sets a pin as an input with an optional pull-up and active-low
// inversion (so a subsequent read returns 1 = active). It pushes the direction,
// pull-up, and polarity registers for the pin's port.
func (c *mcp23017) configureInput(pin int, pullUp, activeLow bool) error {
	port, bit := portBit(pin)
	c.mu.Lock()
	defer c.mu.Unlock()
	setBit(&c.iodir[port], bit, true)
	setBit(&c.gppu[port], bit, pullUp)
	setBit(&c.ipol[port], bit, activeLow)
	if err := c.bus.WriteReg(c.addr, gppuReg[port], c.gppu[port]); err != nil {
		return err
	}
	if err := c.bus.WriteReg(c.addr, ipolReg[port], c.ipol[port]); err != nil {
		return err
	}
	return c.bus.WriteReg(c.addr, iodirReg[port], c.iodir[port])
}

// configureOutput sets a pin as an output, driving its latch to the de-energized
// level BEFORE flipping the direction, so arming a relay never glitches it
// energized for the interval between the two writes.
func (c *mcp23017) configureOutput(pin int, activeLow bool) error {
	port, bit := portBit(pin)
	c.mu.Lock()
	defer c.mu.Unlock()
	// De-energized physical level = (active=false) XOR activeLow = activeLow.
	setBit(&c.olat[port], bit, activeLow)
	if err := c.bus.WriteReg(c.addr, olatReg[port], c.olat[port]); err != nil {
		return err
	}
	setBit(&c.iodir[port], bit, false)
	return c.bus.WriteReg(c.addr, iodirReg[port], c.iodir[port])
}

// writeOutput drives an output pin to a logical-active state, honoring active-low
// wiring (physical high = active XOR activeLow).
func (c *mcp23017) writeOutput(pin int, active, activeLow bool) error {
	port, bit := portBit(pin)
	c.mu.Lock()
	defer c.mu.Unlock()
	setBit(&c.olat[port], bit, active != activeLow)
	return c.bus.WriteReg(c.addr, olatReg[port], c.olat[port])
}

// readPort reads a GPIO port register (0 = A, 1 = B). With IPOL configured per
// pin, a set bit on a configured input means "active".
func (c *mcp23017) readPort(port int) (byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bus.ReadReg(c.addr, gpioReg[port])
}

// release returns an output pin to its fail-safe state: de-energized, then back
// to a high-impedance input. Used on disarm and close.
func (c *mcp23017) release(pin int, activeLow bool) error {
	port, bit := portBit(pin)
	c.mu.Lock()
	defer c.mu.Unlock()
	setBit(&c.olat[port], bit, activeLow) // de-energized
	if err := c.bus.WriteReg(c.addr, olatReg[port], c.olat[port]); err != nil {
		return err
	}
	setBit(&c.iodir[port], bit, true) // high-impedance input
	return c.bus.WriteReg(c.addr, iodirReg[port], c.iodir[port])
}
