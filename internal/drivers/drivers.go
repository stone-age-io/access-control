// Package drivers is the hardware-abstraction boundary for the edge controller.
// v1 ships interfaces plus mock implementations; real OSDP/RS485 reader and
// GPIO relay/FAI drivers (KinCony CM5) slot in behind these later without
// touching the tap loop or the decision core.
package drivers

import "time"

// Tap is a single credential presentation at a reader: which portal, what
// opaque credential value, and when (UTC). The reader stamps At so tests can
// drive deterministic instants.
type Tap struct {
	Portal     string
	Credential string
	At         time.Time
}

// ReaderDriver emits taps. The returned channel is closed when the reader stops.
type ReaderDriver interface {
	Taps() <-chan Tap
}

// LockDriver energizes a strike/relay for a portal. Pulse holds it for the
// given number of seconds (the decision's pulse value). A zero or negative
// value means "use the driver's default"; the mock just records it.
type LockDriver interface {
	Pulse(seconds int) error
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
