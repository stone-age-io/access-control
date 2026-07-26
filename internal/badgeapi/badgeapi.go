// Package badgeapi serves the BADGE TIER: the routes a cardholder or visitor signed
// into the `cardholders` auth collection may call.
//
//	GET  /api/badge/me                 this person's badge — photo, QR, doors
//	POST /api/badge/unlock/{portalId}  remote unlock, authorized by policy.Decide
//	POST /api/badge/password           set or change one's own password
//
// All three are bound to `cardholders` alone. They are NOT operator routes and share
// no authorization with internal/commandapi: a cardholder has no `permissions` field,
// so capability checks are meaningless here.
//
// The package also serves the OPERATOR routes that mint and end a visit
// (visitors.go), gated the opposite way — operator collection plus the `enroll`
// capability — and kept here because everything they touch is badge-tier.
//
// # One collection, two views
//
// `cardholders` is BOTH the person and the login. There is no separate login record:
// a cardholder who may sign in is one with `badge_login` set, and the same row is
// what `credentials.user` points at and what the KV mirror publishes as `user.{id}`.
// Two tiers read the same substrate through different lenses — an operator sees the
// policy graph, a holder sees one badge — and the substrate is shared rather than
// copied.
//
// It started as a second auth collection (`badge_users`) with a 1:1 relation to
// `cardholders`. Everything that made that version complicated was the relation
// itself: a unique index to stop two logins for one person, a cascade delete to stop
// an orphan login authenticating and resolving nobody, a field guard to stop a holder
// repointing the relation at someone else and inheriting their doors, and a delete
// hook to keep a visitor's two halves dying together. Collapsing the two records
// deletes all four, because none of them describe a rule about badges — they were all
// describing the join.
//
// What the collapse costs, stated plainly:
//
//   - Every person is now an auth record, including the majority who never sign in.
//     `badge_login` is the gate (it backs the collection's AuthRule), and a record
//     without it carries a random password nobody has seen. Being an auth record is
//     not being an account.
//   - Auth-flow writes now land on a MIRRORED collection, so they could wake every
//     controller's policy watch. Sign-in itself never writes the record (OTP, MFA and
//     login-alert state live in PocketBase's own `_otps`/`_mfas`/`_authOrigins`);
//     the only write is a first OTP flipping `verified`. internal/mirror skips a KV
//     put whose payload is unchanged, so even that one is absorbed.
//   - `policykv.User` is a hand-built three-field struct — id, status, roles — so no
//     auth field can reach the edge regardless. The wire is unchanged by all of this.
//
// One thing it fixed for free: `cardholders.photo` is a PROTECTED file, and PocketBase
// checks a protected download against the record's own ViewRule. With the login in a
// separate collection, a holder fetching their own photo was refused — the badge
// rendered with no face on it. Being the record is what makes it readable.
//
// # How a remote unlock is authorized
//
// Not by a capability, and not by a list of doors chosen at mint time — by running
// the REAL policy.Decide over a live snapshot of the ACC_POLICY KV, via
// internal/policysnapshot (the same package and the same pure function the access
// simulator and the edge controller use). Consequences, all of them wanted:
//
//   - A remote unlock can never exceed what that person's badge opens in person.
//   - Schedule windows, holidays, credential validity bounds, cardholder status,
//     and the portal's posture gate (lockdown/disabled) all apply identically to
//     the remote path, with no second implementation to drift.
//   - A denial carries the same stable reason code the door would emit.
//
// On top of that floor sits `portals.allow_remote_unlock` (migration 1750000031):
// being on someone's badge is necessary but not sufficient, because "may walk
// through" and "may open from anywhere, with no presence proof" are different
// permissions.
//
// # Why it emits cmd.grant, never a tap
//
// The controller sees an ordinary operator-style grant on the existing
// `cmd.grant` subject — no new subject, no new KV key, no edge change. Publishing a
// synthetic `.tap` instead would make the audit trail assert that a credential was
// presented at a reader when it was not; `Tap.Source` exists precisely to keep a
// physical read distinguishable. The actor is stamped `badge:<cardholderId>` so the
// resulting event attributes the person rather than an operator.
//
// # Denials are audited here
//
// An allowed unlock is audited by the controller's own event (evt.tap → ACC_EVENTS
// → the events projection). A DENIED one never reaches the controller, so it would
// otherwise be invisible — a badge holder could probe doors with no trace. Every
// attempt therefore writes an audit_logs row from this package.
package badgeapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/policysnapshot"
	"github.com/stone-age-io/access-control/internal/subjects"
)

