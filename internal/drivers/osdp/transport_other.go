//go:build !linux

package osdp

import "fmt"

// OpenSerial is Linux-only: the controller's OSDP reader runs on the Raspberry Pi
// CM4/CM5 edge boards. This stub lets the package build on a dev machine (e.g.
// Windows/macOS) so unit tests — which use an in-memory Transceiver — still run.
func OpenSerial(device string, baud int) (Transceiver, error) {
	return nil, fmt.Errorf("osdp: serial transport is only supported on Linux (requested %s @ %d baud)", device, baud)
}
