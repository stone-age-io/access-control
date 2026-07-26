package badgeapi

import (
	"net/http"
	"net/mail"
	"strings"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	pbmailer "github.com/pocketbase/pocketbase/tools/mailer"
	"github.com/stone-age-io/access-control/internal/authz"
)

// This file covers a badge holder's own password, plus the one operator courtesy that
// goes with issuing a login:
//
//	POST /api/badge/password       a badge holder  — set or change their own password
//	POST /api/badge/invite/{id}    operator + `enroll` — email them where their badge is
//
// # Why setting a password needs a route
//
// PocketBase's own record-update path requires `oldPassword` from anyone without
// manage access (apis.hasAuthManageAccess). A holder who signed in by OTP cannot
// satisfy that — the stored password is the random fill from RegisterGuards, which
// nobody has ever seen — so there would be no way to set a FIRST password. Dropping
// the proof unconditionally would be worse than the problem: a stolen session could
// then silently change the password of a holder who does have one. `password_set`
// distinguishes the two states, and this route demands the old password exactly when
// it is true.
//
// # Why there is no /api/badge/holders any more
//
// There used to be a route for "give this cardholder a badge login". It existed to
// centralise three things a caller must not get wrong: the `kind` discriminator, the
// throwaway password PocketBase demands on an auth record, and `password_set`. All
// three moved when the login became the person's own row — `kind` and `badge_login`
// are ordinary fields on a form the operator already uses, and the password and flag
// are filled by bindPasswordFill. Issuing a login is now a checkbox, i.e. a PATCH the
// collection rules already govern, so a bespoke route would only be re-implementing
// them.
//
// The invite email is what remains, because emailing someone is not a field write.

// minBadgePasswordLength matches PocketBase's own password validator. Stated
// explicitly so the caller gets an actionable message instead of a validation error
// from deep inside a record save.
const minBadgePasswordLength = 8

// registerPasswordRoute is called from Register.
func (h *handler) registerPasswordRoute(se *core.ServeEvent) {
	se.Router.POST("/api/badge/password", h.setPassword).Bind(apis.RequireAuth(BadgeCollection))
	se.Router.POST("/api/badge/invite/{id}", h.sendInvite).Bind(authz.RequireOperatorAuth())

	// A completed "forgot password" round-trip means the holder now knows their
	// password, so the flag must follow. Without this, a holder who recovered by email
	// would still be treated as having no password and could skip the old-password
	// proof on a later change. Fail-safe: a flag write failure is logged, never
	// propagated — the reset itself has already succeeded and refusing it would strand
	// the holder.
	se.App.OnRecordConfirmPasswordResetRequest(BadgeCollection).BindFunc(func(e *core.RecordConfirmPasswordResetRequestEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		if e.Record == nil || e.Record.GetBool("password_set") {
			return nil
		}
		e.Record.Set("password_set", true)
		if err := e.App.Save(e.Record); err != nil {
			h.log.Error("failed to flag password_set after reset", "cardholder", e.Record.Id, "error", err)
		}
		return nil
	})
}

// --- POST /api/badge/password ---

type setPasswordRequest struct {
	OldPassword     string `json:"oldPassword"`
	Password        string `json:"password"`
	PasswordConfirm string `json:"passwordConfirm"`
}

type setPasswordResponse struct {
	OK bool `json:"ok"`
	// TokenInvalidated is always true and says so explicitly: PocketBase rotates an
	// auth record's tokenKey whenever the password changes, which kills EVERY session
	// including the caller's. The client must re-authenticate immediately (the badge
	// UI does it silently with the new password) rather than discover it as a 401 on
	// the next request.
	TokenInvalidated bool `json:"tokenInvalidated"`
}

// setPassword lets a signed-in badge holder set or change their own password.
func (h *handler) setPassword(e *core.RequestEvent) error {
	if e.Auth == nil {
		return e.UnauthorizedError("authentication required", nil)
	}

	var req setPasswordRequest
	if err := e.BindBody(&req); err != nil {
		return e.BadRequestError("invalid request body", err)
	}

	if len(req.Password) < minBadgePasswordLength {
		return e.BadRequestError("password must be at least 8 characters", nil)
	}
	if req.Password != req.PasswordConfirm {
		return e.BadRequestError("the two passwords do not match", nil)
	}

	// Re-read rather than trusting the token's copy of the record: `password_set`
	// decides whether the old-password proof is required, and a stale value would
	// decide it wrongly.
	rec, err := h.app.FindRecordById(BadgeCollection, e.Auth.Id)
	if err != nil {
		return e.NotFoundError("cardholder not found", err)
	}

	if msg := authorizePasswordChange(rec, req.OldPassword); msg != "" {
		return e.BadRequestError(msg, nil)
	}

	rec.SetPassword(req.Password)
	rec.Set("password_set", true)
	if err := h.app.Save(rec); err != nil {
		return e.BadRequestError("failed to set password: "+err.Error(), err)
	}

	h.auditPasswordChange(e, rec)
	h.log.Info("badge password set", "cardholder", rec.Id)
	return e.JSON(http.StatusOK, setPasswordResponse{OK: true, TokenInvalidated: true})
}