// BadgeCollection is the auth collection these routes serve — which is also the
// collection holding the people the whole PACS is about. See the package doc.
const BadgeCollection = "cardholders"

// Badge kinds (cardholders.kind). The kind decides what the QR encodes; it is NOT
// an access level or an expiry — validity lives on the credential.
const (
	KindHolder  = "holder"  // ongoing cardholder: QR identifies, opens nothing
	KindVisitor = "visitor" // short-lived pass: QR carries the credential value
)

// QR payload kinds reported to the client, so the UI never has to infer whether the
// code it is rendering is a secret.
const (
	QRIdentifier = "identifier" // an opaque cardholder id — safe to display indefinitely
	QRCredential = "credential" // the credential value itself — a key; short-lived only
)

// Pass states. Exactly one describes a badge at any moment, and the client renders
// one branch per state.
//
// This is ONE enum rather than a set of booleans on purpose. It replaced an
// `expired bool` that was computed as "no in-window credential exists", which
// conflated two states a person needs told apart: a cardholder who has never been
// issued a credential saw "your pass is not currently valid", which is true but
// unactionable and reads like a fault. Splitting them into separate booleans would
// have left the client to combine them, and combining them wrongly is precisely the
// bug — so the server decides, once.
const (
	PassValid       = "valid"         // a credential is active and in-window right now
	PassExpired     = "expired"       // a credential exists but its window has passed
	PassNotYetValid = "not_yet_valid" // a credential exists but its window hasn't opened
	PassNone        = "none"          // no usable credential is issued at all
	PassSuspended   = "suspended"     // the CARDHOLDER is suspended; the badge is withdrawn
)

const (
	kvSnapshotTimeout = 5 * time.Second
	// snapshotTTL bounds how stale a cached policy snapshot may be. SnapshotKV
	// drains the whole keyspace, which is fine for an operator pressing "simulate"
	// but not for a button a visitor can tap: without this, every press costs
	// O(credentials + portals + …) of KV traffic. A few seconds keeps revocation
	// effectively immediate while making repeated presses O(1).
	snapshotTTL = 3 * time.Second
)

type handler struct {
	app  core.App
	nc   *nats.Conn
	kv   jetstream.KeyValue
	subj subjects.Subjects
	log  *logger.Logger

	mu       sync.Mutex
	cached   *policysnapshot.Snapshot
	cachedAt time.Time
}

// Register wires the badge routes. apis.RequireAuth names ONLY `cardholders`: an
// operator token must not reach these (an operator is not a person with a badge), and
// a bare RequireAuth() would admit any auth collection.
func Register(se *core.ServeEvent, nc *nats.Conn, kv jetstream.KeyValue, subj subjects.Subjects, log *logger.Logger) {
	h := &handler{
		app:  se.App,
		nc:   nc,
		kv:   kv,
		subj: subj,
		log:  log.With("component", "badgeapi"),
	}
	se.Router.GET("/api/badge/me", h.me).Bind(apis.RequireAuth(BadgeCollection))
	se.Router.POST("/api/badge/unlock/{portalId}", h.unlock).Bind(apis.RequireAuth(BadgeCollection))
	// The OPERATOR routes: mint a visitor, end a visit. Gated by the operator
	// collection + `enroll`, never by a badge token. See visitors.go.
	h.registerVisitorRoutes(se)
	// POST /api/badge/password — a holder setting their own. See password.go.
	h.registerPasswordRoute(se)
}

