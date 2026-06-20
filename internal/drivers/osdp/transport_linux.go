//go:build linux

package osdp

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// serialPort is the Linux RS485 Transceiver: a raw, 8N1 serial device configured
// via termios. v1 relies on the board's auto-direction RS485 transceiver (the
// KinCony Server-Mini and Pi5R8 RS485 ports self-sense TX/RX), so no software
// DE/RE line toggling is done here — if a board needs it, the TIOCSRS485 ioctl
// would be added at Open (a bench item; see docs/protocol.md).
type serialPort struct {
	fd     int
	device string
}

// OpenSerial opens an RS485 device (e.g. /dev/ttyAMA2) at the given baud (OSDP is
// 9600 by default) in raw 8N1 mode and returns it as a Transceiver.
func OpenSerial(device string, baud int) (Transceiver, error) {
	speed, ok := baudConst(baud)
	if !ok {
		return nil, fmt.Errorf("osdp: unsupported baud rate %d", baud)
	}

	fd, err := unix.Open(device, unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("osdp: open %s: %w", device, err)
	}

	t, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("osdp: get termios %s: %w", device, err)
	}

	// Raw mode (cfmakeraw): no canonical processing, echo, signals, or output
	// translation; 8 data bits, no parity, one stop bit; ignore modem control.
	t.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP |
		unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	t.Oflag &^= unix.OPOST
	t.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	t.Cflag &^= unix.CSIZE | unix.PARENB | unix.CSTOPB
	t.Cflag |= unix.CS8 | unix.CREAD | unix.CLOCAL
	t.Cflag = (t.Cflag &^ unix.CBAUD) | speed
	t.Ispeed = speed
	t.Ospeed = speed

	// Blocking read that returns as soon as any byte arrives, or after 100ms with
	// none (VMIN=0, VTIME=1). The engine loops these slices up to its respTimeout.
	t.Cc[unix.VMIN] = 0
	t.Cc[unix.VTIME] = 1

	if err := unix.IoctlSetTermios(fd, unix.TCSETS, t); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("osdp: set termios %s: %w", device, err)
	}
	_ = unix.IoctlSetInt(fd, unix.TCFLSH, unix.TCIOFLUSH) // discard stale bytes

	return &serialPort{fd: fd, device: device}, nil
}

// Send writes a full frame, then drains so the bytes are on the wire before the
// engine switches to reading (matters on half-duplex).
func (s *serialPort) Send(frame []byte) error {
	for len(frame) > 0 {
		n, err := unix.Write(s.fd, frame)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return fmt.Errorf("osdp: write %s: %w", s.device, err)
		}
		frame = frame[n:]
	}
	// TCSBRK with a non-zero arg waits for pending output to drain (tcdrain).
	_ = unix.IoctlSetInt(s.fd, unix.TCSBRK, 1)
	return nil
}

// Receive reads whatever is available, blocking up to the termios VTIME slice.
// It returns (0, nil) on an empty slice so the engine can poll the deadline.
func (s *serialPort) Receive(buf []byte) (int, error) {
	for {
		n, err := unix.Read(s.fd, buf)
		if err != nil {
			if errors.Is(err, unix.EINTR) || errors.Is(err, unix.EAGAIN) {
				return 0, nil
			}
			return 0, fmt.Errorf("osdp: read %s: %w", s.device, err)
		}
		return n, nil
	}
}

func (s *serialPort) Close() error { return unix.Close(s.fd) }

// baudConst maps a numeric baud rate to its termios constant.
func baudConst(baud int) (uint32, bool) {
	switch baud {
	case 9600:
		return unix.B9600, true
	case 19200:
		return unix.B19200, true
	case 38400:
		return unix.B38400, true
	case 57600:
		return unix.B57600, true
	case 115200:
		return unix.B115200, true
	default:
		return 0, false
	}
}
