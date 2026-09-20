package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// An index for the unacknowledged-alarm question, which is asked far more often
// than the events timeline is paged and was costing just as much.
//
// ui/src/utils/events.ts's unackedAlarmFilter is
//
//	(kind = "alarm" || kind = "fire") && acknowledged = false && created > cutoff
//
// and it runs on the operator console's landing page (OverviewView's headline
// count) and all over the Alarm Console — whose per-type tiles fire FIVE of these
// counts in parallel, once per load and again on every realtime burst, coalesced
// to 400ms. None of it was indexed, so a quiet building with a large history
// answered "are there any alarms?" by reading every row it had ever recorded.
//
// The column order is (kind, acknowledged, created): equality, equality, then a
// range that is also the sort (`-created`), so one seek per kind covers the
// filter, the window and the ordering with no temp B-tree. SQLite's OR
// optimization turns the two-kind disjunction into a union of two such seeks.
// `created` and not `ts` deliberately — this filter is written against `created`
// (the v1 ack-window wart), and an index has to match the query it serves, not
// the column we'd prefer it used.
//
// It is NOT a partial index, though `WHERE acknowledged = false` would be far
// smaller and looks like the obvious win. PocketBase binds filter values as query
// parameters, so the SQL reaching SQLite is `acknowledged = ?`; SQLite decides
// partial-index usability at PLAN time, when it cannot know that ? is false, so
// it can't prove the query implies the index's WHERE and silently never uses it.
// A partial index here would cost writes and buy nothing.
//
// The Alarm Console's per-type narrowing (`payload.type = "..."`) stays
// unindexable — it compiles to json_extract — but that no longer matters much: it
// now filters the handful of rows these seeks return instead of deciding which of
// 230k to read.
//
// This is the sixth index on events, which is a high-write collection (a row per
// tap and per door state change). That is a deliberate trade: the write rate of a
// physical access system is a few rows per second at its worst, while these reads
// are on the console's landing page and on a realtime-driven reconcile loop.
func init() {
	migrations.Register(func(app core.App) error {
		events, err := app.FindCollectionByNameOrId("events")
		if err != nil {
			return err
		}
		events.AddIndex("idx_events_kind_ack_created", false, "kind, acknowledged, created", "")
		return app.Save(events)
	}, func(app core.App) error {
		events, err := app.FindCollectionByNameOrId("events")
		if err != nil {
			return nil // already gone
		}
		events.RemoveIndex("idx_events_kind_ack_created")
		return app.Save(events)
	})
}
