// Package authz holds the small capability checks shared by accessd's custom
// HTTP routes. Collection CRUD is gated by PocketBase collection rules (see the
// 1750000016 migration); these helpers gate the bespoke routes
// (internal/commandapi, internal/modelsapi) that don't go through a collection.
//
// Operator ability lives in the multi-select `permissions` field of the built-in
// `users` auth collection — an orthogonal set of capabilities, not a rank.
// Superusers (the break-glass account) always pass.
package authz

import (
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

// OperatorCollections are the auth collections whose records may reach accessd's
// operator routes: the operator tier plus the break-glass superuser.
//
// Both names are load-bearing, in opposite directions:
//
//   - apis.RequireAuth() with NO argument admits a record from ANY auth
//     collection. That was harmless while `users` was the only one, but it widens
//     silently the moment a second auth tier exists (badge_users, migration
//     1750000030). /api/simulate is the sharp case — a decision oracle over the
//     entire policy graph.
//   - apis.RequireAuth("users") ALONE is also wrong: PocketBase's requireAuth is a
//     plain collection-name membership test with no superuser exemption, so it
//     would lock out the break-glass account that RequireCapability deliberately
//     admits (and that collection rules bypass entirely).
var OperatorCollections = []string{"users", core.CollectionNameSuperusers}

// RequireOperatorAuth is apis.RequireAuth scoped to OperatorCollections. Prefer it
// over bare apis.RequireAuth() on every operator route, so the operator/badge
// boundary is stated in one place rather than re-derived per route.
func RequireOperatorAuth() *hook.Handler[*core.RequestEvent] {
	return apis.RequireAuth(OperatorCollections...)
}

// Capability constants — the values of users.permissions.
const (
	CapEnroll    = "enroll"    // write people: cardholders, credentials
	CapPolicy    = "policy"    // write access logic: roles, access_groups, schedules, holidays
	CapTopology  = "topology"  // write hardware: locations, controllers, portals, aux_io
	CapCommand   = "command"   // issue door commands
	CapOperators = "operators" // manage operator accounts, read audit log, hard-delete
)

// RequireCapability returns nil when the request's authenticated identity is
// allowed: any superuser, or a user whose `permissions` include cap. Otherwise
// it returns a 403. Routes must already require auth (apis.RequireAuth) so e.Auth
// is set; the nil check is defensive.
func RequireCapability(e *core.RequestEvent, cap string) error {
	if e.Auth == nil {
		return e.ForbiddenError("authentication required", nil)
	}
	if e.Auth.IsSuperuser() {
		return nil
	}
	if HasCapability(e.Auth, cap) {
		return nil
	}
	return e.ForbiddenError("insufficient permissions for this action", nil)
}

// HasCapability reports whether the auth record's `permissions` include cap.
// Pure membership — it does NOT grant superusers anything (callers that want the
// break-glass bypass check IsSuperuser separately).
func HasCapability(auth *core.Record, cap string) bool {
	if auth == nil {
		return false
	}
	for _, c := range auth.GetStringSlice("permissions") {
		if c == cap {
			return true
		}
	}
	return false
}