// --- GET /api/badge/me ---

// meResponse is the badge as the holder's own device renders it. It carries no
// portal codes and no hardware fields: a badge holder has no business knowing a
// portal's relay index, area membership, or KV code, and `policykv.Portal` carries
// all three.
type meResponse struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Kind  string `json:"kind"`
	// PhotoRecord/PhotoFile, not a ready-made URL: cardholders.photo is a PROTECTED
	// file, so a bare URL would 403. The client builds it with the SDK
	// (pb.files.getURL + pb.files.getToken), which is the only supported way to
	// attach a file token. PhotoFile is "" when the cardholder has no photo.
	PhotoRecord string `json:"photoRecord"`
	PhotoFile   string `json:"photoFile"`

	QR       string `json:"qr"`       // payload to encode, "" when there is nothing valid to show
	QRKind   string `json:"qrKind"`   // QRIdentifier | QRCredential
	QRSecret bool   `json:"qrSecret"` // true when QR is a working credential, so the UI can warn

	// The window of the credential PassState describes — which is the live one when
	// valid, the soonest to open when not yet valid, and the last to close when
	// expired. Both "" when there is no credential to describe or its bound is
	// unbounded.
	ValidFrom  string `json:"validFrom"`  // RFC3339, "" = unbounded
	ValidUntil string `json:"validUntil"` // RFC3339, "" = unbounded
	// PassState is the single answer to "is this badge usable right now"; see the
	// Pass* constants. The client switches on it and must not re-derive it.
	PassState string `json:"passState"`

	Portals []badgePortal `json:"portals"`
}

// badgePortal is one door on this badge. `id` is the PocketBase record id (what the
// unlock route takes); `name` is for display. No code, no location code, no wiring.
type badgePortal struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Location     string `json:"location"` // human-readable location NAME, not its code
	RemoteUnlock bool   `json:"remoteUnlock"`
}

func (h *handler) me(e *core.RequestEvent) error {
	cardholder := e.Auth

	kind := cardholder.GetString("kind")
	creds, err := h.credentialsFor(cardholder.Id)
	if err != nil {
		return e.InternalServerError("failed to load credentials", err)
	}
	state, describe := evaluatePass(creds, cardholder.GetString("status"), time.Now().UTC())

	resp := meResponse{
		Name:        cardholder.GetString("name"),
		Email:       cardholder.Email(),
		Kind:        kind,
		PhotoRecord: cardholder.Id,
		PhotoFile:   cardholder.GetString("photo"),
		PassState:   state,
	}
	if describe != nil {
		resp.ValidFrom = dateRFC3339(describe, "valid_from")
		resp.ValidUntil = dateRFC3339(describe, "valid_until")
	}

	credValue := ""
	if state == PassValid && describe != nil {
		credValue = describe.GetString("value")
	}
	resp.QR, resp.QRKind, resp.QRSecret = qrPayload(kind, cardholder.Id, credValue)
	if state == PassSuspended {
		// Withdraw the whole badge face, the inert identifier included. A suspended
		// holder's screen must not look like a working badge — that screen is what
		// someone would hold up to a guard.
		resp.QR, resp.QRSecret = "", false
	}

	// Door list, from the same graph the decision uses.
	snap, err := h.snapshot(e.Request.Context())
	if err != nil {
		// Fail soft: the identity half of the badge is still useful and correct
		// without the door list, and a visitor staring at a blank screen because
		// NATS hiccuped is worse than a badge with no buttons.
		h.log.Error("badge me: policy snapshot unavailable", "cardholder", cardholder.Id, "error", err)
		return e.JSON(http.StatusOK, resp)
	}
	resp.Portals = h.portalsForBadge(snap, cardholder.Id)
	return e.JSON(http.StatusOK, resp)
}