// authorizePasswordChange returns an empty string when a badge holder may change their
// own password, or the reason to refuse.
//
// The proof is required exactly when there is something to prove. With `password_set`
// false the stored value is the random fill the holder has never seen, and their
// session — obtained by OTP or OAuth, each a proof of identity in its own right — is
// the authorization. With it true, demanding the current password is what stops a
// stolen session from silently locking the real holder out of their own badge.
func authorizePasswordChange(rec *core.Record, oldPassword string) string {
	if !rec.GetBool("password_set") {
		return ""
	}
	if oldPassword == "" {
		return "your current password is required to change it"
	}
	if !rec.ValidatePassword(oldPassword) {
		return "current password is incorrect"
	}
	return ""
}

// --- POST /api/badge/invite/{id} ---

type inviteResponse struct {
	Sent  bool   `json:"sent"`
	Email string `json:"email"`
}

// sendInvite tells a cardholder where their badge lives.
//
// A route rather than a side effect of enabling the login, because the two are
// genuinely separate acts: an install rolling badges out to 500 people enables the
// flag by import and mails later, and an operator handing a password over at a desk
// wants no mail at all. Tying them together would force one of those to be wrong.
func (h *handler) sendInvite(e *core.RequestEvent) error {
	if err := authz.RequireCapability(e, authz.CapEnroll); err != nil {
		return err
	}

	rec, err := h.app.FindRecordById(BadgeCollection, e.Request.PathValue("id"))
	if err != nil {
		return e.NotFoundError("cardholder not found", err)
	}
	if !rec.GetBool("badge_login") {
		return e.BadRequestError("this cardholder has no badge login to invite them to", nil)
	}
	address := strings.TrimSpace(rec.Email())
	if _, err := mail.ParseAddress(address); err != nil {
		return e.BadRequestError("this cardholder has no valid email address", nil)
	}

	sent := h.mailBadgeInvite(rec.GetString("name"), address, rec.GetBool("password_set"))
	h.writeBadgeAudit(e, "update", rec.Id, map[string]any{
		"action": "send_badge_invite",
		"email":  address,
		"sent":   sent,
	})
	return e.JSON(http.StatusOK, inviteResponse{Sent: sent, Email: address})
}

// mailBadgeInvite sends the "your badge is ready" mail.
//
// It never contains the password, even when the operator just set one: mail is stored
// indefinitely, forwarded, and synced to devices, so emailing a door-opening password
// would outlive every other control around it. The operator hands that over in person —
// which is the point of the operator-set path.
//
// Returns whether the mail was sent; never an error, because the login already exists
// and an install with no SMTP is a supported configuration.
func (h *handler) mailBadgeInvite(name, address string, hasPassword bool) bool {
	settings := h.app.Settings()
	sender := settings.Meta.SenderAddress
	if sender == "" {
		h.log.Warn("badge invite not sent: no sender address configured", "to", address)
		return false
	}

	appName := settings.Meta.AppName
	if appName == "" {
		appName = "Access control"
	}
	appURL := strings.TrimRight(settings.Meta.AppURL, "/")

	signIn := "Enter this email address (" + address + ") and you will be sent a one-time sign-in code."
	if hasPassword {
		signIn = "Sign in with this email address (" + address + ") and the password you were given.\n" +
			"You can change it from your badge page once you are signed in."
	}

	body := "Hello " + name + ",\n\n" +
		"Your access badge for " + appName + " is ready.\n\n" +
		"To view it, go to:\n  " + appURL + "/login?as=badge\n\n" +
		signIn + "\n\n" +
		"Your badge shows your photo and access, and can open the doors your organisation has enabled for remote unlock.\n"

	if err := h.app.NewMailClient().Send(&pbmailer.Message{
		From:    mail.Address{Address: sender, Name: settings.Meta.SenderName},
		To:      []mail.Address{{Address: address, Name: name}},
		Subject: appName + " — your access badge",
		Text:    body,
	}); err != nil {
		h.log.Warn("badge invite email failed", "to", address, "error", err)
		return false
	}
	return true
}

// auditPasswordChange records a holder changing their own password. The actor here is
// the badge, not an operator — worth a row precisely because it is a self-service
// change to an authentication credential.
func (h *handler) auditPasswordChange(e *core.RequestEvent, rec *core.Record) {
	h.writeBadgeAudit(e, "update", rec.Id, map[string]any{
		"action": "badge_password_change",
	})
}
