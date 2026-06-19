package controller

import (
	"context"
	"testing"
	"time"

	"github.com/stone-age-io/access-control/internal/drivers"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/subjects"
)

const lobby = "lobby-main"

// alarmTypes extracts the "type" of every alarm event the emitter recorded
// (alarm payloads are map[string]any{"type":..., "ts":...}).
func alarmTypes(e *fakeEmitter) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []string
	for _, ev := range e.events {
		if mp, ok := ev.payload.(map[string]any); ok {
			if tp, ok := mp["type"].(string); ok {
				out = append(out, tp)
			}
		}
	}
	return out
}

func countAlarm(e *fakeEmitter, typ string) int {
	n := 0
	for _, a := range alarmTypes(e) {
		if a == typ {
			n++
		}
	}
	return n
}

// eventually polls cond until it holds or the timeout elapses (for the
// asynchronous DOTL timer; the rest of the state machine is synchronous).
func eventually(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

// monitorRuntime builds a runtime with a door-input source wired, for the
// run-loop wiring test. The seeded fixture binds lobby-main with held_open=30.
func monitorRuntime(t *testing.T) (*Runtime, *drivers.MockDoorInput, *fakeEmitter) {
	t.Helper()
	store := seeded(t)
	input := drivers.NewMockDoorInput(8)
	lock := drivers.NewMockLock(lobby, nil)
	emit := &fakeEmitter{}
	rt := NewRuntime("hq", store, drivers.NewMockReader(8), input,
		map[string]drivers.LockDriver{lobby: lock}, emit,
		subjects.Default(), logger.NewNopLogger(), nil)
	return rt, input, emit
}

// An open with no grant or REX is a forced door.
func TestDoorForcedOpen(t *testing.T) {
	rt, _, _, emit := runtimeFor(t)
	at := ny(t, 2026, 1, 5, 9, 0)
	rt.handleDPS(lobby, false, at) // open, unauthorized

	if got := countAlarm(emit, AlarmForced); got != 1 {
		t.Errorf("forced alarms = %d, want 1 (alarms=%v)", got, alarmTypes(emit))
	}
}

// A grant opens the authorized window, so the following door-open is not forced.
func TestDoorGrantThenOpenNotForced(t *testing.T) {
	rt, _, _, emit := runtimeFor(t)
	at := ny(t, 2026, 1, 5, 9, 0)
	rt.handleTap(drivers.Tap{Portal: lobby, Credential: "CARD-001", At: at}) // allow → grant window
	rt.handleDPS(lobby, false, at)                                           // open within window

	if got := countAlarm(emit, AlarmForced); got != 0 {
		t.Errorf("forced alarms = %d, want 0 (authorized open)", got)
	}
}

// A request-to-exit masks forced: egress is not a break-in.
func TestDoorRexMasksForced(t *testing.T) {
	rt, _, _, emit := runtimeFor(t)
	at := ny(t, 2026, 1, 5, 9, 0)
	rt.handleInput(drivers.InputEvent{Portal: lobby, Kind: drivers.InputREX, Active: true, At: at})
	rt.handleDPS(lobby, false, at)

	if got := countAlarm(emit, AlarmForced); got != 0 {
		t.Errorf("forced alarms = %d, want 0 (REX egress)", got)
	}
}

// An authorized door left open past its threshold raises held-open, and closing
// it clears the alarm.
func TestDoorHeldOpenThenClear(t *testing.T) {
	old := heldOpenUnit
	heldOpenUnit = time.Millisecond // 30 held_open_seconds -> 30ms
	defer func() { heldOpenUnit = old }()

	rt, _, _, emit := runtimeFor(t)
	at := ny(t, 2026, 1, 5, 9, 0)
	rt.handleTap(drivers.Tap{Portal: lobby, Credential: "CARD-001", At: at}) // grant
	rt.handleDPS(lobby, false, at)                                           // authorized open; arms DOTL

	eventually(t, 2*time.Second, func() bool { return countAlarm(emit, AlarmHeld) == 1 })

	rt.handleDPS(lobby, true, at.Add(time.Minute)) // close
	if got := countAlarm(emit, AlarmHeldClear); got != 1 {
		t.Errorf("held_clear alarms = %d, want 1 (alarms=%v)", got, alarmTypes(emit))
	}
	if got := countAlarm(emit, AlarmForced); got != 0 {
		t.Errorf("forced alarms = %d, want 0", got)
	}
}

// While the location's fire input is active, forced/held alarms are suppressed.
func TestDoorFireSuppressesForced(t *testing.T) {
	rt, _, _, emit := runtimeFor(t)
	at := ny(t, 2026, 1, 5, 9, 0)
	rt.SetFire("hq", true, at)
	rt.handleDPS(lobby, false, at) // unauthorized open during fire

	if got := countAlarm(emit, AlarmForced); got != 0 {
		t.Errorf("forced alarms = %d, want 0 (suppressed by fire)", got)
	}
}

// A repeated open (no intervening close) is a no-op: one forced alarm, not two.
func TestDoorDuplicateOpenIgnored(t *testing.T) {
	rt, _, _, emit := runtimeFor(t)
	at := ny(t, 2026, 1, 5, 9, 0)
	rt.handleDPS(lobby, false, at)
	rt.handleDPS(lobby, false, at.Add(time.Second)) // duplicate open

	if got := countAlarm(emit, AlarmForced); got != 1 {
		t.Errorf("forced alarms = %d, want 1 (duplicate open ignored)", got)
	}
}

// Door inputs delivered through the run loop reach the state machine.
func TestDoorInputLoopEmitsForced(t *testing.T) {
	rt, input, emit := monitorRuntime(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = rt.Run(ctx) }()

	input.Send(drivers.InputEvent{Portal: lobby, Kind: drivers.InputDPS, Closed: false, At: ny(t, 2026, 1, 5, 9, 0)})
	eventually(t, 2*time.Second, func() bool { return countAlarm(emit, AlarmForced) == 1 })
}
