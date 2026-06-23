// Package gpio drives portal lock relays and DPS/REX door inputs over the Linux
// GPIO character device (no cgo — via go-gpiocdev), resolving each portal's
// logical relay/input indices through its controller model's hardware.Profile.
//
// It is Linux-only: the GPIO character device exists only on Linux. On other
// platforms New returns an error (see gpio_other.go) so the rest of the binary
// still builds for local development; the controller only ever runs on Linux edge
// hardware.
package gpio

import "github.com/stone-age-io/access-control/internal/drivers"

// Driver is the GPIO hardware backend. It satisfies the controller's per-portal
// hardware-arming surface (Arm/Disarm → controller.PortalHardware) and
// drivers.DoorInput (Inputs), and is Closed on shutdown to release every line.
type Driver interface {
	// Arm requests the lock relay line (returned as the LockDriver the tap loop
	// pulses) plus the DPS/REX input lines for a portal, applying the portal's
	// logical sense (maglock / contact inversion) on top of the model's electrical
	// polarity. A dps/rex index of 0 means "not wired" and is skipped. On any
	// partial failure it releases what it had already requested and returns an error.
	Arm(code string, io drivers.PortalIO) (drivers.LockDriver, error)
	// Disarm releases every line requested for a portal.
	Disarm(code string)
	// ArmOutput/DisarmOutput/ArmInput/DisarmInput arm auxiliary points (a bare
	// relay or input line) — the controller.AuxHardware surface. Aux input edges
	// flow into the same Inputs() stream with kind InputAux; invert flips the
	// input's contact sense (normally-closed wiring).
	ArmOutput(code string, relayIndex int) (drivers.LockDriver, error)
	DisarmOutput(code string)
	ArmInput(code string, inputIndex int, invert bool) error
	DisarmInput(code string)
	// Inputs is the shared door-input stream (DPS/REX/aux edges) feeding the runtime.
	Inputs() <-chan drivers.InputEvent
	// Close releases all lines and closes the input stream.
	Close() error
}
