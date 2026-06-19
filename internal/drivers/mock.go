package drivers

import (
	"sync"

	"github.com/stone-age-io/access-control/internal/logger"
)

// MockReader is a channel-fed reader for tests and simulation. Call Tap to
// enqueue a presentation and Close when done.
type MockReader struct {
	ch chan Tap
}

// NewMockReader creates a mock reader with the given channel buffer.
func NewMockReader(buffer int) *MockReader {
	return &MockReader{ch: make(chan Tap, buffer)}
}

// Taps implements ReaderDriver.
func (r *MockReader) Taps() <-chan Tap { return r.ch }

// Tap enqueues a credential presentation.
func (r *MockReader) Tap(t Tap) { r.ch <- t }

// Close closes the taps channel, which stops the consuming tap loop.
func (r *MockReader) Close() { close(r.ch) }

// MockDoorInput is a channel-fed door-input source for tests and simulation.
// Call Send to enqueue a DPS/REX transition and Close when done.
type MockDoorInput struct {
	ch chan InputEvent
}

// NewMockDoorInput creates a mock door input with the given channel buffer.
func NewMockDoorInput(buffer int) *MockDoorInput {
	return &MockDoorInput{ch: make(chan InputEvent, buffer)}
}

// Inputs implements DoorInput.
func (d *MockDoorInput) Inputs() <-chan InputEvent { return d.ch }

// Send enqueues a door-input transition.
func (d *MockDoorInput) Send(e InputEvent) { d.ch <- e }

// Close closes the inputs channel, which stops the consuming loop.
func (d *MockDoorInput) Close() { close(d.ch) }

// MockHardware is the no-hardware portal backend: Arm hands back a MockLock and
// records no door inputs. It is the controller's default driver and what the
// portal-manager tests use. It satisfies controller.PortalHardware.
type MockHardware struct {
	log *logger.Logger

	mu    sync.Mutex
	locks map[string]*MockLock
}

// NewMockHardware creates a mock portal backend. log may be nil.
func NewMockHardware(log *logger.Logger) *MockHardware {
	return &MockHardware{log: log, locks: make(map[string]*MockLock)}
}

// Arm returns a fresh MockLock for the portal; the logical indices are ignored
// (no physical lines). It never fails.
func (h *MockHardware) Arm(code string, _, _, _ int) (LockDriver, error) {
	l := NewMockLock(code, h.log)
	h.mu.Lock()
	h.locks[code] = l
	h.mu.Unlock()
	return l, nil
}

// Disarm forgets the portal's mock lock.
func (h *MockHardware) Disarm(code string) {
	h.mu.Lock()
	delete(h.locks, code)
	h.mu.Unlock()
}

// Lock returns the mock lock armed for a portal (for tests/inspection).
func (h *MockHardware) Lock(code string) (*MockLock, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	l, ok := h.locks[code]
	return l, ok
}

// MockLock records pulses and (optionally) logs them. Safe for concurrent use.
type MockLock struct {
	portal string
	log    *logger.Logger

	mu     sync.Mutex
	pulses []int
}

// NewMockLock creates a mock lock for a portal. log may be nil.
func NewMockLock(portal string, log *logger.Logger) *MockLock {
	return &MockLock{portal: portal, log: log}
}

// Pulse implements LockDriver.
func (l *MockLock) Pulse(seconds int) error {
	l.mu.Lock()
	l.pulses = append(l.pulses, seconds)
	l.mu.Unlock()
	if l.log != nil {
		l.log.Info("lock pulse", "portal", l.portal, "seconds", seconds)
	}
	return nil
}

// Pulses returns a copy of the recorded pulse durations.
func (l *MockLock) Pulses() []int {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]int, len(l.pulses))
	copy(out, l.pulses)
	return out
}
