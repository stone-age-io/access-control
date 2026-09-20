package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// An index for the denied-access report, whose filter is `kind = "tap" &&
// allow = false` sorted `-ts,-created`.
//
// idx_events_kind_ts (kind, ts, created) already serves the `kind` half, but
// `allow` is not in it — so SQLite seeks to the taps and then has to fetch the
// TABLE ROW for every one of them just to test a boolean. Taps are the dominant
// event kind by a wide margin, so "denials in the last week" was reading most of
// the table to find a few thousand rows.
//
// It reads it repeatedly, too. The report uses getFullList, which pages with
// LIMIT/OFFSET (batch 500, and 500 is PocketBase's per-page ceiling), and an
// OFFSET has to re-walk everything before it. Eight batches over an unindexed
// predicate means eight passes, each longer than the last.
//
// (kind, allow, ts, created) collapses both problems: the seek lands directly on
// the denials, `allow` is answered from the index with no row fetch, and the
// OFFSET walk crosses only matching entries — thousands rather than hundreds of
// thousands.
//
// This does NOT replace idx_events_kind_ts, which has to stay. The events
// timeline filters `kind` alone and sorts on ts; with `allow` sitting between
// them in the key, entries are ordered by (allow, ts) within a kind, so that
// index cannot satisfy the timeline's ORDER BY and its sort would come back.
// The two answer genuinely different questions.
//
// That makes five indexes on events, which is a lot for a collection taking a
// row per tap. Weighed and accepted: a B-tree insert is a fixed small cost on a
// write path that runs a few times per second at worst, and the alternative is
// an operator-facing report that reads the whole table.
//
// What this still does not fix is the tail. The report's date range compiles to
// `(ts >= X || (ts = "" && created >= X))` — an OR across two columns, so SQLite
// cannot seek to the window's edge or know when it has passed it, and the last
// batch walks every older denial to return nothing. That set is now denials
// rather than taps, which is why this is worth doing on its own, but the clean
// fix is still to stop `ts` being nullable so the clause can collapse to a plain
// range.
func init() {
	migrations.Register(func(app core.App) error {
		events, err := app.FindCollectionByNameOrId("events")
		if err != nil {
			return err
		}
		events.AddIndex("idx_events_kind_allow_ts", false, "kind, allow, ts, created", "")
		return app.Save(events)
	}, func(app core.App) error {
		events, err := app.FindCollectionByNameOrId("events")
		if err != nil {
			return nil // already gone
		}
		events.RemoveIndex("idx_events_kind_allow_ts")
		return app.Save(events)
	})
}
