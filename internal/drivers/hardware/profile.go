// Package hardware maps a controller model's *logical* relay/input indices (the
// numbers an operator sets on a portal record: lock_relay, dps_input, rex_input)
// to the *physical* lines on that board. Each supported model has a Profile; the
// GPIO driver resolves a portal's indices through the profile for the controller's
// model. Boards differ only by this data, so adding one is adding a Profile, not
// new driver code.
//
// Two physical backends are modelled. The KinCony Server-Mini (Raspberry Pi CM4)
// wires relays and inputs to CM4 GPIO directly, so its lines are BackendGPIO
// (gpiochip + offset). The Pi5R8 (planned) addresses I/O through I2C expanders,
// so its lines are BackendI2C — defined here as data but not yet driven (the GPIO
// backend rejects BackendI2C with a clear error).
package hardware

// Backend is the physical access method for a line.
type Backend string

const (
	// BackendGPIO is a direct GPIO character-device line (chip + offset).
	BackendGPIO Backend = "gpio"
	// BackendI2C is an I2C GPIO-expander pin (bus + addr + pin). Declared for the
	// Pi5R8 profile; not implemented this milestone.
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

	// BackendI2C (Pi5R8 — placeholder values, verify against the schematic):
	Bus  int
	Addr int
	Pin  int
}

// Profile is one model's logical→physical mapping. Relay/Input look-ups are by
// the 1-based logical index stored on portal records.
type Profile struct {
	Model  string
	relays map[int]LineSpec
	inputs map[int]LineSpec
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
	// BCM pin map per KinCony's CM4 pin definition; relay/input polarity assumed
	// per the board's typical wiring — verify on the bench before production.
	"kincony-server-mini": {
		Model: "kincony-server-mini",
		relays: map[int]LineSpec{
			1: gpioRelay(5), 2: gpioRelay(22), 3: gpioRelay(17), 4: gpioRelay(4),
			5: gpioRelay(6), 6: gpioRelay(13), 7: gpioRelay(19), 8: gpioRelay(26),
		},
		inputs: map[int]LineSpec{
			1: gpioInput(18), 2: gpioInput(23), 3: gpioInput(24), 4: gpioInput(25),
			5: gpioInput(12), 6: gpioInput(16), 7: gpioInput(20), 8: gpioInput(21),
		},
	},

	// KinCony Pi5R8 (Raspberry Pi CM5) — STUB. I/O is addressed via I2C expanders
	// rather than native GPIO; the descriptors below are PLACEHOLDERS (two PCF8574-
	// style expanders on bus 1: relays at 0x20, inputs at 0x22) to exercise the
	// multi-backend template. The I2C backend is not implemented this milestone, so
	// selecting this model fails fast in the GPIO driver. Replace with verified
	// bus/addr/pin values when the board is in hand.
	"kincony-pi5r8": {
		Model: "kincony-pi5r8",
		relays: map[int]LineSpec{
			1: i2cLine(1, 0x20, 0, false), 2: i2cLine(1, 0x20, 1, false),
			3: i2cLine(1, 0x20, 2, false), 4: i2cLine(1, 0x20, 3, false),
			5: i2cLine(1, 0x20, 4, false), 6: i2cLine(1, 0x20, 5, false),
			7: i2cLine(1, 0x20, 6, false), 8: i2cLine(1, 0x20, 7, false),
		},
		inputs: map[int]LineSpec{
			1: i2cLine(1, 0x22, 0, true), 2: i2cLine(1, 0x22, 1, true),
			3: i2cLine(1, 0x22, 2, true), 4: i2cLine(1, 0x22, 3, true),
			5: i2cLine(1, 0x22, 4, true), 6: i2cLine(1, 0x22, 5, true),
			7: i2cLine(1, 0x22, 6, true), 8: i2cLine(1, 0x22, 7, true),
		},
	},
}
