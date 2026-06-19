package health

import (
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
