package audit

import (
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/types"
	"github.com/stone-age-io/access-control/internal/logger"

	// Side-effect import applies the schema (incl. the events collection) when the
	// test app runs migrations.
	_ "github.com/stone-age-io/access-control/pbmigrations"
)

func newTestApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(app.Cleanup)
	return app
}

// seedEvent inserts an events row with the given ts (a programmatic save, which
// bypasses collection rules). A blank ts argument leaves the ts field empty.
func seedEvent(t *testing.T, app core.App, ts string) {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("events")
	if err != nil {
		t.Fatalf("events collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("kind", "tap")
	rec.Set("location", "hq")
	if ts != "" {
		rec.Set("ts", ts)
	}
	if err := app.Save(rec); err != nil {
		t.Fatalf("seed event: %v", err)
	}
}

func daysAgo(t *testing.T, n int) string {
	t.Helper()
	dt, err := types.ParseDateTime(time.Now().UTC().AddDate(0, 0, -n))
	if err != nil {
		t.Fatalf("ParseDateTime: %v", err)
	}
	return dt.String()
}

func eventCount(t *testing.T, app core.App) int {
	t.Helper()
	rows, err := app.FindAllRecords("events")
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	return len(rows)
}

// Prune deletes only rows older than the retention window, drains across batch
// boundaries (batchSize 2 over 3 deletable rows exercises the loop), and never
// touches rows with an empty ts.
func TestPruneEventsByAge(t *testing.T) {
	app := newTestApp(t)

	seedEvent(t, app, daysAgo(t, 400)) // delete
	seedEvent(t, app, daysAgo(t, 200)) // delete
	seedEvent(t, app, daysAgo(t, 100)) // delete
	seedEvent(t, app, daysAgo(t, 10))  // keep (within 30d)
	seedEvent(t, app, daysAgo(t, 1))   // keep (within 30d)
	seedEvent(t, app, "")              // keep (empty ts is never pruned)

	if got := eventCount(t, app); got != 6 {
		t.Fatalf("seeded event count = %d, want 6", got)
	}

	n, err := pruneEvents(app, 30, 2, logger.NewNopLogger())
	if err != nil {
		t.Fatalf("pruneEvents: %v", err)
	}
	if n != 3 {
		t.Errorf("pruned count = %d, want 3", n)
	}
	if got := eventCount(t, app); got != 3 {
		t.Errorf("remaining event count = %d, want 3", got)
	}
}

// With nothing past the cutoff, the prune is a clean no-op (and the drain loop
// terminates on the first, empty batch rather than spinning).
func TestPruneEventsNothingOld(t *testing.T) {
	app := newTestApp(t)

	seedEvent(t, app, daysAgo(t, 5))
	seedEvent(t, app, daysAgo(t, 1))

	n, err := pruneEvents(app, 30, pruneBatch, logger.NewNopLogger())
	if err != nil {
		t.Fatalf("pruneEvents: %v", err)
	}
	if n != 0 {
		t.Errorf("pruned count = %d, want 0", n)
	}
	if got := eventCount(t, app); got != 2 {
		t.Errorf("remaining event count = %d, want 2", got)
	}
}
