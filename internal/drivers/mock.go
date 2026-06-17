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

// MockLock records pulses and (optionally) logs them. Safe for concurrent use.
type MockLock struct {
	point string
	log   *logger.Logger

	mu     sync.Mutex
	pulses []int
}

// NewMockLock creates a mock lock for an access point. log may be nil.
func NewMockLock(point string, log *logger.Logger) *MockLock {
	return &MockLock{point: point, log: log}
}

// Pulse implements LockDriver.
func (l *MockLock) Pulse(seconds int) error {
	l.mu.Lock()
	l.pulses = append(l.pulses, seconds)
	l.mu.Unlock()
	if l.log != nil {
		l.log.Info("lock pulse", "point", l.point, "seconds", seconds)
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
