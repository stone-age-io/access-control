package badgeapi

import (
	"fmt"

	"github.com/pocketbase/pocketbase/core"
	"github.com/stone-age-io/access-control/internal/authz"
)

// protectedBadgeFields are the badge_users fields a badge holder must never write on
// their own record. Each one decides something the holder is the subject of, not the
// author of:
//
//   - cardholder — WHOSE credentials this login resolves. GET /api/badge/me and
//     POST /api/badge/unlock both start from it, so repointing it at another person's
//     cardholder id hands over every door that person can open, attributed to them.
//   - kind — whether the QR carries the credential VALUE (visitor) or an inert
//     identifier (staff). Flipping a lanyard badge to `visitor` mints a permanent,
//     photographable key out of an identifier that opened nothing.
//   - password_set — whether changing the password requires proving the current one.
//     Clearing it lets a stolen session change the password unchallenged and lock the
//     real holder out of their own badge.
var protectedBadgeFields = []string{"cardholder", "kind", "password_set"}

// RegisterGuards binds the field-level guard on badge_users self-update.
//
// # Why a hook and not a collection rule
//
// The update rule is `id = @request.auth.id || <operator enroll>`, which lets a holder
// PATCH their own record — necessary, and by itself not the problem. The problem is
// that a PocketBase collection rule selects which RECORDS may be written and says
// nothing about which FIELDS, so "may edit my own record" silently means "may edit
// every non-system field on it", including the three above. There is no rule syntax
// that expresses "this record, but not these columns"; a hook is the mechanism
// PocketBase provides. This mirrors the privilege-escalation guard internal/changelog
// puts on users.permissions, for exactly the same reason.
//
// Bound to *Request hooks, so it constrains API traffic only — accessd's own
// app.Save writes (internal/badgeapi's issue and password routes, the visitor mint)
// set these fields deliberately and must not be blocked. That is the same
// API-only/programmatic split the changelog relies on.
//
// Registered at startup rather than inside OnServe, so the guard exists even on a boot
// path where the badge ROUTES are never registered: the collection API is served
// regardless of whether NATS came up, and it is the collection API being guarded here.
func RegisterGuards(app core.App) {
	app.OnRecordUpdateRequest(BadgeCollection).BindFunc(func(e *core.RecordRequestEvent) error {
		if isBadgeManager(e.Auth) {
			return e.Next()
		}
		original := e.Record.Original()
		for _, field := range protectedBadgeFields {
			// Compared as formatted strings rather than with != on the `any` values:
			// Record.Get returns the field's native type, and a direct comparison of
			// two interfaces holding a slice (which a relation field yields whenever
			// MaxSelect > 1) panics at runtime instead of reporting inequality.
			if fmt.Sprint(e.Record.Get(field)) != fmt.Sprint(original.Get(field)) {
				return e.ForbiddenError(
					"a badge login may not change its own "+field+"; ask an operator", nil)
			}
		}
		return e.Next()
	})
}

// isBadgeManager reports whether an actor may write the protected fields: a superuser,
// or an operator holding `enroll` (the capability that governs the badge tier).
// Naming the operator collection matters — a badge record has no `permissions` field,
// so the capability test alone would merely be false rather than wrong, but stating it
// keeps the intent legible against a future tier that happens to have one.
func isBadgeManager(auth *core.Record) bool {
	if auth == nil {
		return false
	}
	if auth.IsSuperuser() {
		return true
	}
	return auth.Collection().Name == "users" && authz.HasCapability(auth, authz.CapEnroll)
}
