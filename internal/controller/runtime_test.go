package controller

import (
	"context"
	"sync"
	"testing"

	"github.com/stone-age-io/access-control/internal/drivers"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/policy"
	"github.com/stone-age-io/access-control/internal/subjects"
)

type fakeEmitter struct {
	mu     sync.Mutex
	events []emitted
}

type emitted struct {
	subject string
	payload any
}

func (e *fakeEmitter) Emit(subject string, payload any) error {
	e.mu.Lock()
	e.events = append(e.events, emitted{subject, payload})
	e.mu.Unlock()
	return nil
}

func (e *fakeEmitter) taps() []TapEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []TapEvent
	for _, ev := range e.events {
		if te, ok := ev.payload.(TapEvent); ok {
			out = append(out, te)
		}
	}
	return out
}

func (e *fakeEmitter) hasSubject(s string) bool {
	return e.countSubject(s) > 0
}

func (e *fakeEmitter) countSubject(s string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, ev := range e.events {
		if ev.subject == s {
			n++
		}
	}
	return n
}

// runtimeFor builds a runtime over the seeded fixture store with a mock reader
// and a single lock for lobby-main.
func runtimeFor(t *testing.T) (*Runtime, *drivers.MockReader, *drivers.MockLock, *fakeEmitter) {
	t.Helper()
	store := seeded(t)
	reader := drivers.NewMockReader(8)
	lock := drivers.NewMockLock("lobby-main", nil)
	emit := &fakeEmitter{}
	rt := NewRuntime("hq", store, reader,
		map[string]drivers.LockDriver{"lobby-main": lock}, emit,
		subjects.Default(), logger.NewNopLogger(), nil)
	return rt, reader, lock, emit
}

// drain enqueues taps, closes the reader, and runs the loop to completion —
// fully deterministic, no sleeps.
func drain(rt *Runtime, reader *drivers.MockReader, taps ...drivers.Tap) {
	for _, tp := range taps {
		reader.Tap(tp)
	}
	reader.Close()
	_ = rt.Run(context.Background())
}

func TestRuntimeGrantPulses(t *testing.T) {
	rt, reader, lock, emit := runtimeFor(t)
	drain(rt, reader, drivers.Tap{Point: "lobby-main", Credential: "CARD-001", At: ny(t, 2026, 1, 5, 9, 0)})

	if got := lock.Pulses(); len(got) != 1 || got[0] != 5 {
		t.Errorf("pulses = %v, want [5]", got)
	}
	taps := emit.taps()
	if len(taps) != 1 || !taps[0].Allow || taps[0].Reason != policy.ReasonAllowGrant || taps[0].User != "alice" {
		t.Errorf("tap events = %+v, want one allow_grant for alice", taps)
	}
	if !emit.hasSubject("acc.evt.hq.lobby-main.tap") {
		t.Errorf("missing acc.evt.hq.lobby-main.tap subject")
	}
}

func TestRuntimeScheduleClosedNoPulse(t *testing.T) {
	rt, reader, lock, emit := runtimeFor(t)
	drain(rt, reader, drivers.Tap{Point: "lobby-main", Credential: "CARD-001", At: ny(t, 2026, 1, 5, 18, 0)})

	if got := lock.Pulses(); len(got) != 0 {
		t.Errorf("pulses = %v, want none (after hours)", got)
	}
	taps := emit.taps()
	if len(taps) != 1 || taps[0].Allow || taps[0].Reason != policy.ReasonDenyScheduleClosed {
		t.Errorf("tap events = %+v, want one deny_schedule_closed", taps)
	}
}

func TestRuntimeUnknownCredNoPulse(t *testing.T) {
	rt, reader, lock, emit := runtimeFor(t)
	drain(rt, reader, drivers.Tap{Point: "lobby-main", Credential: "NOPE", At: ny(t, 2026, 1, 5, 9, 0)})

	if got := lock.Pulses(); len(got) != 0 {
		t.Errorf("pulses = %v, want none", got)
	}
	if taps := emit.taps(); len(taps) != 1 || taps[0].Reason != policy.ReasonDenyUnknownCredential {
		t.Errorf("tap events = %+v, want one deny_unknown_credential", taps)
	}
}

func TestRuntimePostureOverrideUnlocked(t *testing.T) {
	rt, reader, lock, emit := runtimeFor(t)
	// Unlock the door: an unknown credential should now pass freely.
	rt.SetPosture("lobby-main", policy.PostureUnlocked, "guard", "open house", ny(t, 2026, 1, 5, 8, 0))
	drain(rt, reader, drivers.Tap{Point: "lobby-main", Credential: "NOPE", At: ny(t, 2026, 1, 5, 9, 0)})

	if got := lock.Pulses(); len(got) != 1 || got[0] != 5 {
		t.Errorf("pulses = %v, want [5] (free passage)", got)
	}
	if taps := emit.taps(); len(taps) != 1 || !taps[0].Allow || taps[0].Reason != policy.ReasonAllowPostureUnlocked {
		t.Errorf("tap events = %+v, want one allow_posture_unlocked", taps)
	}
	if !emit.hasSubject("acc.evt.hq.lobby-main.state") {
		t.Errorf("expected a state event on posture override")
	}
}

func TestRuntimeLockdownOverride(t *testing.T) {
	rt, reader, lock, emit := runtimeFor(t)
	rt.SetPosture("lobby-main", policy.PostureLockdown, "guard", "incident", ny(t, 2026, 1, 5, 8, 0))
	drain(rt, reader, drivers.Tap{Point: "lobby-main", Credential: "CARD-001", At: ny(t, 2026, 1, 5, 9, 0)})

	if got := lock.Pulses(); len(got) != 0 {
		t.Errorf("pulses = %v, want none (lockdown)", got)
	}
	if taps := emit.taps(); len(taps) != 1 || taps[0].Allow || taps[0].Reason != policy.ReasonDenyLockdown {
		t.Errorf("tap events = %+v, want one deny_lockdown", taps)
	}
}
