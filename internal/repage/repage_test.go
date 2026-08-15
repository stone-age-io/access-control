package repage

import (
	"errors"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/types"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/notify"

	// Side-effect import registers the schema (events collection + repage_count).
	_ "github.com/stone-age-io/access-control/pbmigrations"
)

// now is the instant every test sweeps at; event timestamps are placed relative to
// it so the age window is explicit at each call site.
var now = time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)

// newSweeper builds a sweeper over a test app whose SendFunc records the events it
// is handed and reports them sent.
func newSweeper(t *testing.T) (*Sweeper, *tests.TestApp, *[]notify.Event) {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(app.Cleanup)

	got := &[]notify.Event{}
	send := func(ev notify.Event) (bool, error) {
		*got = append(*got, ev)
		return true, nil
	}
	return New(app, send, logger.NewNopLogger()), app, got
}

// seedAlarm writes one alarm events row, aged by `age` before `now`.
func seedAlarm(t *testing.T, app core.App, alarmType string, age time.Duration, acked bool) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("events")
	if err != nil {
		t.Fatalf("events collection: %v", err)
	}
	ts, err := types.ParseDateTime(now.Add(-age))
	if err != nil {
		t.Fatalf("ParseDateTime: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("location", "hq")
	rec.Set("portal", "lobby-main")
	rec.Set("type", "door")
	rec.Set("kind", "alarm")
	rec.Set("ts", ts)
	rec.Set("acknowledged", acked)
	rec.Set("payload", map[string]any{"type": alarmType, "ts": ts.String()})
	if err := app.Save(rec); err != nil {
		t.Fatalf("save event: %v", err)
	}
	return rec
}

// An urgent alarm left unacknowledged past the threshold is re-paged, and the
// attempt is recorded on the row so the bound survives a restart.
func TestSweepRepagesUnacknowledged(t *testing.T) {
	s, app, got := newSweeper(t)
	rec := seedAlarm(t, app, notify.TypeForced, repageAfter+time.Minute, false)

	s.sweep(now)

	if len(*got) != 1 {
		t.Fatalf("re-pages = %d, want 1", len(*got))
	}
	if ev := (*got)[0]; ev.Repage != 1 || ev.NotifyType() != notify.TypeForced {
		t.Errorf("event = repage %d / type %q, want 1 / forced", ev.Repage, ev.NotifyType())
	}
	reloaded, err := app.FindRecordById("events", rec.Id)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if n := reloaded.GetInt("repage_count"); n != 1 {
		t.Errorf("repage_count = %d, want 1", n)
	}
}

// An acknowledged alarm is never re-paged — that is the whole point of the ack.
func TestSweepSkipsAcknowledged(t *testing.T) {
	s, app, got := newSweeper(t)
	seedAlarm(t, app, notify.TypeForced, repageAfter+time.Minute, true)

	s.sweep(now)

	if len(*got) != 0 {
		t.Errorf("re-pages = %d, want 0 (acknowledged)", len(*got))
	}
}

// An alarm younger than the threshold is left alone, so an operator who acts on the
// original page is never paged twice.
func TestSweepSkipsFreshAlarm(t *testing.T) {
	s, app, got := newSweeper(t)
	seedAlarm(t, app, notify.TypeForced, time.Minute, false)

	s.sweep(now)

	if len(*got) != 0 {
		t.Errorf("re-pages = %d, want 0 (too fresh)", len(*got))
	}
}

// held and no_entry are the high-volume diagnostics: re-paging them is how a
// reminder feature becomes the reason notifications get switched off.
func TestSweepSkipsNonUrgentTypes(t *testing.T) {
	for _, alarmType := range []string{notify.TypeHeld, notify.TypeNoEntry, "held_clear"} {
		s, app, got := newSweeper(t)
		seedAlarm(t, app, alarmType, repageAfter+time.Minute, false)

		s.sweep(now)

		if len(*got) != 0 {
			t.Errorf("%s: re-pages = %d, want 0 (not urgent)", alarmType, len(*got))
		}
	}
}

// Reminders are bounded: a permanently unacknowledged alarm stops nagging.
func TestSweepStopsAtMaxRepages(t *testing.T) {
	s, app, got := newSweeper(t)
	seedAlarm(t, app, notify.TypeIntrusion, repageAfter+time.Minute, false)

	for i := 0; i < maxRepages+3; i++ {
		s.sweep(now)
	}

	if len(*got) != maxRepages {
		t.Errorf("re-pages = %d, want %d (capped)", len(*got), maxRepages)
	}
}

// An alarm older than the lookback window is not picked up, so switching this on
// does not page about a backlog from months ago.
func TestSweepIgnoresAncientAlarms(t *testing.T) {
	s, app, got := newSweeper(t)
	seedAlarm(t, app, notify.TypeForced, lookback+time.Hour, false)

	s.sweep(now)

	if len(*got) != 0 {
		t.Errorf("re-pages = %d, want 0 (outside lookback)", len(*got))
	}
}

// A send failure leaves the count untouched so the next sweep retries, rather than
// consuming one of the two reminders on an SMTP outage.
func TestSweepRetriesAfterSendFailure(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(app.Cleanup)

	var calls int
	send := func(notify.Event) (bool, error) {
		calls++
		if calls == 1 {
			return false, errors.New("smtp down")
		}
		return true, nil
	}
	s := New(app, send, logger.NewNopLogger())
	rec := seedAlarm(t, app, notify.TypeForced, repageAfter+time.Minute, false)

	s.sweep(now)
	reloaded, _ := app.FindRecordById("events", rec.Id)
	if n := reloaded.GetInt("repage_count"); n != 0 {
		t.Fatalf("repage_count after failure = %d, want 0 (must retry)", n)
	}

	s.sweep(now)
	if calls != 2 {
		t.Errorf("send calls = %d, want 2", calls)
	}
	reloaded, _ = app.FindRecordById("events", rec.Id)
	if n := reloaded.GetInt("repage_count"); n != 1 {
		t.Errorf("repage_count after success = %d, want 1", n)
	}
}

// Nobody opted in is not a failure: the attempt still counts, so an alarm whose
// source or recipients are switched off is not re-examined every sweep forever.
func TestSweepCountsAttemptWhenNobodyOptedIn(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(app.Cleanup)

	send := func(notify.Event) (bool, error) { return false, nil } // opted out
	s := New(app, send, logger.NewNopLogger())
	rec := seedAlarm(t, app, notify.TypeForced, repageAfter+time.Minute, false)

	s.sweep(now)

	reloaded, _ := app.FindRecordById("events", rec.Id)
	if n := reloaded.GetInt("repage_count"); n != 1 {
		t.Errorf("repage_count = %d, want 1 (attempt counted even when unsent)", n)
	}
}
