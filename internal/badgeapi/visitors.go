package badgeapi

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	pbmailer "github.com/pocketbase/pocketbase/tools/mailer"
	"github.com/pocketbase/pocketbase/tools/types"
	"github.com/stone-age-io/access-control/internal/authz"
)

// This file holds the OPERATOR routes in this package: minting a visitor and ending
// their visit.
//
//	POST /api/badge/visitors             mint: cardholder + time-bound credential
//	POST /api/badge/visitors/{id}/revoke end the visit, keep the record of it
//
// They live here rather than in internal/commandapi because everything they touch is
// badge-tier, and they share this package's constants. Both are gated completely
// differently from the holder routes — operator collection + `enroll` capability,
// never a badge token.
//
// # Revoke is the end-of-visit verb, not delete
//
// A visitor is a cardholder with `kind = visitor` and a credential whose window closes.
// Ending the visit means revoking the credential and KEEPING the person: it preserves
// the record that they were here, it lets the mint route recognise and refresh them as
// a returning visitor rather than duplicating them, and it leaves them a badge page
// that honestly says the pass is over instead of a dead sign-in.
//
// Deleting the person is a separate, later, retention decision — and it now works
// properly, because `credentials.user` cascades (1750000036), so removing a visitor
// removes their card with them. Before the collapse this was the tangled part of the
// design: the login was a second record, so "delete the visitor" deleted the login and
// left the credential live and — worse — invisible to internal/badgesweep, which finds
// expired passes by enumerating visitors. A delete hook existed purely to revoke first.
// With one record there are no halves to keep in step, so the hook is gone.
//
// # Why the credential value is generated server-side
//
// It is a key to a building. A client-chosen value could be low-entropy, guessable,
// or deliberately collide with an existing card. It comes from crypto/rand here, and
// the caller never gets a say.
//
// # Why uppercase base32
//
// QR codes have a dense alphanumeric mode covering 0-9, A-Z and a few symbols
// (including '-'). Staying inside it makes a visitor's QR meaningfully smaller and
// easier for a cheap scanner to read than mixed-case base64 would, which falls back
// to byte mode. It is also inside the NATS KV key charset
// (policykv.CredentialValuePattern), which the value must satisfy or the credential
// would never mirror to the edge.

const (
	// visitorCredPrefix makes a minted credential obvious in the credentials list,
	// so an operator can tell a visitor pass from an enrolled card at a glance.
	visitorCredPrefix = "V-"
	// visitorCredBytes is the entropy behind each minted value. 16 bytes = 128 bits,
	// encoded as 26 base32 characters.
	visitorCredBytes = 16
	// maxVisitorWindow caps how long a "visitor" pass may live. Without a cap,
	// "visitor" quietly becomes a way to issue a permanent credential that skips the
	// enrollment path and its review.
	maxVisitorWindow = 30 * 24 * time.Hour
)

// base32Upper is unpadded uppercase base32 — QR alphanumeric-mode friendly and
// inside the KV key charset.
var base32Upper = base32.StdEncoding.WithPadding(base32.NoPadding)

// registerVisitorRoutes is called from Register.
func (h *handler) registerVisitorRoutes(se *core.ServeEvent) {
	se.Router.POST("/api/badge/visitors", h.mintVisitor).Bind(authz.RequireOperatorAuth())
	se.Router.POST("/api/badge/visitors/{id}/revoke", h.revokeVisitor).Bind(authz.RequireOperatorAuth())
}

type mintRequest struct {
	Name       string `json:"name"`
	Email      string `json:"email"`
	Role       string `json:"role"`       // roles record id; must have visitor_preset
	ValidFrom  string `json:"validFrom"`  // RFC3339; empty = now
	ValidUntil string `json:"validUntil"` // RFC3339; required
	Label      string `json:"label"`      // optional note on the credential
}

type mintResponse struct {
	CardholderID string `json:"cardholderId"`
	CredentialID string `json:"credentialId"`
	Email        string `json:"email"`
	ValidFrom    string `json:"validFrom"`
	ValidUntil   string `json:"validUntil"`
	// InviteSent reports whether the invite email went out. Minting SUCCEEDS even
	// when it does not — the pass is valid regardless, and an operator can always
	// hand over the link — so this is informational, not an error.
	InviteSent bool `json:"inviteSent"`
	// BadgeURL is where the visitor signs in. Deliberately carries no token and no
	// email: the visitor types their address and PocketBase emails them an OTP, so
	// nothing sensitive lands in a URL, a browser history, or a referrer header.
	BadgeURL string `json:"badgeUrl"`
	// Reused is true when this was a repeat visitor: the existing cardholder was
	// refreshed and their previous credential revoked, rather than a duplicate person
	// being created. Surfaced so the UI can say so plainly.
	Reused bool `json:"reused"`
}

