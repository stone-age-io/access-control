//go:build linux

package gpio

import (
	"testing"

	"github.com/stone-age-io/access-control/internal/drivers/hardware"
	"github.com/stone-age-io/access-control/internal/logger"
)

func newHW(t *testing.T, model string) Driver {
	t.Helper()
	p, ok := hardware.ProfileFor(model)
	if !ok {
		t.Fatalf("profile %q not registered", model)
	}
	d, err := New(p, logger.NewNopLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return d
}

// New allocates no lines, so it succeeds without any gpiochip; Inputs is live.
func TestNewAndInputs(t *testing.T) {
	d := newHW(t, "kincony-server-mini")
	if d.Inputs() == nil {
		t.Error("Inputs() returned nil channel")
	}
	if err := d.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if err := d.Close(); err != nil { // idempotent
		t.Errorf("second Close: %v", err)
	}
}

// A relay index the model does not define fails before touching any hardware, so
// this is deterministic without a gpiochip.
func TestArmUnknownRelayIndex(t *testing.T) {
	d := newHW(t, "kincony-server-mini")
	defer d.Close()
	if _, err := d.Arm("p1", 99, 0, 0); err == nil {
		t.Error("Arm with relay index 99 succeeded, want error")
	}
	// Index 0 (unset) is also undefined → error, not a wrong line.
	if _, err := d.Arm("p2", 0, 0, 0); err == nil {
		t.Error("Arm with unset relay index 0 succeeded, want error")
	}
}

// The Pi5R8 profile is I2C-backed; the GPIO driver rejects it before any line
// request, proving the stub is "defined but not driven".
func TestArmI2CBackendRejected(t *testing.T) {
	d := newHW(t, "kincony-pi5r8")
	defer d.Close()
	_, err := d.Arm("p1", 1, 0, 0)
	if err == nil {
		t.Fatal("Arm on an I2C-backed model succeeded, want 'not supported' error")
	}
}

// Arming on a closed backend fails; disarming an unknown portal is a no-op.
func TestArmAfterCloseAndDisarmUnknown(t *testing.T) {
	d := newHW(t, "kincony-server-mini")
	d.Disarm("never-armed") // no panic
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := d.Arm("p1", 1, 0, 0); err == nil {
		t.Error("Arm after Close succeeded, want error")
	}
}
