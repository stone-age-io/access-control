package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Indexes for the control-plane collections, on the same principle as 1750000045:
// index the shape the UI actually reads, and end every composite with the sort's
// tiebreak column so the ORDER BY is satisfied outright instead of coming back as
// a temp B-tree.
//
// audit_logs is the one that matters. It is the other collection that grows
// without bound — a row per API-driven policy edit and per operator login — and
// it had exactly the bug events did. Its list view sorts `-timestamp,-created`
// and filters `event_type`, yet event_type had no index at all and
// idx_audit_logs_timestamp covers only `timestamp`, missing the tiebreak. It is
// also far more expensive per row than events: `before` and `after` are JSON
// fields with a 1MB ceiling each, so the table is very wide and a scan of it
// touches many more pages.
//
// credentials.user is a required relation that nothing indexed, filtered on five
// paths: the cardholder detail page, the roster's pass-state lookup (which builds
// a 25-way `user = ... || user = ...` OR from the loaded page, so one index turns
// one scan into 25 seeks), internal/badgeapi's visitor revoke, internal/badgesweep
// INSIDE its per-visitor loop, and the 1750000036 cascade delete. Both UI callers
// sort `-created`.
//
// cardholders gets the two shapes its list view actually has. Note that only one
// of them is a seek: the default roster is `kind != "visitor"`, and a `!=` can't
// seek, so idx_cardholders_name is there for the SORT (there was no index on
// `name` at all, so the default view sorted the whole table on every page). The
// Visitors view is a real equality on `kind` with a `-created` sort, which
// idx_cardholders_kind_created serves end to end. Its free-text search is `~`
// (LIKE %q%) and is unindexable by nature — that is not a gap to be closed.
//
// The audit_logs side also DROPS three indexes; see the `dead` list below.
//
// Deliberately NOT indexed: point_status.kind, despite `kind = "portal"` /
// `kind = "area"` appearing on seven query paths including the Overview landing
// page. That table holds one row per physical point — hundreds, and a few
// thousand on a large campus — and SQLite scans that in less time than it takes
// to consult an index. An index there would be cargo cult: real write cost, no
// measurable read win. Revisit only if a site's point count ever reaches the tens
// of thousands, which would mean something else has gone wrong.
func init() {
	// A slice, not a map, so the order is the same on every run.
	type collectionIndexes struct {
		collection string
		indexes    [][2]string // {name, columns}
	}
	plan := []collectionIndexes{
		{"audit_logs", [][2]string{
			{"idx_audit_logs_timestamp_created", "timestamp, created"},
			{"idx_audit_logs_type_timestamp", "event_type, timestamp, created"},
		}},
		{"credentials", [][2]string{
			{"idx_credentials_user_created", "user, created"},
		}},
		{"cardholders", [][2]string{
			{"idx_cardholders_name", "name"},
			{"idx_cardholders_kind_created", "kind, created"},
		}},
	}

	// The other half of the same job: audit_logs was indexed on three columns
	// that nothing reads. collection_name, record_id and actor_id are written by
	// internal/changelog on every policy edit and operator login, and no query
	// anywhere filters or sorts on them — not the list view (which only knows
	// event_type), not internal/changelog's own prune (timestamp), not any Go
	// caller. So they were paid for on every write and never earned it back,
	// while the one column the UI does filter had no index at all.
	//
	// Dropping them is safe to reverse — the down migration puts them back — and
	// cheap to redo if a "history for this record" view ever wants one. An index
	// is a claim about how the data is read; these were making a claim nothing
	// backed up.
	dead := []struct{ name, cols string }{
		{"idx_audit_logs_collection", "collection_name"},
		{"idx_audit_logs_record", "record_id"},
		{"idx_audit_logs_actor", "actor_id"},
	}

	migrations.Register(func(app core.App) error {
		for _, p := range plan {
			c, err := app.FindCollectionByNameOrId(p.collection)
			if err != nil {
				return err
			}
			for _, idx := range p.indexes {
				c.AddIndex(idx[0], false, idx[1], "")
			}
			if p.collection == "audit_logs" {
				for _, idx := range dead {
					c.RemoveIndex(idx.name)
				}
			}
			if err := app.Save(c); err != nil {
				return err
			}
		}
		return nil
	}, func(app core.App) error {
		for _, p := range plan {
			c, err := app.FindCollectionByNameOrId(p.collection)
			if err != nil {
				continue // already gone
			}
			for _, idx := range p.indexes {
				c.RemoveIndex(idx[0])
			}
			if p.collection == "audit_logs" {
				for _, idx := range dead {
					c.AddIndex(idx.name, false, idx.cols, "")
				}
			}
			if err := app.Save(c); err != nil {
				return err
			}
		}
		return nil
	})
}
