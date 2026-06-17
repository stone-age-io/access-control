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
// the KV watcher drives). Mirrors the PocketBase fixture: site hq
// (America/New_York) → lobby-main (secure, pulse 5) under business-hours
// (M–F 08:00–17:00); role staff → cardholder alice → credential CARD-001.
func seeded(t *testing.T) *PolicyStore {
	t.Helper()
	s := NewPolicyStore(nil, logger.NewNopLogger(), nil)
	records := []struct{ key, val string }{
		{"site.hq", `{"code":"hq","timezone":"America/New_York","faiSuppress":true}`},
		{"sched.business-hours", `{"code":"business-hours","windows":[{"days":[1,2,3,4,5],"start":"08:00","end":"17:00"}]}`},
		{"point.lobby-main", `{"code":"lobby-main","site":"hq","posture":"secure","pulseSeconds":5}`},
		{"group.lobby-group", `{"code":"lobby-group","points":["lobby-main"],"schedule":"business-hours"}`},
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

// TestStoreDecideTimezone proves the store resolves the site timezone rather
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

func TestStorePointLookup(t *testing.T) {
	s := seeded(t)
	ap, ok := s.Point("lobby-main")
	if !ok || ap.Site != "hq" || ap.Posture != policy.PostureSecure || ap.PulseSeconds != 5 {
		t.Errorf("Point(lobby-main) = %+v ok=%v, want site=hq posture=secure pulse=5", ap, ok)
	}
	if _, ok := s.Point("nope"); ok {
		t.Errorf("Point(nope) ok=true, want false")
	}
}
