package audit

import (
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
	"github.com/stone-age-io/access-control/internal/logger"
)

// pruneBatch bounds how many rows a single delete pass loads, so a large backlog
// can't blow up memory. events is high-volume (every tap and door state change),
// so a single batch — unlike the changelog's low-volume audit_logs — could never
// keep up with ingestion; the cron drains in batches until caught up.
const pruneBatch = 1000

// RegisterPrune wires a daily 03:00 cron that deletes events rows older than
// retentionDays. It is a no-op when retentionDays <= 0 (keep forever — the
// default), so the full projection is preserved unless an operator opts in.
//
// events is a rebuildable projection of the ACC_EVENTS JetStream stream (the
// system of record), so a prune trims a read model, not the audit trail. Safe to
// call once at startup, before serving — the cron only fires once serving.
func RegisterPrune(app core.App, retentionDays int, log *logger.Logger) {
	if retentionDays <= 0 {
		return
	}
	l := log.With("component", "audit-prune")
	app.Cron().MustAdd("events_prune", "0 3 * * *", func() {
		n, err := pruneEvents(app, retentionDays, pruneBatch, l)
		if err != nil {
			l.Error("events prune failed", "error", err)
			return
		}
		if n > 0 {
			l.Info("pruned events rows", "count", n, "olderThanDays", retentionDays)
		}
	})
}

// pruneEvents deletes events rows older than retentionDays and returns the count
// removed. It drains in bounded batches of batchSize (oldest first), reusing the
// (location, ts) index, and filters out empty ts the same way the changelog does.
// Split out from the cron closure so the drain loop is directly testable.
func pruneEvents(app core.App, retentionDays, batchSize int, log *logger.Logger) (int, error) {
	cutoff, err := types.ParseDateTime(time.Now().UTC().AddDate(0, 0, -retentionDays))
	if err != nil {
		return 0, fmt.Errorf("prune cutoff parse: %w", err)
	}
	total := 0
	for {
		old, err := app.FindRecordsByFilter("events", "ts != '' && ts < {:cutoff}", "ts", batchSize, 0, dbx.Params{"cutoff": cutoff})
		if err != nil {
			return total, fmt.Errorf("prune query: %w", err)
		}
		deleted := 0
		for _, rec := range old {
			if err := app.Delete(rec); err != nil {
				log.Error("prune delete failed", "record", rec.Id, "error", err)
				continue
			}
			deleted++
		}
		total += deleted
		// Stop when caught up (a partial batch means no more matches) or when a
		// full batch made no progress (every delete failed) — the latter guards
		// against an infinite loop on a row that won't delete.
		if len(old) < batchSize || deleted == 0 {
			break
		}
	}
	return total, nil
}
