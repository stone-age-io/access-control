package badgeapi

import (
	"net/http"
	"net/mail"
	"strings"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	pbmailer "github.com/pocketbase/pocketbase/tools/mailer"
	"github.com/pocketbase/pocketbase/tools/types"
	"github.com/stone-age-io/access-control/internal/authz"
)

// This file covers the NON-VISITOR half of the badge tier: giving an already-enrolled
// cardholder a login, and letting that person manage their own password.
//
//	POST /api/badge/holders    operator + `enroll` — issue (or re-issue) a staff badge login
//	POST /api/badge/password   a badge holder      — set or change their own password
//
// # Why issuing needs a route at all
//
// A visitor is minted whole (cardholder + credential + login) by mintVisitor. A staff
// holder is the opposite: the cardholder and credential already exist, enrolled through
// the ordinary flow, and all that is missing is a way to SEE the badge. Creating that
// login through the plain collection API would work, but it would push three things
// onto every caller that must not be got wrong: the `kind` discriminator, the throwaway
// password PocketBase demands on an auth record, and the `password_set` flag that
// decides whether a later password change must prove the old one. Centralising them
// here keeps the collection rules as the boundary and the semantics in one place.
//
// # Why setting a password needs a route
//
// PocketBase's own record-update path requires `oldPassword` from anyone without
// manage access (apis.hasAuthManageAccess). A holder who signed in by OTP cannot
// satisfy that — the stored password is a random throwaway nobody has ever seen — so
// there would be no way to set a FIRST password. Dropping the proof unconditionally
// would be worse than the problem: a stolen session could then silently change the
// password of a holder who does have one. `password_set` (migration 1750000034)
// distinguishes the two states, and this route demands the old password exactly when
// it is true.

// minBadgePasswordLength matches PocketBase's own password validator. Stated
// explicitly so the caller gets an actionable message instead of a validation error
// from deep inside a record save.
const minBadgePasswordLength = 8

// registerHolderRoutes is called from Register.
func (h *handler) registerHolderRoutes(se *core.ServeEvent) {
	se.Router.POST("/api/badge/holders", h.issueHolder).Bind(authz.RequireOperatorAuth())
	se.Router.POST("/api/badge/password", h.setPassword).Bind(apis.RequireAuth(BadgeCollection))

	// A completed "forgot password" round-trip means the holder now knows their
	// password, so the flag must follow. Without this, a holder who recovered by email
	// would still be treated as having no password and could skip the old-password
	// proof on a later change. Bound to badge_users only; the operator tier has its own
	// lifecycle. Fail-safe: a flag write failure is logged, never propagated — the
	// reset itself has already succeeded and refusing it would strand the holder.
	se.App.OnRecordConfirmPasswordResetRequest(BadgeCollection).BindFunc(func(e *core.RecordConfirmPasswordResetRequestEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		if e.Record == nil || e.Record.GetBool("password_set") {
			return nil
		}
		e.Record.Set("password_set", true)
		if err := e.App.Save(e.Record); err != nil {
			h.log.Error("failed to flag password_set after reset", "badgeUser", e.Record.Id, "error", err)
		}
		return nil
	})
}

// --- POST /api/badge/holders ---

type issueHolderRequest struct {
	// Cardholder is the person this login speaks for (a cardholders record id).
	Cardholder string `json:"cardholder"`
	// Email is where they sign in. Optional: defaults to the cardholder's own email,
	// which is the usual case and saves the operator retyping it.
	Email string `json:"email"`
	// Password is optional. Empty leaves the login OTP/OAuth-only; set, it is what the
	// operator hands over in person — the one path that works with no SMTP at all.
	Password string `json:"password"`
	// SendInvite emails the holder where to sign in. Never carries the password.
	SendInvite bool `json:"sendInvite"`
}

type issueHolderResponse struct {
	BadgeUserID  string `json:"badgeUserId"`
	CardholderID string `json:"cardholderId"`
	Email        string `json:"email"`
	// PasswordSet reports whether this login now has a password its holder knows, so
	// the UI can say "password sign-in enabled" rather than guessing.
	PasswordSet bool `json:"passwordSet"`
	// Created distinguishes a new login from an existing one that was updated.
	Created bool `json:"created"`
	// InviteSent is informational: issuing SUCCEEDS even when the mail fails, because
	// the login is valid regardless and the operator can always say it in person.
	InviteSent bool   `json:"inviteSent"`
	BadgeURL   string `json:"badgeUrl"`
}

