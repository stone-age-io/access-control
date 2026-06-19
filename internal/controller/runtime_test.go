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
	rt := NewRuntime("hq", store, reader, nil,
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
	drain(rt, reader, drivers.Tap{Portal: "lobby-main", Credential: "CARD-001", At: ny(t, 2026, 1, 5, 9, 0)})

	if got := lock.Pulses(); len(got) != 1 || got[0] != 5 {
		t.Errorf("pulses = %v, want [5]", got)
	}
	taps := emit.taps()
	if len(taps) != 1 || !taps[0].Allow || taps[0].Reason != policy.ReasonAllowGrant || taps[0].User != "alice" {
		t.Errorf("tap events = %+v, want one allow_grant for alice", taps)
	}
	if !emit.hasSubject("acc.hq.door.lobby-main.evt.tap") {
		t.Errorf("missing acc.hq.door.lobby-main.evt.tap subject")
	}
}

func TestRuntimeScheduleClosedNoPulse(t *testing.T) {
	rt, reader, lock, emit := runtimeFor(t)
	drain(rt, reader, drivers.Tap{Portal: "lobby-main", Credential: "CARD-001", At: ny(t, 2026, 1, 5, 18, 0)})

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
	drain(rt, reader, drivers.Tap{Portal: "lobby-main", Credential: "NOPE", At: ny(t, 2026, 1, 5, 9, 0)})

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
	drain(rt, reader, drivers.Tap{Portal: "lobby-main", Credential: "NOPE", At: ny(t, 2026, 1, 5, 9, 0)})

	if got := lock.Pulses(); len(got) != 1 || got[0] != 5 {
		t.Errorf("pulses = %v, want [5] (free passage)", got)
	}
	if taps := emit.taps(); len(taps) != 1 || !taps[0].Allow || taps[0].Reason != policy.ReasonAllowPostureUnlocked {
		t.Errorf("tap events = %+v, want one allow_posture_unlocked", taps)
	}
	if !emit.hasSubject("acc.hq.door.lobby-main.evt.state") {
		t.Errorf("expected a state event on posture override")
	}
}

// Locks are armed/disarmed at runtime by the portal reconciler. A grant before a
// lock is armed decides allow but can't pulse; once SetLock arms it, the same tap
// pulses; DeleteLock disarms it again.
func TestRuntimeDynamicLockArming(t *testing.T) {
	store := seeded(t)
	emit := &fakeEmitter{}
	rt := NewRuntime("hq", store, drivers.NewMockReader(8), nil, nil, emit,
		subjects.Default(), logger.NewNopLogger(), nil)

	if rt.drives("lobby-main") {
		t.Fatal("drives lobby-main before any lock armed")
	}

	lock := drivers.NewMockLock("lobby-main", nil)
	rt.SetLock("lobby-main", lock)
	if !rt.drives("lobby-main") {
		t.Fatal("does not drive lobby-main after SetLock")
	}

	rt.handleTap(drivers.Tap{Portal: "lobby-main", Credential: "CARD-001", At: ny(t, 2026, 1, 5, 9, 0)})
	if got := lock.Pulses(); len(got) != 1 || got[0] != 5 {
		t.Errorf("pulses after arming = %v, want [5]", got)
	}

	rt.DeleteLock("lobby-main")
	if rt.drives("lobby-main") {
		t.Error("still drives lobby-main after DeleteLock")
	}
}

// A controller hears location-wildcarded commands for portals other controllers
// drive; it must ignore them — no override stored, no state event emitted (which
// would duplicate the owning controller's audit row).
func TestRuntimeIgnoresCommandsForUndrivenPortal(t *testing.T) {
	rt, _, _, emit := runtimeFor(t) // drives only lobby-main
	at := ny(t, 2026, 1, 5, 8, 0)

	rt.SetPosture("side-gate", policy.PostureLockdown, "guard", "incident", at)
	rt.ClearPosture("side-gate", "guard", "all clear", at)
	rt.Unlock("side-gate", 5, "guard", "buzz in")

	if len(emit.events) != 0 {
		t.Errorf("emitted %d events for an undriven portal, want 0: %+v", len(emit.events), emit.events)
	}
	rt.mu.RLock()
	_, has := rt.overrides["side-gate"]
	rt.mu.RUnlock()
	if has {
		t.Errorf("stored an override for an undriven portal")
	}
}

func TestRuntimeLockdownOverride(t *testing.T) {
	rt, reader, lock, emit := runtimeFor(t)
	rt.SetPosture("lobby-main", policy.PostureLockdown, "guard", "incident", ny(t, 2026, 1, 5, 8, 0))
	drain(rt, reader, drivers.Tap{Portal: "lobby-main", Credential: "CARD-001", At: ny(t, 2026, 1, 5, 9, 0)})

	if got := lock.Pulses(); len(got) != 0 {
		t.Errorf("pulses = %v, want none (lockdown)", got)
	}
	if taps := emit.taps(); len(taps) != 1 || taps[0].Allow || taps[0].Reason != policy.ReasonDenyLockdown {
		t.Errorf("tap events = %+v, want one deny_lockdown", taps)
	}
}
