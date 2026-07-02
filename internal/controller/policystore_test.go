package controller

import (
	"testing"
	"time"

	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/policy"

	// Hermetic timezones on any OS (Windows has no system zoneinfo).
	_ "time/tzdata"
)

// seeded returns a store loaded with the fixture graph via apply (the same path
// the KV watcher drives). Mirrors the PocketBase fixture: location hq
// (America/New_York) → portal lobby-main (door, secure, pulse 5) under
// business-hours (M–F 08:00–17:00); role staff → cardholder alice → CARD-001.
func seeded(t *testing.T) *PolicyStore {
	t.Helper()
	s := NewPolicyStore(nil, logger.NewNopLogger(), nil)
	records := []struct{ key, val string }{
		{"location.hq", `{"code":"hq","timezone":"America/New_York","faiSuppress":true}`},
		{"sched.business-hours", `{"code":"business-hours","windows":[{"days":[1,2,3,4,5],"start":"08:00","end":"17:00"}]}`},
		{"controller.ctrl-hq-1", `{"code":"ctrl-hq-1","name":"HQ Controller 1","location":"hq","model":"kincony-server-mini"}`},
		{"portal.lobby-main", `{"code":"lobby-main","type":"door","location":"hq","posture":"secure","pulseSeconds":5,"controller":"ctrl-hq-1","lockRelay":1,"dpsInput":1,"rexInput":2,"heldOpenSeconds":30}`},
		{"group.lobby-group", `{"code":"lobby-group","portals":["lobby-main"],"schedule":"business-hours"}`},
		{"role.staff", `{"code":"staff","groups":["lobby-group"]}`},
		{"user.alice", `{"id":"alice","status":"active","roles":["staff"]}`},
		{"cred.CARD-001", `{"value":"CARD-001","user":"alice","status":"active"}`},
	}
	for _, r := range records {
		s.apply(r.key, []byte(r.val))
	}
	return s
}

// ny builds the UTC instant for a given America/New_York wall-clock time, so
// the store's timezone resolution is exercised round-trip.
func ny(t *testing.T, y int, mo time.Month, d, h, mi int) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load America/New_York: %v", err)
	}
	return time.Date(y, mo, d, h, mi, 0, 0, loc).UTC()
}

func TestStoreDecideGrant(t *testing.T) {
	s := seeded(t)
	// Mon 2026-01-05 09:00 NY — in business hours.
	d := s.Decide(policy.PostureSecure, "CARD-001", "lobby-main", ny(t, 2026, 1, 5, 9, 0))
	if !d.Allow || d.Reason != policy.ReasonAllowGrant || d.User != "alice" || d.Pulse != 5 {
		t.Errorf("decision = %+v, want allow_grant alice pulse=5", d)
	}
}

// TestStoreDecideTimezone proves the store resolves the location timezone rather
// than evaluating in UTC: 16:30 NY (= 21:30 UTC) is inside the window in NY but
// would be outside if evaluated as 21:30 UTC.
func TestStoreDecideTimezone(t *testing.T) {
	s := seeded(t)
	d := s.Decide(policy.PostureSecure, "CARD-001", "lobby-main", ny(t, 2026, 1, 5, 16, 30))
	if !d.Allow || d.Reason != policy.ReasonAllowGrant {
		t.Errorf("decision = %+v, want allow_grant (16:30 NY is in-hours; UTC would deny)", d)
	}
}

func TestStoreDecideScheduleClosed(t *testing.T) {
	s := seeded(t)
	// Mon 18:00 NY — after hours.
	d := s.Decide(policy.PostureSecure, "CARD-001", "lobby-main", ny(t, 2026, 1, 5, 18, 0))
	if d.Allow || d.Reason != policy.ReasonDenyScheduleClosed {
		t.Errorf("decision = %+v, want deny_schedule_closed", d)
	}
}

func TestStoreDecideLockdown(t *testing.T) {
	s := seeded(t)
	d := s.Decide(policy.PostureLockdown, "CARD-001", "lobby-main", ny(t, 2026, 1, 5, 9, 0))
	if d.Allow || d.Reason != policy.ReasonDenyLockdown {
		t.Errorf("decision = %+v, want deny_lockdown", d)
	}
}

