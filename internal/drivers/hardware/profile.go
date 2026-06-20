// Package hardware maps a controller model's *logical* relay/input indices (the
// numbers an operator sets on a portal record: lock_relay, dps_input, rex_input)
// to the *physical* lines on that board. Each supported model has a Profile; the
// GPIO driver resolves a portal's indices through the profile for the controller's
// model. Boards differ only by this data, so adding one is adding a Profile, not
// new driver code.
//
// Two physical backends are modelled. The KinCony Server-Mini (Raspberry Pi CM4)
// wires relays and inputs to CM4 GPIO directly, so its lines are BackendGPIO
// (gpiochip + offset), driven by internal/drivers/gpio. The Pi5R8 (Raspberry Pi
// CM5) addresses I/O through an MCP23017 I2C expander, so its lines are
// BackendI2C (bus + addr + pin), driven by internal/drivers/i2c. The controller
// picks the matching backend from Profile.Transport().
package hardware

// Backend is the physical access method for a line.
type Backend string

const (
	// BackendGPIO is a direct GPIO character-device line (chip + offset).
	BackendGPIO Backend = "gpio"
	// BackendI2C is an I2C GPIO-expander pin (bus + addr + pin), e.g. an MCP23017.
	// Driven by internal/drivers/i2c.
	BackendI2C Backend = "i2c"
)

// LineSpec describes one physical line — a relay output or a digital input.
type LineSpec struct {
	Backend Backend

	// BackendGPIO:
	Chip   string // gpiochip name, e.g. "gpiochip0"
	Offset int    // line offset on Chip (the BCM number on a Raspberry Pi)

	// ActiveLow reports that the line is asserted when physically low. The GPIO
	// driver pushes this into the kernel request (AsActiveLow), so "active" means
	// "relay energized" / "input contact made" regardless of wiring polarity.
	ActiveLow bool

	// BackendI2C (e.g. MCP23017): Bus is the Linux I2C bus number (/dev/i2c-N),
	// Addr is the 7-bit device address, and Pin is the expander pin 0-15 — 0-7 is
	// port A, 8-15 is port B.
	Bus  int
	Addr int
	Pin  int
}

// SerialPort describes a model's RS485 serial port — where an OSDP reader bus is
// wired and at what line rate. Board-specific, like the relay/input lines, so it
// lives in the profile rather than in per-controller config.
type SerialPort struct {
	Device string // Linux device path, e.g. /dev/ttyAMA2
	Baud   int    // line rate; OSDP defaults to 9600
}

// Profile is one model's logical→physical mapping. Relay/Input look-ups are by
// the 1-based logical index stored on portal records.
type Profile struct {
	Model  string
	relays map[int]LineSpec
	inputs map[int]LineSpec
	serial SerialPort
}

// Serial returns the model's RS485 serial port for the OSDP reader and whether
// the model defines one. A model with no serial port cannot drive an OSDP reader.
func (p Profile) Serial() (SerialPort, bool) {
	return p.serial, p.serial.Device != ""
}

// Relay returns the physical line for a logical relay index (1-based) and whether
// the model defines it.
func (p Profile) Relay(idx int) (LineSpec, bool) {
	s, ok := p.relays[idx]
	return s, ok
}

// Input returns the physical line for a logical input index (1-based) and whether
// the model defines it.
func (p Profile) Input(idx int) (LineSpec, bool) {
	s, ok := p.inputs[idx]
	return s, ok
}

// Lines returns every physical line the profile defines — relays then inputs, in
// ascending logical-index order — for backend setup and diagnostics (e.g.
// resolving the I2C bus a model's lines share).
func (p Profile) Lines() []LineSpec {
	out := make([]LineSpec, 0, len(p.relays)+len(p.inputs))
	for i := 1; i <= len(p.relays); i++ {
		if s, ok := p.relays[i]; ok {
			out = append(out, s)
		}
	}
	for i := 1; i <= len(p.inputs); i++ {
		if s, ok := p.inputs[i]; ok {
			out = append(out, s)
		}
	}
	return out
}

// Transport reports the physical backend this model's lines use, so the
// controller can construct the matching driver (GPIO char device vs I2C). A
// profile is homogeneous — every line shares one backend — so this returns the
// backend of the first defined line, and BackendGPIO if the profile is empty.
func (p Profile) Transport() Backend {
	if lines := p.Lines(); len(lines) > 0 {
		return lines[0].Backend
	}
	return BackendGPIO
}

// ProfileFor returns the hardware profile for a controller model and whether one
// is registered. An unknown model has no profile, so a GPIO controller of that
// model arms nothing (fail-safe).
func ProfileFor(model string) (Profile, bool) {
	p, ok := registry[model]
	return p, ok
}

