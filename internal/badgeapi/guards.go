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

// RegisterGuards binds the badge_users invariants that must hold on the plain
// collection API, whether or not the badge routes were ever registered:
//
//   - the field-level guard on self-update (below),
//   - visitor pass revocation on delete (bindVisitorPassRevocation in visitors.go),
//     because a deleted visitor login must never leave a live credential behind, and
//   - preservation of an operator-set password across a first OTP sign-in
//     (bindOTPPasswordPreservation below).
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
	bindVisitorPassRevocation(app)
	bindOTPPasswordPreservation(app)

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

// bindOTPPasswordPreservation stops a first OTP sign-in from destroying a password the
// holder already knows.
//
// # The behaviour being worked around
//
// PocketBase's auth-with-otp handler, on validating a code, does this when the record
// was not yet `verified` and the code went to the record's own address:
//
//	e.Record.SetVerified(true)
//	if !e.Record.Collection().MFA.Enabled {
//	    e.Record.SetRandomPassword()   // <-- here
//	}
//
// It is a defence against account pre-hijacking: on an OPEN-SIGNUP collection an
// attacker can create a record for someone else's address with a password they know,
// and verifying it later would hand them a live account. Randomising the password at
// the moment of verification kills that.
//
// # Why it is wrong for this tier, and what it broke
//
// `badge_users` has MFA off and (since migration 1750000034) password auth on, so the
// branch fires. The sequence that loses a password:
//
//  1. An operator issues a badge login with an initial password, handed over in
//     person — the one path that works with no SMTP at all. `password_set` is true.
//  2. The holder instead signs in with an emailed one-time code, which is offered on
//     the same page and is the faster option.
//  3. PocketBase randomises their password. `password_set` stays true, because it is
//     ours and PocketBase knows nothing about it.
//  4. POST /api/badge/password now demands a current password that no longer exists
//     and cannot be produced. The holder is locked out of password sign-in with no
//     self-service route back — in exactly the SMTP-less install that needed it.
//
// The pre-hijacking attack it defends against cannot happen here: `badge_users` has an
// explicit `enroll`-gated create rule (migration 1750000030) precisely because an auth
// collection's DEFAULT create rule is open signup. Nobody but an operator can bring a
// record into existence, so there is no attacker-authored record to disarm.
//
// # Why this shape
//
// The randomisation is guarded by `!e.Record.Verified()`, so marking the record
// verified BEFORE PocketBase's handler body runs skips the whole branch. This binds
// pre-e.Next() to get that ordering. Claiming `verified` is not a fiction: an operator
// set this password while looking at the person, which is a stronger identity check
// than clicking a link in an inbox.
//
// Scoped to records that have something to lose (`password_set`), so a login with only
// the random throwaway still gets PocketBase's ordinary verification behaviour.
//
// Fail-safe: a save failure is logged and the sign-in proceeds. Refusing it would
// strand the holder, which is the outcome this exists to prevent.
func bindOTPPasswordPreservation(app core.App) {
	app.OnRecordAuthWithOTPRequest(BadgeCollection).BindFunc(func(e *core.RecordAuthWithOTPRequestEvent) error {
		if !otpWouldEraseAKnownPassword(e.Record) {
			return e.Next()
		}
		e.Record.SetVerified(true)
		if err := e.App.Save(e.Record); err != nil {
			e.App.Logger().Error(
				"could not pre-verify badge login; OTP sign-in may reset its password",
				"badgeUser", e.Record.Id, "error", err)
		}
		return e.Next()
	})
}

// otpWouldEraseAKnownPassword reports whether completing an OTP sign-in for this record
// would randomise a password its holder knows — i.e. whether PocketBase's verification
// branch is about to fire (`!verified`) on a record that has something to lose
// (`password_set`).
func otpWouldEraseAKnownPassword(rec *core.Record) bool {
	return rec != nil && rec.GetBool("password_set") && !rec.Verified()
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
