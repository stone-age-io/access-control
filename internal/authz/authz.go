// Package authz holds the small role checks shared by accessd's custom HTTP
// routes. Collection CRUD is gated by PocketBase collection rules (see the
// 1750000009 migration); these helpers gate the bespoke routes
// (internal/commandapi, internal/modelsapi) that don't go through a collection.
//
// The operator role lives in the `role` field of the built-in `users` auth
// collection: admin / operator / viewer. Superusers (the break-glass account)
// always pass.
package authz

import "github.com/pocketbase/pocketbase/core"

// Role constants — the values of users.role.
const (
	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleViewer   = "viewer"
)

// RequireRole returns nil when the request's authenticated identity is allowed:
// any superuser, or a user whose `role` is one of allowed. Otherwise it returns
// a 403. Routes must already require auth (apis.RequireAuth) so e.Auth is set;
// the nil check is defensive.
func RequireRole(e *core.RequestEvent, allowed ...string) error {
	if e.Auth == nil {
		return e.ForbiddenError("authentication required", nil)
	}
	if e.Auth.IsSuperuser() {
		return nil
	}
	role := e.Auth.GetString("role")
	for _, a := range allowed {
		if role == a {
			return nil
		}
	}
	return e.ForbiddenError("insufficient role for this action", nil)
}