// Models lists the registered model identifiers (for diagnostics).
func Models() []string {
	out := make([]string, 0, len(registry))
	for m := range registry {
		out = append(out, m)
	}
	return out
}

// rpiChip is the Broadcom GPIO bank on a Raspberry Pi 4 / CM4; the BCM number is
// the line offset on it. (A Pi 5 native bank would be a different chip, but the
// Pi5R8 uses I2C expanders, so it does not apply here.)
const rpiChip = "gpiochip0"

func gpioRelay(offset int) LineSpec {
	// KinCony relays are driven active-high (GPIO high energizes the relay).
	return LineSpec{Backend: BackendGPIO, Chip: rpiChip, Offset: offset, ActiveLow: false}
}

func gpioInput(offset int) LineSpec {
	// KinCony isolated inputs pull low when the external contact closes, so the
	// asserted ("active") state is low — request them active-low with a pull-up.
	return LineSpec{Backend: BackendGPIO, Chip: rpiChip, Offset: offset, ActiveLow: true}
}

func i2cLine(bus, addr, pin int, activeLow bool) LineSpec {
	return LineSpec{Backend: BackendI2C, Bus: bus, Addr: addr, Pin: pin, ActiveLow: activeLow}
}

// registry holds every supported model's profile. Model strings match the
// controllers.model select values (see pbmigrations) and the controller's policy
// record.
var registry = map[string]Profile{
	// KinCony Server-Mini (Raspberry Pi CM4, 8 relays + 8 inputs to GPIO directly).
	// BCM pin map verified against KinCony's published CM4 pin definition: logical
	// relay N = OUTn, logical input N = INn. Line polarity (relays active-high,
	// inputs active-low) follows the board's wiring convention — verify on the
	// bench before production.
	"kincony-server-mini": {
		Model: "kincony-server-mini",
		// OUT1..8 = BCM 5, 22, 17, 4, 6, 13, 19, 26
		relays: map[int]LineSpec{
			1: gpioRelay(5), 2: gpioRelay(22), 3: gpioRelay(17), 4: gpioRelay(4),
			5: gpioRelay(6), 6: gpioRelay(13), 7: gpioRelay(19), 8: gpioRelay(26),
		},
		// IN1..8 = BCM 18, 23, 24, 25, 12, 16, 20, 21
		inputs: map[int]LineSpec{
			1: gpioInput(18), 2: gpioInput(23), 3: gpioInput(24), 4: gpioInput(25),
			5: gpioInput(12), 6: gpioInput(16), 7: gpioInput(20), 8: gpioInput(21),
		},
		// RS485 on the CM4 primary UART (GPIO14/15), exposed as /dev/ttyAMA0.
		serial: SerialPort{Device: "/dev/ttyAMA0", Baud: 9600},
	},

	// KinCony Pi5R8 (Raspberry Pi CM5): all 16 relay/input lines are on a single
	// MCP23017 I2C expander at 0x20 on bus 1 (/dev/i2c-1), per KinCony's reference
	// Node-RED flow. Inputs IN1..8 are Port A (pins 0-7), pulled up and active-low
	// (contact to GND); relays OUT1..8 are Port B (pins 8-15). Relay polarity
	// (assumed active-high) and a possible second expander at 0x22 (present but
	// unused in the reference flow) are bench items to confirm on the board.
	"kincony-pi5r8": {
		Model: "kincony-pi5r8",
		// OUT1..8 = MCP23017 @0x20 port B, pins 8..15
		relays: map[int]LineSpec{
			1: i2cLine(1, 0x20, 8, false), 2: i2cLine(1, 0x20, 9, false),
			3: i2cLine(1, 0x20, 10, false), 4: i2cLine(1, 0x20, 11, false),
			5: i2cLine(1, 0x20, 12, false), 6: i2cLine(1, 0x20, 13, false),
			7: i2cLine(1, 0x20, 14, false), 8: i2cLine(1, 0x20, 15, false),
		},
		// IN1..8 = MCP23017 @0x20 port A, pins 0..7
		inputs: map[int]LineSpec{
			1: i2cLine(1, 0x20, 0, true), 2: i2cLine(1, 0x20, 1, true),
			3: i2cLine(1, 0x20, 2, true), 4: i2cLine(1, 0x20, 3, true),
			5: i2cLine(1, 0x20, 4, true), 6: i2cLine(1, 0x20, 5, true),
			7: i2cLine(1, 0x20, 6, true), 8: i2cLine(1, 0x20, 7, true),
		},
		// RS485 on the CM5 UART2 (GPIO4/5), exposed as /dev/ttyAMA2.
		serial: SerialPort{Device: "/dev/ttyAMA2", Baud: 9600},
	},
}
