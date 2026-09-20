package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Indexes that let the events timeline be *paged* rather than re-sorted.
//
// events is the one collection that grows without bound (every tap, every door
// state change), and until now nothing indexed the way the UI actually reads it.
// The two existing indexes are idx_events_location_ts on (location, ts) and the
// partial unique idx_events_stream_seq — neither of which can serve the list
// query, which sorts `-ts,-created` with NO location clause. A composite whose
// leading column is unconstrained is unusable for an ordering on its second, so
// SQLite full-scanned the table, spilled every row into a temp B-tree to sort it,
// and threw away all but the requested page. At ~230k rows that is most of a CPU
// core per page turn — and, because the whole cost is in the sort, page 1 pays it
// exactly as dearly as page 900.
//
// All three are (filter..., ts, created): the ORDER BY is `ts DESC, created DESC`
// and both index columns are ASC, so SQLite walks the index in reverse and stops
// at the page size. Covering `created` as well as `ts` matters — with the tiebreak
// column missing, the ordering isn't fully satisfied by the index and the sort
// comes back.
//
//   - idx_events_ts_created  — the unfiltered timeline (the default view), plus
//     internal/repage's `ts` range scan and internal/audit's prune, which both
//     filter and sort on ts alone.
//   - idx_events_kind_ts     — the Kind dropdown.
//   - idx_events_source_ts   — the Source dropdown.
//
// Deliberately NOT indexed here: the date-range filter. ui/src/utils/events.ts
// emits `(ts >= X || (ts = "" && created >= X))`, and an OR across two columns
// can't use an index on either. The fix is to stop rows having an empty ts (the
// audit consumer only sets it when the message body carries one) so the clause
// can collapse to a plain `ts` range — a backfill, not an index.
//
// Note for large installs: CREATE INDEX rewrites the whole table's keys and holds
// a write lock while it does. On a few hundred thousand rows that is seconds, but
// accessd is not serving during migrate, and the audit consumer isn't running
// either (NATS resources come up only on `serve`), so nothing is racing it.
func init() {
	migrations.Register(func(app core.App) error {
		events, err := app.FindCollectionByNameOrId("events")
		if err != nil {
			return err
		}
		events.AddIndex("idx_events_ts_created", false, "ts, created", "")
		events.AddIndex("idx_events_kind_ts", false, "kind, ts, created", "")
		events.AddIndex("idx_events_source_ts", false, "source, ts, created", "")
		return app.Save(events)
	}, func(app core.App) error {
		events, err := app.FindCollectionByNameOrId("events")
		if err != nil {
			return nil // already gone
		}
		events.RemoveIndex("idx_events_ts_created")
		events.RemoveIndex("idx_events_kind_ts")
		events.RemoveIndex("idx_events_source_ts")
		return app.Save(events)
	})
}
