// Package drivers is the hardware-abstraction boundary for the edge controller:
// the tap loop and the decision core depend only on these interfaces. It ships
// mock implementations (this package) and a real GPIO lock/door-input backend
// (internal/drivers/gpio, keyed by a model profile in internal/drivers/hardware).
// The reader stays simulated over NATS — a real OSDP/RS485 ReaderDriver slots in
// behind ReaderDriver later without touching the loop or the decision core.
package drivers

import (
	"time"

	"github.com/stone-age-io/access-control/internal/subjects"
)

// Reader source markers for Tap.Source: which transport produced the tap. They
// flow verbatim onto the tap event so an operator can tell a physical OSDP read
// from a NATS-published tap forensically.
//
// Re-exported from internal/subjects, which holds the canonical list — accessd
// stamps two further sources (command / badge) for acts that never reached a
// reader, and one drifting set of these strings would make the audit trail lie.
const (
	SourceNATS = subjects.SourceNATS
	SourceOSDP = subjects.SourceOSDP
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

// PortalIO is the physical wiring a PortalHardware backend arms for one portal:
// the logical relay/input indices (0 = not wired) plus the per-line *logical
// sense* the operator configured. The backend resolves the indices to physical
// lines through the model profile (which carries the board's electrical
// polarity) and folds these inversions on top, so "sense" stays a per-install
// concern, separate from the board convention.
//
//   - Maglock inverts the lock relay's drive sense: a fail-safe maglock energizes
//     to LOCK (so it idles energized), versus a fail-secure strike that energizes
//     to unlock. The backend XORs it into the relay line's active-low.
//   - DpsInvert / RexInvert flip a door input's contact sense for normally-open
//     (DPS) / normally-closed (REX) wiring, relative to the system default.
type PortalIO struct {
	LockRelay int
	DpsInput  int
	RexInput  int
	Maglock   bool
	DpsInvert bool
	RexInvert bool
}

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

// A fire-alarm interface is NOT a driver interface. It was one (FAIInput, with a
// FireState channel) and had zero implementations in any backend, because an FAI is
// electrically just another dry contact — everything it needed already existed as an
// aux input: a controller binding, a logical index, a contact sense, and a driver
// path that already delivers InputAux transitions on both GPIO and I2C.
//
// So a fire point is an aux_input whose point_type is `fire`. The runtime turns its
// transitions into {app}.{location}.evt.fire, which every controller at that location
// applies. That makes the FAI configuration data rather than code, and deleted an
// interface instead of adding an implementation.
//
// Hardware still owns egress: the fire panel's relay drops maglock power directly,
// and nothing in software unlocks a door for fire.
