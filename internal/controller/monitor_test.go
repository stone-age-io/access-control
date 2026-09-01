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

// A grant nobody walks through reports no_entry once the grace window expires —
// the "access granted, no entry" case that separates a real entry from a badge
// test or a stuck strike.
func TestNoEntryAfterUnusedGrant(t *testing.T) {
	rt, _, _, emit := runtimeFor(t)
	at := ny(t, 2026, 1, 5, 9, 0)
	rt.handleTap(drivers.Tap{Portal: lobby, Credential: "CARD-001", At: at}) // grant, no open

	// Inside the grace window there is nothing to report yet.
	rt.reconcileHolds(at.Add(time.Second))
	if got := countAlarm(emit, AlarmNoEntry); got != 0 {
		t.Errorf("no_entry alarms inside the grace window = %d, want 0", got)
	}

	rt.reconcileHolds(at.Add(accessGrace + time.Second))
	if got := countAlarm(emit, AlarmNoEntry); got != 1 {
		t.Errorf("no_entry alarms = %d, want 1 (alarms=%v)", got, alarmTypes(emit))
	}

	// One alarm per grant, not one per tick.
	rt.reconcileHolds(at.Add(accessGrace + time.Minute))
	if got := countAlarm(emit, AlarmNoEntry); got != 1 {
		t.Errorf("no_entry alarms after further ticks = %d, want 1", got)
	}
}

// A grant that IS walked through reports nothing.
func TestNoEntrySilentWhenDoorOpens(t *testing.T) {
	rt, _, _, emit := runtimeFor(t)
	at := ny(t, 2026, 1, 5, 9, 0)
	rt.handleTap(drivers.Tap{Portal: lobby, Credential: "CARD-001", At: at})
	rt.handleDPS(lobby, false, at.Add(time.Second)) // authorized open

	rt.reconcileHolds(at.Add(accessGrace + time.Second))

	if got := countAlarm(emit, AlarmNoEntry); got != 0 {
		t.Errorf("no_entry alarms = %d, want 0 (the grant was used)", got)
	}
}

// A denied tap opens no grant window, so it can never produce a no_entry.
func TestNoEntrySilentAfterDeny(t *testing.T) {
	rt, _, _, emit := runtimeFor(t)
	at := ny(t, 2026, 1, 5, 9, 0)
	rt.handleTap(drivers.Tap{Portal: lobby, Credential: "NOPE-999", At: at})

	rt.reconcileHolds(at.Add(accessGrace + time.Second))

	if got := countAlarm(emit, AlarmNoEntry); got != 0 {
		t.Errorf("no_entry alarms = %d, want 0 (the tap was denied)", got)
	}
}

// A portal with no door contact can never observe an open, so it must stay silent
// rather than report no_entry on every single grant.
func TestNoEntrySilentWithoutDPS(t *testing.T) {
	rt, _, _, emit := runtimeFor(t)
	// Re-bind lobby-main with no dpsInput.
	rt.store.apply("portal."+lobby, []byte(`{"code":"`+lobby+`","type":"door","location":"hq","posture":"secure","pulseSeconds":5,"controller":"ctrl-hq-1","lockRelay":1}`))
	at := ny(t, 2026, 1, 5, 9, 0)
	rt.handleTap(drivers.Tap{Portal: lobby, Credential: "CARD-001", At: at})

	rt.reconcileHolds(at.Add(accessGrace + time.Second))

	if got := countAlarm(emit, AlarmNoEntry); got != 0 {
		t.Errorf("no_entry alarms = %d, want 0 (no DPS wired)", got)
	}
}

// A location that opts OUT of suppression (fai_suppress off) keeps alarming while
// its fire input is active. Before this the flag was mirrored to KV but never read,
// so the toggle in the UI did nothing.
func TestDoorFireSuppressOptOut(t *testing.T) {
	rt, _, _, emit := runtimeFor(t)
	rt.store.apply("location.hq", []byte(`{"code":"hq","timezone":"America/New_York","faiSuppress":false}`))
	at := ny(t, 2026, 1, 5, 9, 0)
	rt.SetFire("hq", true, at)
	rt.handleDPS(lobby, false, at) // unauthorized open during fire

	if got := countAlarm(emit, AlarmForced); got != 1 {
		t.Errorf("forced alarms = %d, want 1 (location opted out of suppression)", got)
	}
}

// A clearing event escapes fire suppression. Without the exemption, a door that
// was held-open-alarming when the fire input asserted strands an unresolved alarm
// on the console: point_status recovers on the close, but no later event emits the
// clear.
func TestDoorHeldClearEscapesFireSuppression(t *testing.T) {
	old := heldOpenUnit
	heldOpenUnit = time.Millisecond // 30 held_open_seconds -> 30ms
	defer func() { heldOpenUnit = old }()

	rt, _, _, emit := runtimeFor(t)
	at := ny(t, 2026, 1, 5, 9, 0)
	rt.handleTap(drivers.Tap{Portal: lobby, Credential: "CARD-001", At: at}) // grant
	rt.handleDPS(lobby, false, at)                                           // authorized open; arms DOTL
	eventually(t, 2*time.Second, func() bool { return countAlarm(emit, AlarmHeld) == 1 })

	rt.SetFire("hq", true, at)                     // evacuation begins mid-alarm
	rt.handleDPS(lobby, true, at.Add(time.Minute)) // door closes

	if got := countAlarm(emit, AlarmHeldClear); got != 1 {
		t.Errorf("held_clear alarms = %d, want 1 while fire active (alarms=%v)", got, alarmTypes(emit))
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

// A controller with no door-input driver can never observe an open, so an unused
// grant must stay silent — even though the portal's policy binding declares a DPS.
//
// Those two facts are independent, and they diverge in exactly one place: the
// mock driver, which has a lock and no inputs, and which is what every demo, dev
// box and `reader: nats` simulation runs. sweepNoEntry checked only the binding,
// so on that setup every single granted tap produced a no_entry alarm 10-20s
// later. They land unacknowledged in the Alarm Console, so a demo staged to show
// a forced door buried it under a few hundred an hour of "access granted, nobody
// came through" — from doors nobody had touched.
//
// The companion is TestNoEntryAfterUnusedGrant, which asserts the alarm DOES fire
// when a door input is present. Together they pin that no_entry depends on both
// halves being true, which is the property the fix is about.
func TestNoEntrySilentWithoutADoorInputDriver(t *testing.T) {
	rt, _, emit := runtimeWithoutDoorInput(t)
	at := ny(t, 2026, 1, 5, 9, 0)

	// lobby-main's binding DOES declare a dpsInput; the driver is what is absent.
	if b, ok := rt.store.Binding(lobby); !ok || b.DpsInput == 0 {
		t.Fatal("fixture changed: this test needs a portal whose binding declares a DPS")
	}

	rt.handleTap(drivers.Tap{Portal: lobby, Credential: "CARD-001", At: at})
	rt.reconcileHolds(at.Add(accessGrace + time.Second))
	rt.reconcileHolds(at.Add(accessGrace + time.Minute))

	if got := countAlarm(emit, AlarmNoEntry); got != 0 {
		t.Errorf("no_entry alarms = %d, want 0 (no door-input driver: an open is unobservable)", got)
	}
}