// findCardholderByEmail returns the cardholder for an address, or nil when there is
// none. cardholders is an auth collection, so email carries a unique index and there
// is at most one.
func (h *handler) findCardholderByEmail(address string) (*core.Record, error) {
	rec, err := h.app.FindAuthRecordByEmail(BadgeCollection, address)
	if err != nil {
		// PocketBase returns an error for "not found"; treat that as absence rather
		// than a failure, since absence is the common case.
		return nil, nil
	}
	return rec, nil
}

// revokeCredentials marks every credential of a cardholder revoked. Used when a
// repeat visitor is issued a new pass: the previous visit's QR must stop working.
func revokeCredentials(tx core.App, cardholderID string) error {
	creds, err := tx.FindRecordsByFilter("credentials", "user = {:user}", "", 0, 0,
		dbx.Params{"user": cardholderID})
	if err != nil {
		return fmt.Errorf("load existing credentials: %w", err)
	}
	for _, c := range creds {
		if c.GetString("status") == "revoked" {
			continue
		}
		c.Set("status", "revoked")
		if err := tx.Save(c); err != nil {
			return fmt.Errorf("revoke previous credential: %w", err)
		}
	}
	return nil
}

// --- POST /api/badge/visitors/{id}/revoke ---

type revokeVisitorResponse struct {
	CardholderID string `json:"cardholderId"`
}

// revokeVisitor ends a visit early: it revokes the credentials and KEEPS the person.
// See the file header for why that is the end-of-visit verb rather than delete.
func (h *handler) revokeVisitor(e *core.RequestEvent) error {
	if err := authz.RequireCapability(e, authz.CapEnroll); err != nil {
		return err
	}

	ch, err := h.app.FindRecordById(BadgeCollection, e.Request.PathValue("id"))
	if err != nil {
		return e.NotFoundError("cardholder not found", err)
	}
	// Deliberately visitor-only. Revoking an enrolled cardholder's credentials from a
	// visitor route would make one button mean "lock this person out of the building",
	// which is the confusion the two kinds exist to keep apart. Staff revocation stays
	// on the credential, where an operator can see exactly what they are turning off.
	if ch.GetString("kind") != KindVisitor {
		return e.BadRequestError(
			"only a visitor pass can be revoked this way; revoke a staff credential on the credential itself", nil)
	}

	if err := revokeCredentials(h.app, ch.Id); err != nil {
		return e.InternalServerError("failed to revoke the visitor pass", err)
	}

	h.writeBadgeAudit(e, "update", ch.Id, map[string]any{
		"action": "revoke_visitor_pass",
		"email":  ch.Email(),
	})
	h.log.Info("visitor pass revoked", "cardholder", ch.Id)
	return e.JSON(http.StatusOK, revokeVisitorResponse{CardholderID: ch.Id})
}

// --- POST /api/badge/visitors ---