// TestStoreRevocation is the headline flow: a delete of the credential key (the
// revocation path) flips the next decision to deny.
func TestStoreRevocation(t *testing.T) {
	s := seeded(t)
	at := ny(t, 2026, 1, 5, 9, 0)

	if d := s.Decide(policy.PostureSecure, "CARD-001", "lobby-main", at); !d.Allow {
		t.Fatalf("precondition: expected allow before revocation, got %+v", d)
	}
	s.remove("cred.CARD-001")
	if d := s.Decide(policy.PostureSecure, "CARD-001", "lobby-main", at); d.Allow || d.Reason != policy.ReasonDenyUnknownCredential {
		t.Errorf("after revocation: decision = %+v, want deny_unknown_credential", d)
	}
}

// TestStoreSuspendUpdate: re-applying the cardholder as suspended denies.
func TestStoreSuspendUpdate(t *testing.T) {
	s := seeded(t)
	at := ny(t, 2026, 1, 5, 9, 0)

	s.apply("user.alice", []byte(`{"id":"alice","status":"suspended","roles":["staff"]}`))
	if d := s.Decide(policy.PostureSecure, "CARD-001", "lobby-main", at); d.Allow || d.Reason != policy.ReasonDenyRevoked {
		t.Errorf("after suspend: decision = %+v, want deny_revoked", d)
	}
}

// TestStoreCredentialValidity: the store parses RFC 3339 bounds on apply and the
// decision honors them (deny before valid_from / after valid_until, allow within).
func TestStoreCredentialValidity(t *testing.T) {
	s := seeded(t)
	at := ny(t, 2026, 1, 5, 9, 0) // Mon in-hours

	// Not yet valid: activates in February.
	s.apply("cred.CARD-001", []byte(`{"value":"CARD-001","user":"alice","status":"active","validFrom":"2026-02-01T00:00:00Z"}`))
	if d := s.Decide(policy.PostureSecure, "CARD-001", "lobby-main", at); d.Allow || d.Reason != policy.ReasonDenyNotYetValid {
		t.Errorf("not-yet-valid: decision = %+v, want deny_not_yet_valid", d)
	}

	// Expired last year.
	s.apply("cred.CARD-001", []byte(`{"value":"CARD-001","user":"alice","status":"active","validUntil":"2025-12-31T23:59:00Z"}`))
	if d := s.Decide(policy.PostureSecure, "CARD-001", "lobby-main", at); d.Allow || d.Reason != policy.ReasonDenyExpired {
		t.Errorf("expired: decision = %+v, want deny_expired", d)
	}

	// Within an explicit window grants again.
	s.apply("cred.CARD-001", []byte(`{"value":"CARD-001","user":"alice","status":"active","validFrom":"2026-01-01T00:00:00Z","validUntil":"2026-12-31T23:59:00Z"}`))
	if d := s.Decide(policy.PostureSecure, "CARD-001", "lobby-main", at); !d.Allow || d.Reason != policy.ReasonAllowGrant {
		t.Errorf("within validity: decision = %+v, want allow_grant", d)
	}
}

// TestStoreCredentialBadValidityFailsClosed: a present-but-unparseable bound drops
// the credential entirely rather than honoring a half-parsed value.
func TestStoreCredentialBadValidityFailsClosed(t *testing.T) {
	s := seeded(t)
	at := ny(t, 2026, 1, 5, 9, 0)

	// Precondition: the seeded credential grants.
	if d := s.Decide(policy.PostureSecure, "CARD-001", "lobby-main", at); !d.Allow {
		t.Fatalf("precondition: expected allow, got %+v", d)
	}
	// Re-apply with a garbage valid_until: the credential is dropped (fail closed).
	s.apply("cred.CARD-001", []byte(`{"value":"CARD-001","user":"alice","status":"active","validUntil":"not-a-date"}`))
	if d := s.Decide(policy.PostureSecure, "CARD-001", "lobby-main", at); d.Allow || d.Reason != policy.ReasonDenyUnknownCredential {
		t.Errorf("bad validity: decision = %+v, want deny_unknown_credential (dropped)", d)
	}
}