// issueHolder gives a cardholder a badge login, or updates the one they already have.
//
// Deliberately upsert rather than create-only, mirroring mintVisitor's repeat-visitor
// reuse: `badge_users` carries a unique index per cardholder, so a second create would
// fail on a constraint, and "reset this person's password" is the same operator intent
// reaching the same record. Re-issuing never touches the credential or the policy
// graph — a badge login only controls who may SEE the badge.
func (h *handler) issueHolder(e *core.RequestEvent) error {
	if err := authz.RequireCapability(e, authz.CapEnroll); err != nil {
		return err
	}

	var req issueHolderRequest
	if err := e.BindBody(&req); err != nil {
		return e.BadRequestError("invalid request body", err)
	}
	req.Cardholder = strings.TrimSpace(req.Cardholder)
	req.Email = strings.TrimSpace(req.Email)

	if req.Cardholder == "" {
		return e.BadRequestError("cardholder is required", nil)
	}
	cardholder, err := h.app.FindRecordById("cardholders", req.Cardholder)
	if err != nil {
		return e.BadRequestError("unknown cardholder", err)
	}

	// Default to the cardholder's own address — the common case, and it keeps the two
	// records naming the same person by the same string.
	if req.Email == "" {
		req.Email = strings.TrimSpace(cardholder.GetString("email"))
	}
	if _, err := mail.ParseAddress(req.Email); err != nil {
		return e.BadRequestError("a valid email is required (it is the sign-in identity for this badge)", nil)
	}

	if req.Password != "" && len(req.Password) < minBadgePasswordLength {
		return e.BadRequestError("password must be at least 8 characters", nil)
	}

	// An address already used by someone else's badge cannot be reused: auth
	// collections carry a unique email index, and two people sharing a sign-in identity
	// would make the audit trail ambiguous. Checked up front so the operator gets a
	// specific message instead of a constraint violation.
	if byEmail, err := h.findBadgeByEmail(req.Email); err != nil {
		return e.InternalServerError("failed to check for an existing badge", err)
	} else if byEmail != nil && byEmail.GetString("cardholder") != cardholder.Id {
		return e.BadRequestError("that email already belongs to another badge login", nil)
	}

	existing, err := h.findBadgeByCardholder(cardholder.Id)
	if err != nil {
		return e.InternalServerError("failed to check for an existing badge", err)
	}
	if existing != nil && existing.GetString("kind") == KindVisitor {
		return e.BadRequestError(
			"that cardholder holds a VISITOR badge; re-issue it from the visitor flow instead", nil)
	}

	bu, err := upsertHolderBadge(h.app, existing, cardholder.Id, req.Email, req.Password)
	if err != nil {
		return e.BadRequestError("failed to issue badge login: "+err.Error(), err)
	}

	resp := issueHolderResponse{
		BadgeUserID:  bu.Id,
		CardholderID: cardholder.Id,
		Email:        req.Email,
		PasswordSet:  bu.GetBool("password_set"),
		Created:      existing == nil,
		BadgeURL:     "/badge",
	}
	if req.SendInvite {
		resp.InviteSent = h.sendHolderInvite(cardholder.GetString("name"), req.Email, resp.PasswordSet)
	}

	h.auditIssueHolder(e, resp)
	h.log.Info("badge login issued",
		"cardholder", cardholder.Id, "created", resp.Created,
		"passwordSet", resp.PasswordSet, "inviteSent", resp.InviteSent)

	status := http.StatusOK
	if resp.Created {
		status = http.StatusCreated
	}
	return e.JSON(status, resp)
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
	badge := e.Auth
	if badge == nil {
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
	rec, err := h.app.FindRecordById(BadgeCollection, badge.Id)
	if err != nil {
		return e.NotFoundError("badge login not found", err)
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
	h.log.Info("badge password set", "badgeUser", rec.Id, "cardholder", rec.GetString("cardholder"))
	return e.JSON(http.StatusOK, setPasswordResponse{OK: true, TokenInvalidated: true})
}

// --- record semantics (kept out of the HTTP handlers so they can be tested) ---

// upsertHolderBadge creates or updates the badge login for a cardholder and saves it.
// `existing` is nil to create. An empty password means OTP/OAuth-only.
//
// The two rules it exists to keep in one place:
//
//   - A NEW login always gets a non-blank password, because PocketBase requires one on
//     every auth record regardless of the enabled methods. When the operator supplied
//     none it is an unguessable random throwaway — random rather than a constant, so it
//     cannot become a de-facto shared secret across an install's badges — and
//     `password_set` stays false to record that nobody knows it.
//   - `password_set` is only ever set true alongside a real password. Getting that
//     backwards would either strand a holder (proof demanded for a password they never
//     had) or weaken the change path (no proof demanded for one they do).
func upsertHolderBadge(app core.App, existing *core.Record, cardholderID, email, password string) (*core.Record, error) {
	rec := existing
	if rec == nil {
		col, err := app.FindCollectionByNameOrId(BadgeCollection)
		if err != nil {
			return nil, err
		}
		rec = core.NewRecord(col)
		rec.Set("cardholder", cardholderID)
		rec.Set("kind", KindHolder)
	}
	rec.SetEmail(email)

	switch {
	case password != "":
		rec.SetPassword(password)
		rec.Set("password_set", true)
	case existing == nil:
		throwaway, err := newVisitorCredentialValue()
		if err != nil {
			return nil, err
		}
		rec.SetPassword(throwaway)
		rec.Set("password_set", false)
	}

	if err := app.Save(rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// authorizePasswordChange returns an empty string when a badge holder may change their
// own password, or the reason to refuse.
//
// The proof is required exactly when there is something to prove. With `password_set`
// false the stored value is a throwaway the holder has never seen, and their session —
// obtained by OTP or OAuth, each a proof of identity in its own right — is the
// authorization. With it true, demanding the current password is what stops a stolen
// session from silently locking the real holder out of their own badge.
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

// --- helpers ---

// findBadgeByCardholder returns the badge login for a cardholder, or nil. The unique
// index on `cardholder` means there is at most one.
func (h *handler) findBadgeByCardholder(cardholderID string) (*core.Record, error) {
	rec, err := h.app.FindFirstRecordByData(BadgeCollection, "cardholder", cardholderID)
	if err != nil {
		return nil, nil // absence is the common case, not a failure
	}
	return rec, nil
}

// findBadgeByEmail returns the badge login for an address, or nil. Auth collections
// carry a unique email index, so there is at most one.
func (h *handler) findBadgeByEmail(address string) (*core.Record, error) {
	rec, err := h.app.FindAuthRecordByEmail(BadgeCollection, address)
	if err != nil {
		return nil, nil
	}
	return rec, nil
}

// sendHolderInvite tells a staff holder where their badge lives.
//
// It never contains the password, even when the operator just set one: mail is stored
// indefinitely, forwarded, and synced to devices, so emailing a door-opening password
// would outlive every other control around it. The operator hands that over in person —
// which is the point of the operator-set path.
//
// Returns whether the mail was sent; never an error, because the login already exists.
func (h *handler) sendHolderInvite(name, address string, hasPassword bool) bool {
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
		"To view it, go to:\n  " + appURL + "/badge\n\n" +
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

// auditIssueHolder records the issue. Giving a person a way to see a badge — and, at a
// remote-unlock door, to open it from a phone — is a control-plane grant, so it belongs
// in the operator change log. The record write above is an app.Save, which does NOT
// trip the changelog's *Request hooks, hence this explicit row (same reasoning as
// commandapi.writeAudit and auditMint).
func (h *handler) auditIssueHolder(e *core.RequestEvent, resp issueHolderResponse) {
	action := "update_badge_login"
	eventType := "update"
	if resp.Created {
		action, eventType = "issue_badge_login", "create"
	}
	h.writeBadgeAudit(e, eventType, resp.BadgeUserID, map[string]any{
		"action":      action,
		"cardholder":  resp.CardholderID,
		"email":       resp.Email,
		"kind":        KindHolder,
		"passwordSet": resp.PasswordSet,
	})
}

// auditPasswordChange records a holder changing their own password. The actor here is
// the badge, not an operator — worth a row precisely because it is a self-service
// change to an authentication credential.
func (h *handler) auditPasswordChange(e *core.RequestEvent, rec *core.Record) {
	h.writeBadgeAudit(e, "update", rec.Id, map[string]any{
		"action":     "badge_password_change",
		"cardholder": rec.GetString("cardholder"),
	})
}

// writeBadgeAudit inserts one audit_logs row against the badge collection. Fail-safe:
// the operation has already committed, so an audit error is logged, never propagated.
// Never records a password, hashed or otherwise — audit rows are widely readable.
func (h *handler) writeBadgeAudit(e *core.RequestEvent, eventType, recordID string, after map[string]any) {
	col, err := h.app.FindCollectionByNameOrId("audit_logs")
	if err != nil {
		h.log.Error("audit sink unavailable", "error", err)
		return
	}
	rec := core.NewRecord(col)
	rec.Set("event_type", eventType)
	rec.Set("collection_name", BadgeCollection)
	rec.Set("record_id", recordID)
	if e.Auth != nil {
		rec.Set("actor_id", e.Auth.Id)
		rec.Set("actor_email", e.Auth.Email())
		rec.Set("actor_collection", e.Auth.Collection().Name)
	}
	rec.Set("request_ip", e.RealIP())
	if e.Request != nil {
		rec.Set("request_method", e.Request.Method)
		rec.Set("request_url", e.Request.URL.Path)
	}
	rec.Set("timestamp", types.NowDateTime())
	rec.Set("after", after)
	if err := h.app.Save(rec); err != nil {
		h.log.Error("failed to write badge audit row", "record", recordID, "error", err)
	}
}
