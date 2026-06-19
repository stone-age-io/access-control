package controller

import (
	"context"
	"testing"
	"time"

	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/subjects"
)

// heartbeats returns the subjects of every emitted heartbeat (those carrying a
// HeartbeatPayload).
func heartbeats(e *fakeEmitter) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
	for _, ev := range e.events {
		if _, ok := ev.payload.(HeartbeatPayload); ok {
			out = append(out, ev.subject)
		}
	}
	return out
}

// Start publishes one heartbeat immediately, before the first tick.
func TestHeartbeatPublishesImmediately(t *testing.T) {
	emit := &fakeEmitter{}
	hb := NewHeartbeat(emit, subjects.Default(), "ctrl-hq-1", "hq", time.Hour, logger.NewNopLogger(), nil)
	hb.Start(context.Background())
	defer hb.Stop()

	eventually(t, time.Second, func() bool { return len(heartbeats(emit)) == 1 })

	got := heartbeats(emit)
	if got[0] != "acc.hq.ctrl.ctrl-hq-1.heartbeat" {
		t.Errorf("heartbeat subject = %q", got[0])
	}
	// Payload carries the controller identity.
	emit.mu.Lock()
	p := emit.events[0].payload.(HeartbeatPayload)
	emit.mu.Unlock()
	if p.Code != "ctrl-hq-1" || p.Location != "hq" || p.TS == "" {
		t.Errorf("payload = %+v, want code=ctrl-hq-1 location=hq ts!=\"\"", p)
	}
}

// The ticker keeps publishing, and Stop halts it.
func TestHeartbeatTicksThenStops(t *testing.T) {
	emit := &fakeEmitter{}
	hb := NewHeartbeat(emit, subjects.Default(), "ctrl-hq-1", "hq", 5*time.Millisecond, logger.NewNopLogger(), nil)
	hb.Start(context.Background())

	eventually(t, 2*time.Second, func() bool { return len(heartbeats(emit)) >= 3 })

	hb.Stop()
	after := len(heartbeats(emit))
	time.Sleep(30 * time.Millisecond) // a few intervals
	if got := len(heartbeats(emit)); got != after {
		t.Errorf("heartbeats kept coming after Stop: %d -> %d", after, got)
	}
}

// A controller with no code publishes nothing (accessd keys liveness on code).
func TestHeartbeatNoCode(t *testing.T) {
	emit := &fakeEmitter{}
	hb := NewHeartbeat(emit, subjects.Default(), "", "hq", time.Millisecond, logger.NewNopLogger(), nil)
	hb.Start(context.Background())
	defer hb.Stop()

	time.Sleep(20 * time.Millisecond)
	if got := len(heartbeats(emit)); got != 0 {
		t.Errorf("heartbeats with empty code = %d, want 0", got)
	}
}

// A non-positive interval falls back to the default rather than spinning.
func TestHeartbeatDefaultInterval(t *testing.T) {
	hb := NewHeartbeat(&fakeEmitter{}, subjects.Default(), "ctrl-hq-1", "hq", 0, logger.NewNopLogger(), nil)
	if hb.interval != DefaultHeartbeatInterval {
		t.Errorf("interval = %s, want %s", hb.interval, DefaultHeartbeatInterval)
	}
}
