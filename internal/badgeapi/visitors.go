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

// This file holds the one OPERATOR route in this package: minting a visitor.
//
// It lives here rather than in internal/commandapi because everything it creates is
// badge-tier (a cardholder, a time-bound credential, a `visitor` badge login) and it
// shares this package's constants. It is gated completely differently from the other
// two routes — operator collection + `enroll` capability, never a badge token.
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
	BadgeUserID  string `json:"badgeUserId"`
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
	// Reused is true when this was a repeat visitor: the existing cardholder and
	// badge login were refreshed and their previous credential revoked, rather than a
	// duplicate person being created. Surfaced so the UI can say so plainly.
	Reused bool `json:"reused"`
}

// findVisitorBadgeByEmail returns the badge login for an address, or nil when there
// is none. Auth collections carry a unique email index, so there is at most one.
func (h *handler) findVisitorBadgeByEmail(address string) (*core.Record, error) {
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

// mintVisitor creates a cardholder, a time-bound credential, and a `visitor` badge
// login in ONE transaction — a half-minted visitor (a cardholder with no credential,
// or a credential with no way to see it) is worse than a clean failure.
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

	// A repeat visitor is the SAME PERSON, and the badge collection has a unique
	// email index — so reuse rather than trying (and failing) to create a second
	// login for the same address. Looked up before the transaction so the operator
	// gets a specific error instead of a constraint violation.
	existing, err := h.findVisitorBadgeByEmail(req.Email)
	if err != nil {
		return e.InternalServerError("failed to check for an existing badge", err)
	}
	if existing != nil && existing.GetString("kind") != KindVisitor {
		return e.BadRequestError(
			"that email already belongs to a non-visitor badge; use enrollment for a cardholder", nil)
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

		// --- cardholder + badge login: reuse for a repeat visitor, else create ---
		var ch, bu *core.Record
		if existing != nil {
			bu = existing
			ch, err = tx.FindRecordById("cardholders", bu.GetString("cardholder"))
			if err != nil {
				return fmt.Errorf("existing badge has no cardholder: %w", err)
			}
			// Refresh the identity and access for THIS visit. The role is replaced,
			// not added to: a visit grants exactly the preset chosen now.
			ch.Set("name", req.Name)
			ch.Set("status", "active")
			ch.Set("roles", []string{role.Id})
			if err := tx.Save(ch); err != nil {
				return fmt.Errorf("update cardholder: %w", err)
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
			ch.Set("email", req.Email)
			ch.Set("status", "active")
			ch.Set("roles", []string{role.Id})
			if err := tx.Save(ch); err != nil {
				return fmt.Errorf("create cardholder: %w", err)
			}

			badgeUsers, err := tx.FindCollectionByNameOrId(BadgeCollection)
			if err != nil {
				return err
			}
			bu = core.NewRecord(badgeUsers)
			bu.SetEmail(req.Email)
			bu.Set("cardholder", ch.Id)
			bu.Set("kind", KindVisitor)
			bu.SetVerified(true) // the OTP round-trip proves control of the address
			// PocketBase requires a non-blank password even though this collection
			// has password auth DISABLED (the field validator is independent of the
			// auth method). It is unusable for sign-in and nobody is ever told it —
			// random rather than a constant, so it cannot become a de-facto shared
			// secret if password auth is ever switched back on.
			throwaway, err := newVisitorCredentialValue()
			if err != nil {
				return err
			}
			bu.SetPassword(throwaway)
			if err := tx.Save(bu); err != nil {
				return fmt.Errorf("create badge login: %w", err)
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
			BadgeUserID:  bu.Id,
			CredentialID: cred.Id,
			Email:        req.Email,
			ValidFrom:    from.Format(time.RFC3339),
			ValidUntil:   until.Format(time.RFC3339),
			BadgeURL:     "/badge",
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
	rec.Set("record_id", resp.BadgeUserID)
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
		h.log.Error("failed to write visitor mint audit row", "badgeUser", resp.BadgeUserID, "error", err)
	}
}
