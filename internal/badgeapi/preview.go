package badgeapi

import (
	"context"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
	"github.com/stone-age-io/access-control/internal/authz"
)

// GET /api/badge/preview/{id} — an operator seeing what a cardholder's own badge says.
//
//	operator collection + `enroll`
//
// # The question this answers
//
// "My pass doesn't work." A holder can describe their screen badly, and almost every
// cause is invisible from the operator side without cross-referencing four collections:
// no credential issued at all, a window that has not opened, a suspended cardholder, a
// role whose group grants nothing, a door that grants in person but not remotely, a
// `badge_login` that was never ticked. The badge itself already reduces all of that to
// one sentence and one list — so the cheapest honest answer is to render the holder's
// actual payload.
//
// # Why a projection and not an impersonation token
//
// PocketBase can mint a real session for another record (`NewStaticAuthToken`), and it
// would have let an operator press the holder's buttons. That was rejected: badge actions
// stamp `actor_id` with the CARDHOLDER, because that is who the audit trail is about. An
// operator acting through a borrowed badge session would write rows indistinguishable
// from the holder's own, so "did this visitor open the loading bay, or did someone
// checking on them?" would become unanswerable from the log — and it is the sort of
// question that only gets asked after something has gone wrong.
//
// So this route mints nothing. It is a READ, it actuates nothing, and an operator who
// needs a door opened uses the command routes, where it is audited as an operator action
// under their own `command` capability. The trade is real and worth stating: this cannot
// prove a holder's unlock button works end-to-end. It can only prove what the server
// would decide, which is where essentially every "my badge is broken" actually lives.
//
// # Why it reuses buildMe/buildLive verbatim
//
// A troubleshooting view that differs from the holder's screen is worse than none: it
// sends the operator hunting for a discrepancy that is in the preview rather than in the
// policy graph. Both builders take a cardholder record and a context and nothing else,
// so there is one implementation of "what does this badge say" and no way for the two
// callers to diverge.
//
// # What it deliberately does NOT widen
//
// The response is the badge payload — names only, no portal codes, no relay indices, no
// other people. An operator can already read all of that through the collections; the
// point is that this route adds no new field to the badge shape, so it cannot become the
// place where a hardware detail leaks into the badge contract.

// previewResponse is the holder's own two payloads, plus the operator-only facts that
// explain a badge which renders correctly and is still unusable.
type previewResponse struct {
	// Me and Live are byte-for-byte what GET /api/badge/me and /api/badge/live return to
	// the holder themselves.
	Me   meResponse   `json:"me"`
	Live liveResponse `json:"live"`

	// BadgeLogin is the commonest cause of "I can't get in" that the badge payload
	// cannot show: without it the person never reaches a badge at all, so `me` looks
	// perfectly healthy while they sit at a sign-in form being refused. The AuthRule is
	// what enforces it; this only reports it.
	BadgeLogin bool `json:"badgeLogin"`
	// PasswordSet distinguishes a holder who can sign in with a password from one who
	// depends on an emailed code — which decides whether an install with no SMTP can get
	// them in at all.
	PasswordSet bool `json:"passwordSet"`
	// Status is the cardholder's own status field. `passState` already collapses
	// `suspended` into itself, but an operator wants the cause named rather than inferred.
	Status string `json:"status"`
}

// registerPreviewRoute is called from Register.
func (h *handler) registerPreviewRoute(se *core.ServeEvent) {
	// Operator-only, and NOT bound to `cardholders`: a badge token calling this for
	// another id would be reading a stranger's badge. `enroll` because this is the
	// capability that already covers people and their credentials — the same operator who
	// can issue the pass can see what it looks like.
	se.Router.GET("/api/badge/preview/{id}", h.previewBadge).Bind(authz.RequireOperatorAuth())
}

func (h *handler) previewBadge(e *core.RequestEvent) error {
	if err := authz.RequireCapability(e, authz.CapEnroll); err != nil {
		return err
	}

	cardholder, err := h.app.FindRecordById(BadgeCollection, e.Request.PathValue("id"))
	if err != nil {
		return e.NotFoundError("cardholder not found", err)
	}

	resp, err := h.buildPreview(e.Request.Context(), cardholder)
	if err != nil {
		return e.InternalServerError("failed to load credentials", err)
	}

	// Audited even though it changes nothing. Reading someone's badge means reading their
	// photo, their QR payload, and every door they hold — that is a look at a person, and
	// looks at people are exactly what an access-control system should be able to account
	// for afterwards. `update` because audit_logs has no `read` event_type (1750000009);
	// `action` is what actually names it.
	h.writeBadgeAudit(e, "update", cardholder.Id, map[string]any{
		"action": "view_badge_preview",
		"email":  cardholder.Email(),
	})
	return e.JSON(http.StatusOK, resp)
}

// buildPreview assembles the whole preview for one cardholder. Split from the route so
// the payload can be tested without a request, and so the route reads as what it is:
// a capability check, a lookup, this, and an audit row.
func (h *handler) buildPreview(ctx context.Context, cardholder *core.Record) (previewResponse, error) {
	me, err := h.buildMe(ctx, cardholder)
	if err != nil {
		return previewResponse{}, err
	}
	return previewResponse{
		Me:          me,
		Live:        h.buildLive(ctx, cardholder.Id),
		BadgeLogin:  cardholder.GetBool("badge_login"),
		PasswordSet: cardholder.GetBool("password_set"),
		Status:      cardholder.GetString("status"),
	}, nil
}