// mintVisitor creates the visitor and their time-bound credential in ONE transaction —
// a half-minted visitor (a person with no pass, or a pass nobody can see) is worse than
// a clean failure.
func (h *handler) mintVisitor(e *core.RequestEvent) error {
	if err := authz.RequireCapability(e, authz.CapEnroll); err != nil {
		return err
	}

	var req mintRequest
	if err := e.BindBody(&req); err != nil {
		return e.BadRequestError("invalid request body", err)
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Email = strings.TrimSpace(req.Email)

	if req.Name == "" {
		return e.BadRequestError("name is required", nil)
	}
	if _, err := mail.ParseAddress(req.Email); err != nil {
		return e.BadRequestError("a valid email is required (it is how the visitor receives their sign-in code)", nil)
	}

	// The role must be one an operator explicitly curated for visitors. Without this
	// check the route would be a way to grant ANY role while only holding `enroll`.
	role, err := h.app.FindRecordById("roles", req.Role)
	if err != nil {
		return e.BadRequestError("unknown role", err)
	}
	if !role.GetBool("visitor_preset") {
		return e.BadRequestError("role is not available for visitors", nil)
	}

	now := time.Now().UTC()
	from := now
	if req.ValidFrom != "" {
		parsed, err := time.Parse(time.RFC3339, req.ValidFrom)
		if err != nil {
			return e.BadRequestError("validFrom must be an RFC 3339 timestamp", err)
		}
		from = parsed.UTC()
	}
	if req.ValidUntil == "" {
		return e.BadRequestError("validUntil is required — a visitor pass must expire", nil)
	}
	until, err := time.Parse(time.RFC3339, req.ValidUntil)
	if err != nil {
		return e.BadRequestError("validUntil must be an RFC 3339 timestamp", err)
	}
	until = until.UTC()

	if !until.After(from) {
		return e.BadRequestError("validUntil must be after validFrom", nil)
	}
	if until.Sub(from) > maxVisitorWindow {
		return e.BadRequestError(
			fmt.Sprintf("a visitor pass may not exceed %d days; enroll a cardholder instead",
				int(maxVisitorWindow.Hours()/24)), nil)
	}
	if until.Before(now) {
		return e.BadRequestError("validUntil is already in the past", nil)
	}

	value, err := newVisitorCredentialValue()
	if err != nil {
		return e.InternalServerError("failed to generate a credential", err)
	}

	// A repeat visitor is the SAME PERSON, and cardholders carries a unique email
	// index — so reuse rather than trying (and failing) to create a second record for
	// the same address. Looked up before the transaction so the operator gets a
	// specific error instead of a constraint violation.
	existing, err := h.findCardholderByEmail(req.Email)
	if err != nil {
		return e.InternalServerError("failed to check for an existing cardholder", err)
	}
	// Refusing to convert a staff cardholder into a visitor is the point: `kind` decides
	// whether the QR carries the credential VALUE, so flipping an enrolled person to
	// `visitor` would turn their lanyard badge into a working key.
	if existing != nil && existing.GetString("kind") != KindVisitor {
		return e.BadRequestError(
			"that email already belongs to an enrolled cardholder; issue their access through enrollment", nil)
	}

	var resp mintResponse
	err = h.app.RunInTransaction(func(tx core.App) error {
		credentials, err := tx.FindCollectionByNameOrId("credentials")
		if err != nil {
			return err
		}
		fromDT, err := types.ParseDateTime(from)
		if err != nil {
			return err
		}
		untilDT, err := types.ParseDateTime(until)
		if err != nil {
			return err
		}

		// --- the visitor: reuse for a repeat, else create. ONE record. ---
		//
		// This is where the collapse shows most plainly. It used to create a
		// cardholder and then a `badge_users` login pointing back at it, and the
		// reuse branch had to resolve the relation and could fail on a login whose
		// cardholder had gone. There is one row now, so there is one branch.
		var ch *core.Record
		if existing != nil {
			ch = existing
			// Refresh the identity and access for THIS visit. The role is replaced,
			// not added to: a visit grants exactly the preset chosen now.
			ch.Set("name", req.Name)
			ch.Set("status", "active")
			ch.Set("roles", []string{role.Id})
			ch.Set("badge_login", true)
			if err := tx.Save(ch); err != nil {
				return fmt.Errorf("update visitor: %w", err)
			}
			// The previous visit's QR must stop working — otherwise a returning
			// visitor's old code would still be presentable until it aged out.
			if err := revokeCredentials(tx, ch.Id); err != nil {
				return err
			}
		} else {
			cardholders, err := tx.FindCollectionByNameOrId("cardholders")
			if err != nil {
				return err
			}
			ch = core.NewRecord(cardholders)
			ch.Set("name", req.Name)
			ch.SetEmail(req.Email)
			ch.Set("status", "active")
			ch.Set("roles", []string{role.Id})
			ch.Set("kind", KindVisitor)
			ch.Set("badge_login", true)
			// The OTP round-trip proves control of the address, which is the only
			// identity check a visitor gets. Marking it up front also keeps
			// PocketBase from randomising the password on that first code (see
			// bindOTPPasswordPreservation) — moot here, since a visitor is minted
			// with no password to lose, but it keeps the two paths consistent.
			ch.SetVerified(true)
			// No password is set: bindPasswordFill supplies an unguessable random
			// one and leaves `password_set` false, so a visitor never has to invent
			// or manage a password for a one-day pass. OTP is their route in.
			if err := tx.Save(ch); err != nil {
				return fmt.Errorf("create visitor: %w", err)
			}
		}

		// --- this visit's credential ---
		label := req.Label
		if label == "" {
			label = "Visitor pass"
		}
		cred := core.NewRecord(credentials)
		cred.Set("value", value)
		cred.Set("type", "mobile")
		cred.Set("user", ch.Id)
		cred.Set("status", "active")
		cred.Set("label", label)
		cred.Set("valid_from", fromDT)
		cred.Set("valid_until", untilDT)
		if err := tx.Save(cred); err != nil {
			return fmt.Errorf("create credential: %w", err)
		}

		resp = mintResponse{
			CardholderID: ch.Id,
			CredentialID: cred.Id,
			Email:        req.Email,
			ValidFrom:    from.Format(time.RFC3339),
			ValidUntil:   until.Format(time.RFC3339),
			BadgeURL:     "/login?as=badge",
			Reused:       existing != nil,
		}
		return nil
	})
	if err != nil {
		return e.BadRequestError("failed to mint visitor: "+err.Error(), err)
	}

	// Best-effort invite. The pass is already valid, so a mail failure must not fail
	// the request — it is reported instead, and the operator can hand over the link.
	resp.InviteSent = h.sendVisitorInvite(req.Name, req.Email, until)

	h.auditMint(e, resp, role.GetString("code"))
	h.log.Info("visitor minted",
		"cardholder", resp.CardholderID, "role", role.GetString("code"),
		"validUntil", resp.ValidUntil, "inviteSent", resp.InviteSent)
	return e.JSON(http.StatusCreated, resp)
}

// newVisitorCredentialValue returns a KV-key-safe, QR-alphanumeric-friendly random
// credential value.
func newVisitorCredentialValue() (string, error) {
	buf := make([]byte, visitorCredBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return visitorCredPrefix + base32Upper.EncodeToString(buf), nil
}

// sendVisitorInvite emails the visitor where to sign in. It deliberately contains no
// code and no token: the visitor enters their address at the badge page and
// PocketBase mails a one-time code then. Sending a code here would be worse than
// useless — OTPs expire in minutes, and a visit may be days away.
//
// Returns whether the mail was sent; never returns an error, because minting has
// already committed.
func (h *handler) sendVisitorInvite(name, address string, until time.Time) bool {
	settings := h.app.Settings()
	sender := settings.Meta.SenderAddress
	if sender == "" {
		h.log.Warn("visitor invite not sent: no sender address configured", "to", address)
		return false
	}

	appName := settings.Meta.AppName
	if appName == "" {
		appName = "Access control"
	}
	appURL := strings.TrimRight(settings.Meta.AppURL, "/")

	body := fmt.Sprintf(`Hello %s,

You have been issued a visitor pass for %s.

To view your badge, go to:
  %s/badge

Enter this email address (%s) and you will be sent a one-time sign-in code.

Your pass is valid until %s.
`, name, appName, appURL, address, until.Format("Mon 2 Jan 2006 15:04 MST"))

	if err := h.app.NewMailClient().Send(&pbmailer.Message{
		From:    mail.Address{Address: sender, Name: settings.Meta.SenderName},
		To:      []mail.Address{{Address: address, Name: name}},
		Subject: appName + " — your visitor pass",
		Text:    body,
	}); err != nil {
		// Not an error for the caller: SMTP may simply not be configured, which is a
		// normal state for an install that hands out links at the desk.
		h.log.Warn("visitor invite email failed", "to", address, "error", err)
		return false
	}
	return true
}

// auditMint records the mint. Minting a visitor creates a working credential for a
// person, so it belongs in the operator change log next to credential edits. The
// badge_users and credentials record writes inside the transaction are app.Save
// calls, which do NOT trip the changelog's *Request hooks — hence this explicit row,
// same reasoning as commandapi.writeAudit.
func (h *handler) auditMint(e *core.RequestEvent, resp mintResponse, roleCode string) {
	col, err := h.app.FindCollectionByNameOrId("audit_logs")
	if err != nil {
		h.log.Error("audit sink unavailable", "error", err)
		return
	}
	rec := core.NewRecord(col)
	rec.Set("event_type", "create")
	rec.Set("collection_name", BadgeCollection)
	rec.Set("record_id", resp.CardholderID)
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
	// Deliberately NOT the credential value — an audit row is widely readable and
	// must never become a place to harvest working credentials.
	rec.Set("after", map[string]any{
		"action":     "mint_visitor",
		"cardholder": resp.CardholderID,
		"credential": resp.CredentialID,
		"role":       roleCode,
		"email":      resp.Email,
		"validFrom":  resp.ValidFrom,
		"validUntil": resp.ValidUntil,
	})
	if err := h.app.Save(rec); err != nil {
		h.log.Error("failed to write visitor mint audit row", "cardholder", resp.CardholderID, "error", err)
	}
}
