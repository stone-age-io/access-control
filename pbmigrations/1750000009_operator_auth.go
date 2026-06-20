package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/tools/types"
)

// Operator auth tier + role-based collection rules + the control-plane audit log.
//
// Until now every operator logged into the management UI as a PocketBase
// _superusers admin and every collection had nil access rules (superuser-only).
// That is all-or-nothing and unattributable. This migration moves day-to-day
// management onto the built-in `users` auth collection (shipped by PocketBase's
// bundled init migration) gated by a new `role` field, and replaces the
// nil-everywhere rules with a least-privilege matrix:
//
//	admin     full control + physical topology + operator-account management
//	operator  daily ops: people, credentials, holidays + door commands; reads the rest
//	viewer    read-only
//
// The role field is distinct from the `roles` collection (which bundles
// access_groups for cardholders). Superusers still exist as the break-glass
// account (PocketBase requires >=1; the /_/ dashboard is superuser-only) and
// bypass every rule below — so the very first admin operator is seeded by a
// superuser (dashboard) or the guarded dev fixture in 1750000010.
//
// Enforcement note: these rules govern the REST API only. accessd's own
// programmatic app.Save() writes (mirror, health heartbeats, audit/status
// projections) bypass collection rules, so the internal data flow is untouched.
func init() {
	migrations.Register(func(app core.App) error {
		// Rule expressions. nil pointer = superusers only; an empty-string pointer
		// = public. @request.auth.role resolves for `users` records; superusers
		// bypass rules regardless of what these say.
		var (
			anyAuth    = types.Pointer(`@request.auth.id != ""`)
			writeOps   = types.Pointer(`@request.auth.role = "operator" || @request.auth.role = "admin"`)
			writeAdmin = types.Pointer(`@request.auth.role = "admin"`)
			adminOnly  = types.Pointer(`@request.auth.role = "admin"`)
			usersSelf  = types.Pointer(`@request.auth.role = "admin" || id = @request.auth.id`)
		)

		// setRules applies the five CRUD rules to a collection (manage left as-is).
		setRules := func(name string, list, view, create, update, del *string) error {
			c, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				return err
			}
			c.ListRule = list
			c.ViewRule = view
			c.CreateRule = create
			c.UpdateRule = update
			c.DeleteRule = del
			return app.Save(c)
		}

		// --- users: add the role field and lock the collection down. ---
		users, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}
		users.Fields.Add(&core.SelectField{
			Name:      "role",
			Values:    []string{"admin", "operator", "viewer"},
			MaxSelect: 1,
			Required:  true,
		})
		// Default ships open-signup (createRule == ""). Admins manage operator
		// accounts; an operator may view/update only its own record (a guard hook
		// in internal/changelog blocks self role-escalation). manageRule (set
		// another record's password) is admin-only.
		users.ListRule = usersSelf
		users.ViewRule = usersSelf
		users.CreateRule = adminOnly
		users.UpdateRule = usersSelf
		users.DeleteRule = adminOnly
		users.ManageRule = adminOnly
		if err := app.Save(users); err != nil {
			return err
		}

		// --- daily-ops collections: operators + admins write; all operators read. ---
		for _, name := range []string{"cardholders", "credentials", "holidays"} {
			if err := setRules(name, anyAuth, anyAuth, writeOps, writeOps, writeOps); err != nil {
				return err
			}
		}

		// --- topology/config collections: admins write; all operators read. ---
		for _, name := range []string{
			"locations", "schedules", "controllers", "portals",
			"access_groups", "roles", "aux_input", "aux_output",
		} {
			if err := setRules(name, anyAuth, anyAuth, writeAdmin, writeAdmin, writeAdmin); err != nil {
				return err
			}
		}

		// --- machine-written projections: all operators read; nobody writes via
		// the API (accessd's app.Save bypasses these). ---
		for _, name := range []string{"events", "point_status"} {
			if err := setRules(name, anyAuth, anyAuth, nil, nil, nil); err != nil {
				return err
			}
		}

		// --- audit_logs: the control-plane change log written by internal/changelog.
		// Admin-readable, hook-written (create/update/delete superuser-only =
		// tamper-resistant; the package writes via app.Save which bypasses rules). ---
		auditLogs := core.NewBaseCollection("audit_logs")
		auditLogs.Fields.Add(&core.SelectField{
			Name:      "event_type",
			Values:    []string{"create", "update", "delete", "auth"},
			MaxSelect: 1,
			Required:  true,
		})
		auditLogs.Fields.Add(&core.TextField{Name: "collection_name"})
		auditLogs.Fields.Add(&core.TextField{Name: "record_id"})
		auditLogs.Fields.Add(&core.TextField{Name: "actor_email"})
		auditLogs.Fields.Add(&core.TextField{Name: "actor_id"})
		auditLogs.Fields.Add(&core.TextField{Name: "actor_collection"})
		auditLogs.Fields.Add(&core.TextField{Name: "request_ip"})
		auditLogs.Fields.Add(&core.TextField{Name: "request_method"})
		auditLogs.Fields.Add(&core.TextField{Name: "request_url"})
		auditLogs.Fields.Add(&core.DateField{Name: "timestamp"})
		auditLogs.Fields.Add(&core.JSONField{Name: "before", MaxSize: 1 << 20})
		auditLogs.Fields.Add(&core.JSONField{Name: "after", MaxSize: 1 << 20})
		addTimestamps(auditLogs)
		auditLogs.AddIndex("idx_audit_logs_collection", false, "collection_name", "")
		auditLogs.AddIndex("idx_audit_logs_record", false, "record_id", "")
		auditLogs.AddIndex("idx_audit_logs_actor", false, "actor_id", "")
		auditLogs.AddIndex("idx_audit_logs_timestamp", false, "timestamp", "")
		auditLogs.ListRule = adminOnly
		auditLogs.ViewRule = adminOnly
		auditLogs.CreateRule = nil
		auditLogs.UpdateRule = nil
		auditLogs.DeleteRule = nil
		return app.Save(auditLogs)
	}, func(app core.App) error {
		// Down: delete audit_logs, drop the role field, and revert every collection
		// to nil (superuser-only) rules; restore the users defaults.
		if c, err := app.FindCollectionByNameOrId("audit_logs"); err == nil {
			if err := app.Delete(c); err != nil {
				return err
			}
		}

		clear := func(name string) error {
			c, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				return nil // already gone
			}
			c.ListRule, c.ViewRule, c.CreateRule, c.UpdateRule, c.DeleteRule = nil, nil, nil, nil, nil
			return app.Save(c)
		}
		for _, name := range []string{
			"cardholders", "credentials", "holidays",
			"locations", "schedules", "controllers", "portals",
			"access_groups", "roles", "aux_input", "aux_output",
			"events", "point_status",
		} {
			if err := clear(name); err != nil {
				return err
			}
		}

		if users, err := app.FindCollectionByNameOrId("users"); err == nil {
			users.Fields.RemoveByName("role")
			self := types.Pointer(`id = @request.auth.id`)
			users.ListRule = self
			users.ViewRule = self
			users.CreateRule = types.Pointer("")
			users.UpdateRule = self
			users.DeleteRule = self
			users.ManageRule = nil
			if err := app.Save(users); err != nil {
				return err
			}
		}
		return nil
	})
}
