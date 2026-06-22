// Package drivers is the hardware-abstraction boundary for the edge controller:
// the tap loop and the decision core depend only on these interfaces. It ships
// mock implementations (this package) and a real GPIO lock/door-input backend
// (internal/drivers/gpio, keyed by a model profile in internal/drivers/hardware).
// The reader stays simulated over NATS — a real OSDP/RS485 ReaderDriver slots in
// behind ReaderDriver later without touching the loop or the decision core.
package drivers

import "time"

// Reader source markers for Tap.Source: which transport produced the tap. They
// flow verbatim onto the tap event so an operator can tell a physical OSDP read
// from a NATS-published tap forensically.
const (
	SourceNATS = "nats"
	SourceOSDP = "osdp"
)

// Tap is a single credential presentation at a reader: which portal, what
// opaque credential value, when (UTC), and which reader produced it. The reader
// stamps At so tests can drive deterministic instants and sets Source to one of
// the Source* constants.
type Tap struct {
	Portal     string
	Credential string
	At         time.Time
	Source     string
}

// ReaderDriver emits taps. The returned channel is closed when the reader stops.
type ReaderDriver interface {
	Taps() <-chan Tap
}

// LockDriver energizes a strike/relay for a portal. The line is energized while
// EITHER a momentary pulse is in flight OR a standing hold is set — the two
// compose, so a habitual tap during a scheduled-unlock window pulses harmlessly
// and the line stays held when the pulse expires.
//
//   - Pulse holds the strike for the given number of seconds (the decision's
//     pulse value). A zero or negative value means "use the driver's default".
//   - SetHeld sets/clears the standing hold (posture unlocked / auto-unlock). It
//     is idempotent: setting the current value is a no-op.
type LockDriver interface {
	Pulse(seconds int) error
	SetHeld(held bool) error
}

// Input kinds for InputEvent.Kind.
const (
	InputDPS = "dps" // door-position switch (open/closed)
	InputREX = "rex" // request-to-exit (egress press)
	InputAux = "aux" // named auxiliary input (observe-only)
)

// InputEvent is one digital-input transition for a portal. Kind selects which
// signal changed: a door-position switch (DPS), whose Closed reports the contact
// state, or a request-to-exit (REX), whose Active reports the press. At is when
// the transition occurred (UTC); the driver stamps it so tests stay deterministic.
type InputEvent struct {
	Portal string
	Kind   string
	Closed bool // DPS: true = door closed
	Active bool // REX: true = egress requested
	At     time.Time
}

// DoorInput emits door-monitoring transitions (DPS/REX) used for forced and
// held-open detection. The returned channel is closed when the source stops. A
// controller without door monitoring wired has a nil DoorInput and simply never
// sees these events.
type DoorInput interface {
	Inputs() <-chan InputEvent
}

// FAIInput reports a location's fire-alarm-input state. Active is true while the
// FAI asserts free egress. The controller only observes this to suppress false
// alarms — hardware owns egress, software never unlocks for fire.
type FAIInput interface {
	Fire() <-chan FireState
}

// FireState is a location-scoped fire signal.
type FireState struct {
	Location string
	Active   bool
	At       time.Time
}
