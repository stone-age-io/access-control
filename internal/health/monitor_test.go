package health

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/subjects"

	// Side-effect import registers the schema (controllers collection) + fixture
	// (seeds controller ctrl-hq-1).
	_ "github.com/stone-age-io/access-control/pbmigrations"
)

const ctrl = "ctrl-hq-1"

func newMonitor(t *testing.T, offlineAfter time.Duration) (*Monitor, *tests.TestApp) {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(app.Cleanup)
	// nc is nil: the tests drive markOnline/sweep directly, never Start (which
	// would need a NATS connection).
	return New(app, nil, subjects.Default(), offlineAfter, logger.NewNopLogger(), nil), app
}

func reload(t *testing.T, app core.App, code string) *core.Record {
	t.Helper()
	rec, err := app.FindFirstRecordByData("controllers", "code", code)
	if err != nil {
		t.Fatalf("reload %q: %v", code, err)
	}
	return rec
}

// capturedEvent is one liveness event recorded by the test publisher.
type capturedEvent struct {
	subject string
	body    map[string]any
}

// capture swaps in a recording publisher (New leaves publish nil for a nil nc) and
// returns the slice it appends to.
func capture(mon *Monitor) *[]capturedEvent {
	got := &[]capturedEvent{}
	mon.publish = func(subject string, data []byte) error {
		var body map[string]any
		_ = json.Unmarshal(data, &body)
		*got = append(*got, capturedEvent{subject: subject, body: body})
		return nil
	}
	return got
}

// Going online emits exactly one liveness event, on the ctrl evt.state subject.
// Subsequent heartbeats are not transitions and emit nothing — otherwise every
// beat would land in the audit log, which is precisely what heartbeats stay off
// the stream to avoid.
func TestLivenessOnlineEmitsOncePerTransition(t *testing.T) {
	mon, _ := newMonitor(t, time.Hour)
	got := capture(mon)

	for i := 0; i < 3; i++ {
		if err := mon.markOnline(ctrl); err != nil {
			t.Fatalf("markOnline: %v", err)
		}
	}

	if len(*got) != 1 {
		t.Fatalf("liveness events = %d, want 1 (only the transition)", len(*got))
	}
	ev := (*got)[0]
	if want := "acc.hq.ctrl." + ctrl + ".evt.state"; ev.subject != want {
		t.Errorf("subject = %q, want %q", ev.subject, want)
	}
	if ev.body["status"] != "online" {
		t.Errorf("status = %v, want online", ev.body["status"])
	}
}

// Going offline emits one event; a settled offline box never re-emits, because the
// sweep only queries records currently "online".
func TestLivenessOfflineEmitsOncePerTransition(t *testing.T) {
	mon, app := newMonitor(t, time.Millisecond)
	if err := mon.markOnline(ctrl); err != nil {
		t.Fatalf("markOnline: %v", err)
	}
	got := capture(mon) // capture AFTER the online transition, so only the flip lands
	time.Sleep(5 * time.Millisecond)

	mon.sweep()
	mon.sweep() // second sweep: already offline, nothing to transition

	if len(*got) != 1 {
		t.Fatalf("liveness events = %d, want 1 (only the transition)", len(*got))
	}
	if s := (*got)[0].body["status"]; s != "offline" {
		t.Errorf("status = %v, want offline", s)
	}
	if got := reload(t, app, ctrl).GetString("status"); got != statusOffline {
		t.Errorf("record status = %q, want offline", got)
	}
}

// A controller whose location relation is unset can't be addressed on the wire.
// The record write still stands; only the event is skipped.
func TestLivenessSkippedWithoutLocation(t *testing.T) {
	mon, app := newMonitor(t, time.Hour)
	got := capture(mon)

	rec := reload(t, app, ctrl)
	rec.Set("location", "")
	// Saving through the app would trip the required-relation validation, so clear
	// it on the in-memory record and emit directly — this exercises emitLiveness's
	// guard, which is the unit under test.
	mon.emitLiveness(rec, statusOnline)

	if len(*got) != 0 {
		t.Errorf("liveness events = %d, want 0 (unresolvable location)", len(*got))
	}
}

// A heartbeat stamps last_seen and flips the controller online.
func TestMarkOnline(t *testing.T) {
	mon, app := newMonitor(t, time.Hour)
	if err := mon.markOnline(ctrl); err != nil {
		t.Fatalf("markOnline: %v", err)
	}
	rec := reload(t, app, ctrl)
	if got := rec.GetString("status"); got != statusOnline {
		t.Errorf("status = %q, want online", got)
	}
	if rec.GetDateTime("last_seen").Time().IsZero() {
		t.Error("last_seen not set after heartbeat")
	}
}

// A heartbeat from an unregistered controller is ignored, never auto-created.
func TestMarkOnlineUnknown(t *testing.T) {
	mon, app := newMonitor(t, time.Hour)
	if err := mon.markOnline("ctrl-ghost"); err != nil {
		t.Errorf("markOnline(unknown) = %v, want nil (skipped)", err)
	}
	if _, err := app.FindFirstRecordByData("controllers", "code", "ctrl-ghost"); err == nil {
		t.Error("unknown controller was created; want it skipped")
	}
}

// The sweep flips an online controller offline once its heartbeat is stale.
func TestSweepMarksOffline(t *testing.T) {
	mon, app := newMonitor(t, time.Millisecond)
	if err := mon.markOnline(ctrl); err != nil {
		t.Fatalf("markOnline: %v", err)
	}
	time.Sleep(10 * time.Millisecond) // age last_seen past the 1ms threshold
	mon.sweep()

	if got := reload(t, app, ctrl).GetString("status"); got != statusOffline {
		t.Errorf("status after sweep = %q, want offline", got)
	}
}

// A fresh heartbeat survives the sweep.
func TestSweepKeepsFresh(t *testing.T) {
	mon, app := newMonitor(t, time.Hour)
	if err := mon.markOnline(ctrl); err != nil {
		t.Fatalf("markOnline: %v", err)
	}
	mon.sweep()

	if got := reload(t, app, ctrl).GetString("status"); got != statusOnline {
		t.Errorf("status after sweep = %q, want online (fresh)", got)
	}
}