// TestStoreHolidays: a holiday on a calendar the location observes closes the
// grant; removing it re-opens. Exercises the calendar->location join in
// rebuildHolidays and the ObserveHolidays flag round-trip from the wire.
func TestStoreHolidays(t *testing.T) {
	s := seeded(t)
	mon := ny(t, 2026, 1, 5, 9, 0) // Mon in business hours

	// hq observes the "us" holiday calendar (re-apply the seeded location with the link).
	s.apply("location.hq", []byte(`{"code":"hq","timezone":"America/New_York","faiSuppress":true,"holidayCalendars":["us"]}`))
	// Make business-hours observe holidays (the seeded schedule omits the flag).
	s.apply("sched.business-hours", []byte(`{"code":"business-hours","windows":[{"days":[1,2,3,4,5],"start":"08:00","end":"17:00"}],"observeHolidays":true}`))
	if d := s.Decide(policy.PostureSecure, "CARD-001", "lobby-main", mon); !d.Allow {
		t.Fatalf("precondition: expected allow before holiday, got %+v", d)
	}

	// A holiday on that Monday in the "us" calendar: the grant now denies (closed).
	s.apply("holiday.h1", []byte(`{"calendar":"us","date":"2026-01-05","recurring":false}`))
	if d := s.Decide(policy.PostureSecure, "CARD-001", "lobby-main", mon); d.Allow || d.Reason != policy.ReasonDenyScheduleClosed {
		t.Errorf("on holiday: decision = %+v, want deny_schedule_closed", d)
	}

	// A holiday on a calendar hq does NOT observe must not affect it.
	s.apply("holiday.h2", []byte(`{"calendar":"emea","date":"2026-01-05","recurring":false}`))
	s.remove("holiday.h1")
	if d := s.Decide(policy.PostureSecure, "CARD-001", "lobby-main", mon); !d.Allow {
		t.Errorf("after removing the observed holiday: decision = %+v, want allow", d)
	}

	// Re-add h1 to the "us" calendar, then drop hq's calendar link: the holiday
	// still exists but is no longer observed here, so the day re-opens. Proves the
	// join keys off the location's calendar membership, not the holiday alone.
	s.apply("holiday.h1", []byte(`{"calendar":"us","date":"2026-01-05","recurring":false}`))
	if d := s.Decide(policy.PostureSecure, "CARD-001", "lobby-main", mon); d.Allow {
		t.Fatalf("precondition: expected deny with us calendar observed, got %+v", d)
	}
	s.apply("location.hq", []byte(`{"code":"hq","timezone":"America/New_York","faiSuppress":true}`))
	if d := s.Decide(policy.PostureSecure, "CARD-001", "lobby-main", mon); !d.Allow {
		t.Errorf("after unlinking the calendar: decision = %+v, want allow", d)
	}
}

// TestStoreApplyFailSafe pins apply/remove's fail-safe contract: a malformed
// value keeps the previous state (never a crash, never a partial write), an
// unknown key prefix is ignored, and a bad timezone falls back to UTC.
func TestStoreApplyFailSafe(t *testing.T) {
	s := seeded(t)
	at := ny(t, 2026, 1, 5, 9, 0) // Mon in-hours; the seeded graph grants

	// Malformed JSON on every existing key: each apply is a no-op, so the
	// decision still grants afterwards.
	for _, key := range []string{
		"location.hq", "sched.business-hours", "controller.ctrl-hq-1",
		"portal.lobby-main", "group.lobby-group", "role.staff",
		"user.alice", "cred.CARD-001",
	} {
		s.apply(key, []byte(`{not json`))
	}
	if d := s.Decide(policy.PostureSecure, "CARD-001", "lobby-main", at); !d.Allow || d.Reason != policy.ReasonAllowGrant {
		t.Errorf("after malformed re-applies: decision = %+v, want allow_grant (previous values kept)", d)
	}

	// Malformed JSON on a NEW key adds nothing.
	s.apply("cred.CARD-999", []byte(`{not json`))
	if d := s.Decide(policy.PostureSecure, "CARD-999", "lobby-main", at); d.Allow || d.Reason != policy.ReasonDenyUnknownCredential {
		t.Errorf("malformed new credential: decision = %+v, want deny_unknown_credential", d)
	}

	// Unknown prefixes are ignored on both apply and remove.
	s.apply("mystery.thing", []byte(`{"code":"thing"}`))
	s.remove("mystery.thing")
	if d := s.Decide(policy.PostureSecure, "CARD-001", "lobby-main", at); !d.Allow {
		t.Errorf("after unknown-prefix apply/remove: decision = %+v, want allow (unaffected)", d)
	}

	// A bad timezone falls back to UTC rather than dropping the location:
	// 09:00 UTC Monday is 04:00 in New York (out of hours), so a grant at that
	// instant proves the schedule now evaluates in UTC.
	s.apply("location.hq", []byte(`{"code":"hq","timezone":"Nope/Nowhere","faiSuppress":true}`))
	utc9 := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	if d := s.Decide(policy.PostureSecure, "CARD-001", "lobby-main", utc9); !d.Allow || d.Reason != policy.ReasonAllowGrant {
		t.Errorf("bad timezone: decision = %+v, want allow_grant (UTC fallback)", d)
	}
}

