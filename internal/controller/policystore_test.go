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
