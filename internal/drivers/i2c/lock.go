package i2c

import (
	"fmt"
	"sync"
	"time"

	"github.com/stone-age-io/access-control/internal/logger"
)

// i2cLock energizes one MCP23017 output pin for a portal (or aux output). The
// line is energized while a momentary Pulse is in flight OR a standing SetHeld is
// set — Pulse's one-shot timer drops back to the held state, not unconditionally
// off, so the two compose (a tap during a scheduled-unlock window pulses
// harmlessly). This mirrors the GPIO backend's gpioLock; only the physical write
// differs (a shared OLAT register bit vs an independent GPIO line).
type i2cLock struct {
	chip      *mcp23017
	pin       int
	activeLow bool
	log       *logger.Logger

	mu     sync.Mutex
	timer  *time.Timer
	held   bool // standing hold (posture unlocked / auto-unlock)
	closed bool
}

// Pulse implements drivers.LockDriver: energize the relay, then drop back to the
// standing hold state after seconds. A non-positive seconds uses defaultPulseSeconds.
func (l *i2cLock) Pulse(seconds int) error {
	if seconds <= 0 {
		seconds = defaultPulseSeconds
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return fmt.Errorf("lock for pin %d is closed", l.pin)
	}
	if l.timer != nil {
		l.timer.Stop()
	}
	if err := l.chip.writeOutput(l.pin, true, l.activeLow); err != nil {
		return err
	}
	l.timer = time.AfterFunc(time.Duration(seconds)*time.Second, func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.closed {
			return
		}
		// Drop back to the standing hold state, not unconditionally off: a pulse
		// over a held-open door must leave the door held when it expires.
		if err := l.chip.writeOutput(l.pin, l.held, l.activeLow); err != nil {
			l.log.Error("failed to de-energize relay", "pin", l.pin, "error", err)
		}
		l.timer = nil
	})
	return nil
}

// SetHeld implements drivers.LockDriver: set or clear the standing hold. Energizes
// immediately when held; when released, de-energizes unless a pulse is still in
// flight (that pulse's timer will then drop to the now-false held state). Idempotent.
func (l *i2cLock) SetHeld(held bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return fmt.Errorf("lock for pin %d is closed", l.pin)
	}
	if l.held == held {
		return nil
	}
	l.held = held
	if held {
		return l.chip.writeOutput(l.pin, true, l.activeLow)
	}
	if l.timer != nil {
		return nil // a momentary pulse is still energizing the line; let it expire
	}
	return l.chip.writeOutput(l.pin, false, l.activeLow)
}

// Close stops any pending release, de-energizes, and returns the pin to a
// high-impedance input (fail-safe).
func (l *i2cLock) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return
	}
	l.closed = true
	if l.timer != nil {
		l.timer.Stop()
		l.timer = nil
	}
	if err := l.chip.release(l.pin, l.activeLow); err != nil && l.log != nil {
		l.log.Error("failed to release relay", "pin", l.pin, "error", err)
	}
}
