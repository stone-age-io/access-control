// Package changelog records the control-plane audit trail: who changed which
// policy record, when, and from where. It is the operator-edit counterpart to
// internal/audit (the JetStream→events consumer, which records door activity);
// the two are complementary and disjoint.
//
// It binds PocketBase's *Request hooks, which fire only for API-driven record
// operations. accessd's own programmatic app.Save() writes — controller heartbeats
// (health), the events/point_status projections, the KV mirror — never trigger
// these hooks, so machine churn is excluded by construction and `controllers`
// config edits can be audited safely. Every row carries the authenticated actor
// (operator or superuser), the before/after field snapshots, and the request
// origin. Audit writes happen after the operation commits and never block it.
package changelog

import (
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
	"github.com/stone-age-io/access-control/internal/authz"
	"github.com/stone-age-io/access-control/internal/logger"
)

// collection is the audit sink created by migration 1750000009.
const collection = "audit_logs"

// audited is the allowlist of control-plane collections whose API-driven changes
// are recorded. Deliberately excludes the machine-written projections (events,
// point_status) and the audit sink itself. `controllers` is safe to include
// because heartbeat updates go through app.Save (no *Request hook), not the API.
var audited = []string{
	"cardholders", "credentials", "holidays",
	"locations", "schedules", "controllers", "portals",
	"access_groups", "roles", "aux_input", "aux_output", "users",
}

type recorder struct {
	app core.App
	log *logger.Logger
}

// Register binds the audit hooks (and, when retentionDays > 0, a daily pruning
// cron). Safe to call once at startup, before serving; the hooks only fire for
// API requests, which can only arrive once serving.
func Register(app core.App, retentionDays int, log *logger.Logger) {
	r := &recorder{app: app, log: log.With("component", "changelog")}

	// Privilege-escalation guard: only a superuser or an operator holding the
	// `operators` capability may change a user's permissions. Registered before
	// the audit handler so it runs first in the chain.
	app.OnRecordUpdateRequest("users").BindFunc(func(e *core.RecordRequestEvent) error {
		privileged := e.Auth != nil && (e.Auth.IsSuperuser() || authz.HasCapability(e.Auth, authz.CapOperators))
		if !privileged && !equalStringSet(e.Record.GetStringSlice("permissions"), e.Record.Original().GetStringSlice("permissions")) {
			return e.ForbiddenError("only an operator with the operators capability may change a user's permissions", nil)
		}
		return e.Next()
	})

	app.OnRecordCreateRequest(audited...).BindFunc(func(e *core.RecordRequestEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		r.write(e.RequestEvent, "create", e.Collection.Name, e.Record.Id, nil, snapshot(e.Record))
		return nil
	})

	app.OnRecordUpdateRequest(audited...).BindFunc(func(e *core.RecordRequestEvent) error {
		before := snapshot(e.Record.Original())
		if err := e.Next(); err != nil {
			return err
		}
		r.write(e.RequestEvent, "update", e.Collection.Name, e.Record.Id, before, snapshot(e.Record))
		return nil
	})

	app.OnRecordDeleteRequest(audited...).BindFunc(func(e *core.RecordRequestEvent) error {
		before := snapshot(e.Record)
		id := e.Record.Id
		col := e.Collection.Name
		if err := e.Next(); err != nil {
			return err
		}
		r.write(e.RequestEvent, "delete", col, id, before, nil)
		return nil
	})

	// Operator logins (skip superusers — their auth goes through _superusers).
	app.OnRecordAuthRequest("users").BindFunc(func(e *core.RecordAuthRequestEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		if e.Record != nil && !e.Record.IsSuperuser() {
			r.write(e.RequestEvent, "auth", e.Collection.Name, e.Record.Id, nil, nil)
		}
		return nil
	})

	if retentionDays > 0 {
		r.registerPrune(retentionDays)
	}
}

// write inserts one audit row. Fail-safe: the underlying operation has already
// committed, so an audit error is logged and swallowed, never propagated.
func (r *recorder) write(req *core.RequestEvent, eventType, colName, recordID string, before, after map[string]any) {
	col, err := r.app.FindCollectionByNameOrId(collection)
	if err != nil {
		r.log.Error("audit sink unavailable", "error", err)
		return
	}
	rec := core.NewRecord(col)
	rec.Set("event_type", eventType)
	rec.Set("collection_name", colName)
	rec.Set("record_id", recordID)
	if req != nil {
		if actor := req.Auth; actor != nil {
			rec.Set("actor_id", actor.Id)
			rec.Set("actor_email", actor.Email())
			rec.Set("actor_collection", actor.Collection().Name)
		}
		rec.Set("request_ip", req.RealIP())
		if req.Request != nil {
			rec.Set("request_method", req.Request.Method)
			rec.Set("request_url", req.Request.URL.Path)
		}
	}
	rec.Set("timestamp", types.NowDateTime())
	if before != nil {
		rec.Set("before", before)
	}
	if after != nil {
		rec.Set("after", after)
	}
	if err := r.app.Save(rec); err != nil {
		r.log.Error("failed to write audit row", "collection", colName, "record", recordID, "error", err)
	}
}

// registerPrune wires a daily 03:00 cron that deletes audit rows older than
// retentionDays, in bounded batches so a large backlog can't blow up memory.
func (r *recorder) registerPrune(retentionDays int) {
	r.app.Cron().MustAdd("changelog_prune", "0 3 * * *", func() {
		cutoff, err := types.ParseDateTime(time.Now().UTC().AddDate(0, 0, -retentionDays))
		if err != nil {
			r.log.Error("prune cutoff parse failed", "error", err)
			return
		}
		old, err := r.app.FindRecordsByFilter(collection, "timestamp != '' && timestamp < {:cutoff}", "timestamp", 1000, 0, dbx.Params{"cutoff": cutoff})
		if err != nil {
			r.log.Error("prune query failed", "error", err)
			return
		}
		for _, rec := range old {
			if err := r.app.Delete(rec); err != nil {
				r.log.Error("prune delete failed", "record", rec.Id, "error", err)
			}
		}
		if len(old) > 0 {
			r.log.Info("pruned audit rows", "count", len(old), "olderThanDays", retentionDays)
		}
	})
}

// equalStringSet reports whether two string slices contain the same elements,
// ignoring order and duplicates (multi-select values have no meaningful order).
func equalStringSet(a, b []string) bool {
	seen := make(map[string]struct{}, len(a))
	for _, s := range a {
		seen[s] = struct{}{}
	}
	bset := make(map[string]struct{}, len(b))
	for _, s := range b {
		bset[s] = struct{}{}
		if _, ok := seen[s]; !ok {
			return false
		}
	}
	for s := range seen {
		if _, ok := bset[s]; !ok {
			return false
		}
	}
	return true
}

// snapshot returns a record's field data as a plain map for storage, dropping the
// auth secrets so password hashes and token keys never reach the audit log.
func snapshot(rec *core.Record) map[string]any {
	if rec == nil {
		return nil
	}
	out := map[string]any{}
	for k, v := range rec.FieldsData() {
		if k == "password" || k == "tokenKey" {
			continue
		}
		out[k] = v
	}
	return out
}
