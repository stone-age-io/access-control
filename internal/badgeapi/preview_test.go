package badgeapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
	"github.com/stone-age-io/access-control/internal/policykv"
	"github.com/stone-age-io/access-control/internal/policysnapshot"
)

// The operator's read-only look at a holder's badge (preview.go).
//
// No NATS is needed: h.snapshot serves a cached snapshot when one is fresh, and
// policysnapshot.Build is pure — so a test can hand the handler the exact policy graph it
// should decide over. Seeding the cache rather than faking jetstream.KeyValue also keeps
// these tests pointed at the projection, which is what this package owns.

// withSnapshot primes the handler's snapshot cache so buildMe/buildLive read the given
// graph without touching KV.
func withSnapshot(h *handler, snap *policysnapshot.Snapshot) *handler {
	h.cached, h.cachedAt = snap, time.Now()
	return h
}

// doorGraph grants cardholderID one portal, under an always-open schedule.
func doorGraph(t *testing.T, cardholderID, portalCode string) *policysnapshot.Snapshot {
	t.Helper()
	mk := func(v any) []byte {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return b
	}
	allDay := policykv.Window{Days: []int{1, 2, 3, 4, 5, 6, 7}, Start: "00:00", End: "24:00"}
	return policysnapshot.Build(map[string][]byte{
		policykv.PrefixLocation + "hq": mk(policykv.Location{Code: "hq", Timezone: "UTC"}),
		policykv.PrefixSched + "always": mk(policykv.Schedule{
			Code: "always", Windows: []policykv.Window{allDay},
		}),
		policykv.PrefixPortal + portalCode: mk(policykv.Portal{
			Code: portalCode, Location: "hq", Type: "door", Posture: "secure",
		}),
		policykv.PrefixGroup + "g1": mk(policykv.AccessGroup{
			Code: "g1", Schedule: "always", Portals: []string{portalCode},
		}),
		policykv.PrefixRole + "r1": mk(policykv.Role{Code: "r1", Groups: []string{"g1"}}),
		policykv.PrefixUser + cardholderID: mk(policykv.User{
			ID: cardholderID, Status: "active", Roles: []string{"r1"},
		}),
	})
}

// mkPortal saves a portals record at the fixture's hq location.
func mkPortal(t *testing.T, app core.App, code, name string, set map[string]any) *core.Record {
	t.Helper()
	hq, err := app.FindFirstRecordByData("locations", "code", "hq")
	if err != nil {
		t.Fatalf("fixture location hq: %v", err)
	}
	col, err := app.FindCollectionByNameOrId("portals")
	if err != nil {
		t.Fatalf("portals collection: %v", err)
	}
	r := core.NewRecord(col)
	r.Set("code", code)
	r.Set("name", name)
	r.Set("location", hq.Id)
	r.Set("type", "door")
	for k, v := range set {
		r.Set(k, v)
	}
	if err := app.Save(r); err != nil {
		t.Fatalf("save portal %q: %v", code, err)
	}
	return r
}

// The reason the preview carries three fields beyond the badge payload: a badge can be
// perfectly healthy and its holder still unable to reach it. Without `badgeLogin` an
// operator would look at a valid pass, agree it should work, and have nothing to go on.
func TestPreviewNamesWhatTheBadgePayloadCannotShow(t *testing.T) {
	app := newGuardedApp(t)
	ch := mkCardholder(t, app, "No Login", "nologin@preview.test", nil) // badge_login absent
	mkCredential(t, app, ch.Id, "CARD-NOLOGIN")

	h := withSnapshot(testHandler(app), doorGraph(t, ch.Id, "lobby-preview"))
	got, err := h.buildPreview(context.Background(), ch)
	if err != nil {
		t.Fatalf("buildPreview: %v", err)
	}

	// The badge itself looks fine — which is exactly the trap.
	if got.Me.PassState != PassValid {
		t.Fatalf("passState = %q, want %q; this test needs a healthy pass to be meaningful",
			got.Me.PassState, PassValid)
	}
	if got.BadgeLogin {
		t.Error("badgeLogin = true for a cardholder with no login; the operator is told the wrong thing")
	}
	if got.Status != "active" {
		t.Errorf("status = %q, want the cardholder's own field verbatim", got.Status)
	}
}

// A suspended cardholder is the one cause `passState` does collapse, and the preview must
// still name it: `suspended` outranks every credential, and the operator needs to know it
// is the person and not the card.
func TestPreviewReportsASuspendedHolder(t *testing.T) {
	app := newGuardedApp(t)
	ch := mkCardholder(t, app, "Suspended", "susp@preview.test", map[string]any{
		"badge_login": true, "status": "suspended",
	})
	mkCredential(t, app, ch.Id, "CARD-SUSPENDED")

	h := withSnapshot(testHandler(app), doorGraph(t, ch.Id, "lobby-preview"))
	got, err := h.buildPreview(context.Background(), ch)
	if err != nil {
		t.Fatalf("buildPreview: %v", err)
	}
	if got.Me.PassState != PassSuspended {
		t.Errorf("passState = %q, want %q", got.Me.PassState, PassSuspended)
	}
	if got.Status != "suspended" {
		t.Errorf("status = %q, want suspended", got.Status)
	}
	// The QR is withdrawn for a suspended holder, and the preview must show that rather
	// than a working-looking badge — an operator checking "what does their screen say"
	// must see the same absence they do.
	if got.Me.QR != "" {
		t.Error("a suspended badge previewed with a QR payload; the holder's own screen has none")
	}
}