// portalsForBadge resolves the graph's portal codes to display records. A code with
// no PocketBase row is skipped (mid-rename, or KV ahead of the DB) rather than
// shown as a door with no name.
func (h *handler) portalsForBadge(snap *policysnapshot.Snapshot, cardholderID string) []badgePortal {
	codes := snap.PortalsFor(cardholderID)
	sort.Strings(codes) // PortalsFor iterates a map; give the UI a stable order
	out := make([]badgePortal, 0, len(codes))
	for _, code := range codes {
		rec, err := h.app.FindFirstRecordByData("portals", "code", code)
		if err != nil {
			continue
		}
		p := badgePortal{
			ID:           rec.Id,
			Name:         rec.GetString("name"),
			RemoteUnlock: rec.GetBool("allow_remote_unlock"),
		}
		if p.Name == "" {
			p.Name = code // never render a nameless door
		}
		if loc, err := h.app.FindRecordById("locations", rec.GetString("location")); err == nil {
			p.Location = loc.GetString("name")
			if p.Location == "" {
				p.Location = loc.GetString("code")
			}
		}
		out = append(out, p)
	}
	return out
}

// qrPayload decides what a badge's QR code encodes. This is the security-relevant
// fork in this package, so it is a pure function rather than inline branching.
//
//   - A VISITOR pass carries the credential value itself, so it works at a scanner.
//     That makes the QR a key, hence secret=true — but a visitor credential lives for
//     hours, which bounds the exposure to roughly the length of the visit.
//   - Every other badge carries the cardholder ID: an identifier that opens nothing.
//     A staff badge hangs on a lanyard for years and gets photographed incidentally;
//     encoding a working credential there would mint a permanent, photographable key
//     to the building.
//
// An expired/absent credential yields NO payload, deliberately. Rendering a dead
// code invites someone to keep presenting it at a reader and reporting a fault, and
// it would leak a (revoked, but real) credential value for nothing.
func qrPayload(kind, cardholderID, activeCredValue string) (payload, qrKind string, secret bool) {
	if kind == KindVisitor {
		if activeCredValue == "" {
			return "", QRCredential, false
		}
		return activeCredValue, QRCredential, true
	}
	return cardholderID, QRIdentifier, false
}

// --- POST /api/badge/unlock/{portalId} ---

type unlockResponse struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason"` // stable policy reason code
}

func (h *handler) unlock(e *core.RequestEvent) error {
	cardholder := e.Auth

	portal, err := h.app.FindRecordById("portals", e.Request.PathValue("portalId"))
	if err != nil {
		return e.NotFoundError("portal not found", err)
	}
	portalCode := portal.GetString("code")

	// The per-door opt-in, checked before anything else so a door that is not
	// remotely openable never even consults the policy graph — and so probing one
	// tells the caller nothing about their access.
	if !portal.GetBool("allow_remote_unlock") {
		h.audit(e, cardholder.Id, portalCode, false, "remote_unlock_not_allowed")
		return e.ForbiddenError("this door cannot be unlocked remotely", nil)
	}

	snap, err := h.snapshot(e.Request.Context())
	if err != nil {
		// Fail CLOSED: with no policy we cannot authorize anything.
		h.log.Error("badge unlock: policy snapshot unavailable", "cardholder", cardholder.Id, "error", err)
		return e.InternalServerError("policy unavailable", err)
	}

	creds, err := h.credentialsFor(cardholder.Id)
	if err != nil {
		return e.InternalServerError("failed to load credentials", err)
	}
	if len(creds) == 0 {
		h.audit(e, cardholder.Id, portalCode, false, "no_credential")
		return e.ForbiddenError("no credential is issued to this badge", nil)
	}

	// A cardholder may hold several credentials (a card and a mobile, say). Any one
	// of them granting is a grant — exactly as at a reader, where they would simply
	// present the one that works. Report the last denial when none do.
	now := time.Now().UTC()
	reason := "denied"
	for _, cred := range creds {
		res := snap.Simulate(cred.GetString("value"), portalCode, now, "")
		if res.Allow {
			if err := h.publishGrant(snap, portal, portalCode, cardholder.Id); err != nil {
				h.log.Error("badge unlock: publish failed", "portal", portalCode, "error", err)
				return e.InternalServerError("failed to publish command", err)
			}
			h.audit(e, cardholder.Id, portalCode, true, res.Reason)
			h.log.Info("badge remote unlock granted",
				"portal", portalCode, "cardholder", cardholder.Id, "reason", res.Reason)
			return e.JSON(http.StatusAccepted, unlockResponse{OK: true, Reason: res.Reason})
		}
		reason = res.Reason
	}

	h.audit(e, cardholder.Id, portalCode, false, reason)
	h.log.Info("badge remote unlock denied",
		"portal", portalCode, "cardholder", cardholder.Id, "reason", reason)
	return e.JSON(http.StatusForbidden, unlockResponse{OK: false, Reason: reason})
}

