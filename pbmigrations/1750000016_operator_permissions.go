package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/tools/types"
)

// Operator capabilities — replace the single `users.role` rank with an
// orthogonal multi-select `permissions` field.
//
// 1750000009 gave operators a strict ladder (viewer ⊂ operator ⊂ admin). Real
// roles are not a ladder: "enrollment only", "view + commands only", "door ops
// but not hardware" are each a non-linear subset of abilities. This migration
// stores ability as a set of capabilities and rewrites the collection rules to
// gate writes per capability. Read stays a universal floor (any authenticated
// operator reads everything); only writes and commands are gated.
//
//	enroll     write people: cardholders, credentials
//	policy     write access logic: roles, access_groups, schedules, holidays
//	topology   write hardware: locations, controllers, portals, aux_input/output
//	command    issue door commands (gated in internal/commandapi, not a collection)
//	operators  manage operator accounts (users) + read audit_logs + hard-delete
//
// Capability value names are pairwise non-substring on purpose. The rule form
// is `@request.auth.permissions ~ "x"` (JSON LIKE): a multi-select referenced
// through @request.auth is bound as its serialized array, not json_each-expanded,
// so the "any equals" operator (?=) does NOT match — `~` (contains) does, and is
// exact here precisely because no value is a substring of another. (Verified by
// TestPermissionRuleEnforcement, which is the security boundary for this change.)
//
// Superusers still bypass every rule (break-glass). The Vue UI authenticates
// `users` records only, so this is the operator's whole authorization surface.
// Control-plane only: policy.Decide, the KV mirror, and the controller never
// see `permissions`.
func init() {
	migrations.Register(func(app core.App) error {
		// Capability-gated rule expressions. nil = superuser-only; "" = public.
		var (
			anyAuth   = types.Pointer(`@request.auth.id != ""`)
			pPeople   = types.Pointer(`@request.auth.permissions ~ "enroll"`)
			pPolicy   = types.Pointer(`@request.auth.permissions ~ "policy"`)
			pTopology = types.Pointer(`@request.auth.permissions ~ "topology"`)
			pOps      = types.Pointer(`@request.auth.permissions ~ "operators"`)
			usersSelf = types.Pointer(`@request.auth.permissions ~ "operators" || id = @request.auth.id`)
		)

		setRules := func(name string, list, view, create, update, del *string) error {
			c, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				return err
			}
			c.ListRule, c.ViewRule = list, view
			c.CreateRule, c.UpdateRule, c.DeleteRule = create, update, del
			return app.Save(c)
		}

		// --- users: add the permissions field (multi-select). ---
		users, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}
		users.Fields.Add(&core.SelectField{
			Name:      "permissions",
			Values:    []string{"enroll", "policy", "topology", "command", "operators"},
			MaxSelect: 5,
		})
		if err := app.Save(users); err != nil {
			return err
		}

		// Capture each operator's legacy role before dropping the field, so the
		// backfill doesn't trip role's Required validation mid-migration.
		all, err := app.FindAllRecords("users")
		if err != nil {
			return err
		}
		caps := make(map[string][]string, len(all))
		for _, u := range all {
			caps[u.Id] = capsForRole(u.GetString("role"))
		}

		// --- users rules: account mgmt needs `operators`; self may edit self.
		// Drop the role rank: permissions is the single source of truth. ---
		users.ListRule, users.ViewRule = usersSelf, usersSelf
		users.CreateRule, users.UpdateRule, users.DeleteRule = pOps, usersSelf, pOps
		users.ManageRule = pOps
		users.Fields.RemoveByName("role")
		if err := app.Save(users); err != nil {
			return err
		}

		// Backfill permissions now that role is gone (re-fetch for fresh schema).
		fresh, err := app.FindAllRecords("users")
		if err != nil {
			return err
		}
		for _, u := range fresh {
			u.Set("permissions", caps[u.Id])
			if err := app.Save(u); err != nil {
				return err
			}
		}

		// --- people: enroll writes; hard-delete is a trusted-admin action. ---
		for _, name := range []string{"cardholders", "credentials"} {
			if err := setRules(name, anyAuth, anyAuth, pPeople, pPeople, pOps); err != nil {
				return err
			}
		}

		// --- holidays: low-value access logic, fully managed by `policy`. ---
		if err := setRules("holidays", anyAuth, anyAuth, pPolicy, pPolicy, pPolicy); err != nil {
			return err
		}

		// --- access logic: policy writes; hard-delete trusted-admin. ---
		for _, name := range []string{"schedules", "access_groups", "roles"} {
			if err := setRules(name, anyAuth, anyAuth, pPolicy, pPolicy, pOps); err != nil {
				return err
			}
		}

		// --- hardware/topology: topology writes; hard-delete trusted-admin. ---
		for _, name := range []string{"locations", "controllers", "portals", "aux_input", "aux_output"} {
			if err := setRules(name, anyAuth, anyAuth, pTopology, pTopology, pOps); err != nil {
				return err
			}
		}

		// --- audit_logs: sensitive read, gated to the account-mgmt capability. ---
		if c, err := app.FindCollectionByNameOrId("audit_logs"); err == nil {
			c.ListRule, c.ViewRule = pOps, pOps
			if err := app.Save(c); err != nil {
				return err
			}
		}
		// events, point_status: machine-written projections — rules unchanged
		// (anyAuth read, nil writes that accessd's app.Save bypasses).
		return nil
	}, func(app core.App) error {
		// Down: restore the role rank and 1750000009's role-based rules.
		role := types.Pointer(`@request.auth.role = "operator" || @request.auth.role = "admin"`)
		admin := types.Pointer(`@request.auth.role = "admin"`)
		anyAuth := types.Pointer(`@request.auth.id != ""`)
		usersSelf := types.Pointer(`@request.auth.role = "admin" || id = @request.auth.id`)

		setRules := func(name string, list, view, create, update, del *string) error {
			c, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				return nil // collection gone — nothing to revert
			}
			c.ListRule, c.ViewRule = list, view
			c.CreateRule, c.UpdateRule, c.DeleteRule = create, update, del
			return app.Save(c)
		}

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
		if err := app.Save(users); err != nil {
			return err
		}
		all, err := app.FindAllRecords("users")
		if err != nil {
			return err
		}
		for _, u := range all {
			u.Set("role", roleForCaps(u.GetStringSlice("permissions")))
			if err := app.Save(u); err != nil {
				return err
			}
		}
		users.ListRule, users.ViewRule = usersSelf, usersSelf
		users.CreateRule, users.UpdateRule, users.DeleteRule = admin, usersSelf, admin
		users.ManageRule = admin
		users.Fields.RemoveByName("permissions")
		if err := app.Save(users); err != nil {
			return err
		}

		for _, name := range []string{"cardholders", "credentials", "holidays"} {
			if err := setRules(name, anyAuth, anyAuth, role, role, role); err != nil {
				return err
			}
		}
		for _, name := range []string{"locations", "schedules", "controllers", "portals", "access_groups", "roles", "aux_input", "aux_output"} {
			if err := setRules(name, anyAuth, anyAuth, admin, admin, admin); err != nil {
				return err
			}
		}
		if c, err := app.FindCollectionByNameOrId("audit_logs"); err == nil {
			c.ListRule, c.ViewRule = admin, admin
			if err := app.Save(c); err != nil {
				return err
			}
		}
		return nil
	})
}

// capsForRole maps a legacy role rank to the equivalent capability set.
func capsForRole(role string) []string {
	switch role {
	case "admin":
		return []string{"enroll", "policy", "topology", "command", "operators"}
	case "operator":
		return []string{"enroll", "policy", "command"}
	default: // viewer or unknown → read-only floor
		return []string{}
	}
}

// roleForCaps best-effort collapses a capability set back to a rank (down only).
func roleForCaps(caps []string) string {
	has := func(c string) bool {
		for _, x := range caps {
			if x == c {
				return true
			}
		}
		return false
	}
	switch {
	case has("operators") || has("topology"):
		return "admin"
	case has("enroll") || has("policy") || has("command"):
		return "operator"
	default:
		return "viewer"
	}
}
