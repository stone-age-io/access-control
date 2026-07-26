package badgeapi

import (
	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// validationError builds the field-scoped error shape PocketBase's own record
// validators return, so a refusal from the hooks below reaches the operator's form as
// an error ON THAT FIELD rather than an opaque "failed to save record".
func validationError(field, message string) error {
	return validation.Errors{
		field: validation.NewError("validation_"+field, message),
	}
}

// RegisterGuards binds the invariants that keep `cardholders` coherent as an AUTH
// collection. They must hold on the plain collection API — the operator UI, an
// import script, the PocketBase dashboard — so they are bound at startup rather than
// inside OnServe: the collection API is served whether or not NATS came up and the
// badge routes were ever registered.
//
//	OnRecordCreate  every record gets a password (PocketBase demands one) and a
//	                visible email (or operators see blanks in their own list)
//	OnRecordUpdate  a login is never left unusable-by-construction
//	OnRecordAuthWithOTPRequest  a first OTP sign-in must not erase a known password
//
// # What is NOT here any more, and why
//
// This used to also carry a field-level guard (`protectedBadgeFields`) stopping a
// badge holder from rewriting `cardholder`, `kind`, or `password_set` on their own
// record. That guard existed because the login was a separate `badge_users` record
// whose update rule had to permit self-writes. It is gone because the escalation
// surface is gone: `cardholders` update is `enroll`-gated with NO self clause, so a
// badge token cannot PATCH its own record at all. A holder changes their password
// through POST /api/badge/password, which is an app.Save and bypasses collection
// rules by design.
//
// That is the shape of this whole collapse — the guard was not deleted because it
// stopped mattering, it was deleted because the thing it guarded no longer exists.
func RegisterGuards(app core.App) {
	bindPasswordFill(app)
	bindLoginRequiresEmail(app)
	bindOTPPasswordPreservation(app)
}

// bindPasswordFill gives every new cardholder a password and a visible email.
//
// # Password
//
// PocketBase requires a non-blank password on every auth record regardless of which
// auth methods are enabled, and unlike the email field it FORCE-re-enforces
// Required on save (core.Collection.initPasswordField), so there is no way to
// declare it optional. Without this hook, creating a cardholder through the ordinary
// operator form would fail validation on a field that form has no business showing —
// most people in a PACS never sign in.
//
// The filled value is random rather than a constant, so it cannot become a de-facto
// shared secret across an install's records, and `password_set` stays false to record
// that nobody has ever seen it. An account in this state cannot be signed into: the
// AuthRule needs `badge_login`, and even with it there is no password to present and
// no OTP without an address.
//
// Only fills when blank, so an operator issuing a login WITH an initial password —
// the path that works with no SMTP at all — is left alone.
//
// # emailVisibility
//
// PocketBase strips `email` from an auth record's API response unless the requester
// is the record owner, a superuser, or matches the collection's ManageRule
// (core.Record.PublicExport → apis.autoResolveRecordsFlags). An operator browsing
// the cardholder list is none of those unless they hold `enroll`, so without this the
// list would show blank emails to a `policy`- or `topology`-only operator. Defaulting
// it true is honest rather than lax: the read rule already limits `cardholders` to
// operators plus the person themselves, so "visible" here only means visible to
// someone already entitled to the whole record.
func bindPasswordFill(app core.App) {
	app.OnRecordCreate("cardholders").BindFunc(func(e *core.RecordEvent) error {
		if e.Record == nil {
			return e.Next()
		}
		if e.Record.GetString("password") == "" {
			e.Record.SetPassword(security.RandomString(32))
			e.Record.Set("password_set", false)
		}
		e.Record.Set("emailVisibility", true)
		return e.Next()
	})
}

// bindLoginRequiresEmail refuses to enable a badge login on a cardholder with no
// email address.
//
// Not a policy preference — an arithmetic fact about the enabled auth methods. Email
// is the sole `PasswordAuth.IdentityFields` entry, so with no address there is
// nothing to type in the identity box; OTP and password-reset are both emails; and
// OAuth2 matches an existing record by the address the provider returns. A login with
// no email is unusable by every route, so allowing one would only produce a checkbox
// that looks enabled and silently does nothing.
//
// Bound on create AND update, because the address can be cleared later just as easily
// as it can be missing at the start.
func bindLoginRequiresEmail(app core.App) {
	check := func(rec *core.Record) error {
		if rec == nil || !rec.GetBool("badge_login") {
			return nil
		}
		if rec.Email() == "" {
			return validationError(
				"badge_login",
				"a badge login needs an email address — it is the sign-in identity, and how a one-time code is delivered")
		}
		return nil
	}
	app.OnRecordCreate("cardholders").BindFunc(func(e *core.RecordEvent) error {
		if err := check(e.Record); err != nil {
			return err
		}
		return e.Next()
	})
	app.OnRecordUpdate("cardholders").BindFunc(func(e *core.RecordEvent) error {
		if err := check(e.Record); err != nil {
			return err
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
// This collection has MFA off and password auth on, so the branch fires. The sequence
// that loses a password:
//
//  1. An operator enables a badge login with an initial password, handed over in
//     person — the one path that works with no SMTP at all. `password_set` is true.
//  2. The holder instead signs in with an emailed one-time code, which is offered on
//     the same page and is the faster option.
//  3. PocketBase randomises their password. `password_set` stays true, because it is
//     ours and PocketBase knows nothing about it.
//  4. POST /api/badge/password now demands a current password that no longer exists
//     and cannot be produced. The holder is locked out of password sign-in with no
//     self-service route back — in exactly the SMTP-less install that needed it.
//
// The pre-hijacking attack it defends against cannot happen here: `cardholders` has an
// `enroll`-gated create rule, so nobody but an operator can bring a record into
// existence and there is no attacker-authored record to disarm.
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
// the random fill still gets PocketBase's ordinary verification behaviour.
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
				"cardholder", e.Record.Id, "error", err)
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