// The preview must not become the place a hardware field leaks into the badge contract.
// It is an operator route, so the operator could read all of this from the collections —
// the point is that the SHAPE stays the badge's, so nothing here can drift into the
// holder's own payload.
func TestPreviewCarriesNoCodesOrHardware(t *testing.T) {
	app := newGuardedApp(t)
	ch := mkCardholder(t, app, "Shape Check", "shape@preview.test", map[string]any{"badge_login": true})
	mkCredential(t, app, ch.Id, "CARD-SHAPECHECK")
	mkPortal(t, app, "secret-code-portal", "Loading Bay", map[string]any{
		"lock_relay": 7, "allow_remote_unlock": true,
	})

	h := withSnapshot(testHandler(app), doorGraph(t, ch.Id, "secret-code-portal"))
	got, err := h.buildPreview(context.Background(), ch)
	if err != nil {
		t.Fatalf("buildPreview: %v", err)
	}
	if len(got.Me.Portals) != 1 || got.Me.Portals[0].Name != "Loading Bay" {
		t.Fatalf("portals = %+v, want the one named door", got.Me.Portals)
	}

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal preview: %v", err)
	}
	for _, banned := range []string{"secret-code-portal", "lock_relay", "reader_address", "CARD-SHAPECHECK"} {
		if strings.Contains(string(raw), banned) {
			t.Errorf("preview JSON contains %q; the badge shape carries names only:\n%s", banned, raw)
		}
	}
}

// buildMe must depend on WHOSE badge it is and nothing else — that is what makes the
// preview trustworthy as a troubleshooting tool. Two cardholders on one graph must get
// their own answers, and the operator's own identity never enters into it.
func TestBuildMeIsPerCardholder(t *testing.T) {
	app := newGuardedApp(t)
	holder := mkCardholder(t, app, "Granted", "granted@preview.test", map[string]any{"badge_login": true})
	stranger := mkCardholder(t, app, "Ungranted", "ungranted@preview.test", map[string]any{"badge_login": true})
	mkCredential(t, app, holder.Id, "CARD-GRANTED")
	mkCredential(t, app, stranger.Id, "CARD-UNGRANTED")
	mkPortal(t, app, "granted-door", "Granted Door", map[string]any{"allow_remote_unlock": true})

	// The graph grants the door to `holder` only.
	h := withSnapshot(testHandler(app), doorGraph(t, holder.Id, "granted-door"))

	mine, err := h.buildMe(context.Background(), holder)
	if err != nil {
		t.Fatalf("buildMe(holder): %v", err)
	}
	if len(mine.Portals) != 1 {
		t.Errorf("granted holder sees %d doors, want 1", len(mine.Portals))
	}

	theirs, err := h.buildMe(context.Background(), stranger)
	if err != nil {
		t.Fatalf("buildMe(stranger): %v", err)
	}
	if len(theirs.Portals) != 0 {
		t.Errorf("ungranted holder sees %+v, want no doors — the preview would show a stranger's access",
			theirs.Portals)
	}
}

// The fail-soft path: with no policy snapshot, the identity half of a badge is still
// correct and useful, so /me returns it rather than an error. But the three target lists are
// never assigned on that path, and a nil slice marshals to JSON `null` — which the client
// reads as an array (`portals.length`). So a degraded badge would render as a blank screen,
// which is precisely what failing soft exists to avoid.
//
// A handler with no KV and no cached snapshot is exactly that path.
func TestBuildMeAlwaysSendsArraysNotNull(t *testing.T) {
	app := newGuardedApp(t)
	ch := mkCardholder(t, app, "Degraded", "degraded@preview.test", map[string]any{"badge_login": true})
	mkCredential(t, app, ch.Id, "CARD-DEGRADED")

	// No withSnapshot: h.kv is nil, so the snapshot cannot be built.
	h := testHandler(app)
	got, err := h.buildMe(context.Background(), ch)
	if err != nil {
		t.Fatalf("buildMe must fail soft, not error: %v", err)
	}
	// The identity half survived, which is the point of failing soft.
	if got.PassState != PassValid {
		t.Errorf("passState = %q, want %q even with no policy graph", got.PassState, PassValid)
	}

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, field := range []string{`"portals":null`, `"areas":null`, `"outputs":null`} {
		if strings.Contains(string(raw), field) {
			t.Errorf("me JSON contains %s; the client reads these as arrays:\n%s", field, raw)
		}
	}
}

// An expired pass is the commonest support call, so the preview must reproduce the
// holder's own explanation rather than a generic failure.
func TestPreviewShowsAnExpiredWindow(t *testing.T) {
	app := newGuardedApp(t)
	ch := mkCardholder(t, app, "Late Guest", "late@preview.test", map[string]any{
		"kind": KindVisitor, "badge_login": true,
	})
	cred := mkCredential(t, app, ch.Id, "V-EXPIREDPASS")
	until, err := types.ParseDateTime(time.Now().UTC().Add(-2 * time.Hour))
	if err != nil {
		t.Fatalf("ParseDateTime: %v", err)
	}
	cred.Set("valid_until", until)
	if err := app.Save(cred); err != nil {
		t.Fatalf("save credential: %v", err)
	}

	h := withSnapshot(testHandler(app), doorGraph(t, ch.Id, "lobby-preview"))
	got, err := h.buildPreview(context.Background(), ch)
	if err != nil {
		t.Fatalf("buildPreview: %v", err)
	}
	if got.Me.PassState != PassExpired {
		t.Errorf("passState = %q, want %q", got.Me.PassState, PassExpired)
	}
	if got.Me.ValidUntil == "" {
		t.Error("no validUntil on an expired pass; the operator cannot say when it lapsed")
	}
	// A dead visitor pass must not render a QR — neither on the holder's screen nor here.
	if got.Me.QR != "" {
		t.Errorf("expired visitor pass previewed with QR %q; that is a revoked key on screen", got.Me.QR)
	}
}
