// Package authz holds the small capability checks shared by accessd's custom
// HTTP routes. Collection CRUD is gated by PocketBase collection rules (see the
// 1750000016 migration); these helpers gate the bespoke routes
// (internal/commandapi, internal/modelsapi) that don't go through a collection.
//
// Operator ability lives in the multi-select `permissions` field of the built-in
// `users` auth collection — an orthogonal set of capabilities, not a rank.
// Superusers (the break-glass account) always pass.
package authz

import "github.com/pocketbase/pocketbase/core"

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