// publishGrant emits the same cmd.grant an operator's grant button emits. The
// location/type come from the policy snapshot when known (it is the graph the
// decision just used) and fall back to PocketBase otherwise.
func (h *handler) publishGrant(snap *policysnapshot.Snapshot, portal *core.Record, portalCode, cardholderID string) error {
	locCode, ok := snap.PortalLocation(portalCode)
	if !ok || locCode == "" {
		loc, err := h.app.FindRecordById("locations", portal.GetString("location"))
		if err != nil {
			return errors.New("portal location unresolved: " + portalCode)
		}
		locCode = loc.GetString("code")
	}
	ptype, ok := snap.PortalType(portalCode)
	if !ok || ptype == "" {
		ptype = portal.GetString("type")
	}

	payload, err := json.Marshal(map[string]any{
		"seconds": 0, // fall back to the portal's configured pulse
		"actor":   "badge:" + cardholderID,
		"reason":  "remote_unlock",
	})
	if err != nil {
		return err
	}
	return h.nc.Publish(h.subj.Grant(locCode, ptype, portalCode), payload)
}

// --- helpers ---

// snapshot returns a recent policy snapshot, rebuilding it at most once per
// snapshotTTL. See the constant's comment for why the cache exists.
func (h *handler) snapshot(reqCtx context.Context) (*policysnapshot.Snapshot, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cached != nil && time.Since(h.cachedAt) < snapshotTTL {
		return h.cached, nil
	}
	// Deliberately NOT derived from reqCtx: a client that disconnects mid-drain
	// would otherwise poison the shared refresh for everyone else.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(reqCtx), kvSnapshotTimeout)
	defer cancel()

	entries, err := policysnapshot.SnapshotKV(ctx, h.kv)
	if err != nil {
		return nil, err
	}
	h.cached, h.cachedAt = policysnapshot.Build(entries), time.Now()
	return h.cached, nil
}