func TestStorePortalLookup(t *testing.T) {
	s := seeded(t)
	ap, ok := s.Portal("lobby-main")
	if !ok || ap.Location != "hq" || ap.Type != "door" || ap.Posture != policy.PostureSecure || ap.PulseSeconds != 5 {
		t.Errorf("Portal(lobby-main) = %+v ok=%v, want location=hq type=door posture=secure pulse=5", ap, ok)
	}
	if _, ok := s.Portal("nope"); ok {
		t.Errorf("Portal(nope) ok=true, want false")
	}
}

// TestStoreBindingAndController verifies the controller-side hardware view parses
// alongside the pure graph: the binding carries the relay/input indices and the
// controller record carries the model.
func TestStoreBindingAndController(t *testing.T) {
	s := seeded(t)

	b, ok := s.Binding("lobby-main")
	if !ok || b.Controller != "ctrl-hq-1" || b.LockRelay != 1 || b.DpsInput != 1 || b.RexInput != 2 || b.HeldOpenSeconds != 30 {
		t.Errorf("Binding(lobby-main) = %+v ok=%v, want controller=ctrl-hq-1 relay=1 dps=1 rex=2 held=30", b, ok)
	}

	c, ok := s.Controller("ctrl-hq-1")
	if !ok || c.Location != "hq" || c.Model != "kincony-server-mini" {
		t.Errorf("Controller(ctrl-hq-1) = %+v ok=%v, want location=hq model=kincony-server-mini", c, ok)
	}
}

// TestStorePortalsForController filters by the controller relation and reflects
// reassignment and removal.
func TestStorePortalsForController(t *testing.T) {
	s := seeded(t)

	got := s.PortalsForController("ctrl-hq-1")
	if len(got) != 1 || got[0].Code != "lobby-main" {
		t.Fatalf("PortalsForController(ctrl-hq-1) = %+v, want [lobby-main]", got)
	}
	if n := len(s.PortalsForController("ctrl-other")); n != 0 {
		t.Errorf("PortalsForController(ctrl-other) returned %d portals, want 0", n)
	}

	// Reassign lobby-main to another controller: it leaves ctrl-hq-1's set.
	s.apply("portal.lobby-main", []byte(`{"code":"lobby-main","type":"door","location":"hq","controller":"ctrl-hq-2"}`))
	if n := len(s.PortalsForController("ctrl-hq-1")); n != 0 {
		t.Errorf("after reassignment, PortalsForController(ctrl-hq-1) returned %d, want 0", n)
	}
	if got := s.PortalsForController("ctrl-hq-2"); len(got) != 1 || got[0].Code != "lobby-main" {
		t.Errorf("after reassignment, PortalsForController(ctrl-hq-2) = %+v, want [lobby-main]", got)
	}

	// Removing the portal drops the binding entirely.
	s.remove("portal.lobby-main")
	if n := len(s.PortalsForController("ctrl-hq-2")); n != 0 {
		t.Errorf("after removal, PortalsForController(ctrl-hq-2) returned %d, want 0", n)
	}
}
