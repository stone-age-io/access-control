//go:build !linux

package gpio

import (
	"fmt"

	"github.com/stone-age-io/access-control/internal/drivers/hardware"
	"github.com/stone-age-io/access-control/internal/logger"
)

// New returns an error on non-Linux platforms: the GPIO character device exists
// only on Linux. This stub keeps the binary buildable elsewhere (e.g. local
// development on macOS); the controller runs on Linux edge hardware in practice.
func New(_ hardware.Profile, _ *logger.Logger) (Driver, error) {
	return nil, fmt.Errorf("gpio driver requires linux")
}