// credentialsFor returns the cardholder's non-revoked credentials. Revoked ones are
// filtered here as well as by Decide — cheaper, and it keeps a revoked value from
// being handed to the QR payload.
func (h *handler) credentialsFor(cardholderID string) ([]*core.Record, error) {
	recs, err := h.app.FindRecordsByFilter(
		"credentials",
		"user = {:user}",
		"created", // stable order, so a multi-credential holder gets deterministic results
		0, 0,
		dbx.Params{"user": cardholderID},
	)
	if err != nil {
		return nil, err
	}
	out := make([]*core.Record, 0, len(recs))
	for _, r := range recs {
		if status := r.GetString("status"); status == "revoked" || status == "suspended" {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// evaluatePass reduces a cardholder's credentials to the ONE state their badge is in,
// plus the credential whose window describes that state (nil when there is none to
// describe). Pure, so the four-way branch that produces a person's only explanation of
// why their badge does not work is table-tested rather than inferred from booleans.
//
// `creds` is expected to be the output of credentialsFor — revoked and suspended
// credentials already removed — so "none" covers both "never issued" and "all
// revoked". Both come out as the same sentence to the holder, and the difference is an
// operator's business, not theirs.
//
// A suspended CARDHOLDER outranks everything: policy.Decide already denies them at a
// reader, so a badge that showed a live pass would be contradicting the door.
func evaluatePass(creds []*core.Record, cardholderStatus string, now time.Time) (state string, describe *core.Record) {
	if cardholderStatus == "suspended" {
		return PassSuspended, nil
	}
	if active := activeCredential(creds, now); active != nil {
		return PassValid, active
	}
	if len(creds) == 0 {
		return PassNone, nil
	}

	// Nothing is live, so every credential is either still to come or already gone.
	// Describe a pass that is COMING in preference to one that has gone: "valid from
	// Monday 09:00" is something the holder can act on, where "expired" only explains.
	var soonest, latest *core.Record
	for _, c := range creds {
		if from := c.GetDateTime("valid_from"); !from.IsZero() && now.Before(from.Time()) {
			if soonest == nil || from.Time().Before(soonest.GetDateTime("valid_from").Time()) {
				soonest = c
			}
			continue
		}
		if until := c.GetDateTime("valid_until"); !until.IsZero() && now.After(until.Time()) {
			if latest == nil || until.Time().After(latest.GetDateTime("valid_until").Time()) {
				latest = c
			}
		}
	}
	switch {
	case soonest != nil:
		return PassNotYetValid, soonest
	case latest != nil:
		return PassExpired, latest
	default:
		// Unreachable: activeCredential treats an unparseable or absent bound as
		// unbounded, so a credential it rejected has a real bound on one side or the
		// other. Fall back to the state that promises nothing.
		return PassNone, nil
	}
}

// activeCredential picks the credential whose validity window contains now,
// preferring one that is currently in-window. Returns nil when none is.
func activeCredential(creds []*core.Record, now time.Time) *core.Record {
	for _, c := range creds {
		from := c.GetDateTime("valid_from")
		until := c.GetDateTime("valid_until")
		if !from.IsZero() && now.Before(from.Time()) {
			continue
		}
		if !until.IsZero() && now.After(until.Time()) {
			continue
		}
		return c
	}
	return nil
}

func dateRFC3339(r *core.Record, field string) string {
	dt := r.GetDateTime(field)
	if dt.IsZero() {
		return ""
	}
	return dt.Time().UTC().Format(time.RFC3339)
}

// audit records every badge unlock attempt, allowed or not.
//
// The allowed path is also audited downstream by the controller's event, but the
// DENIED path never reaches the controller — without this a badge holder could
// probe every door in the building and leave no trace. Fail-safe: an audit failure
// is logged, never propagated, since the unlock decision has already been made.
func (h *handler) audit(e *core.RequestEvent, cardholderID, portalCode string, allowed bool, reason string) {
	col, err := h.app.FindCollectionByNameOrId("audit_logs")
	if err != nil {
		h.log.Error("audit sink unavailable", "error", err)
		return
	}
	rec := core.NewRecord(col)
	rec.Set("event_type", "update") // an action on a portal, not a record mutation
	rec.Set("collection_name", "portals")
	rec.Set("record_id", portalCode)
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
	rec.Set("after", map[string]any{
		"action":     "badge_remote_unlock",
		"cardholder": cardholderID,
		"portal":     portalCode,
		"allowed":    allowed,
		"reason":     reason,
	})
	if err := h.app.Save(rec); err != nil {
		h.log.Error("failed to write badge audit row", "portal", portalCode, "error", err)
	}
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
