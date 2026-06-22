package controller

import (
	"fmt"
	"testing"

	"github.com/stone-age-io/access-control/internal/drivers"
	"github.com/stone-age-io/access-control/internal/policy"
	"github.com/stone-age-io/access-control/internal/statuskv"
)

// Snapshot retains recent decisions oldest-first, with the credential verbatim
// (confirming the decoded card number is the point of the view).
func TestSnapshotCapturesDecisions(t *testing.T) {
	rt, _, _, _ := runtimeFor(t)
	at := ny(t, 2026, 1, 5, 9, 0)
	rt.handleTap(drivers.Tap{Portal: "lobby-main", Credential: "CARD-001", At: at}) // allow_grant
	rt.handleTap(drivers.Tap{Portal: "lobby-main", Credential: "NOPE", At: at})     // deny_unknown_credential

	snap := rt.Snapshot(at)
	if len(snap.Decisions) != 2 {
		t.Fatalf("decisions = %d, want 2", len(snap.Decisions))
	}
	if d := snap.Decisions[0]; !d.Allow || d.Cred != "CARD-001" || d.Reason != policy.ReasonAllowGrant || d.User != "alice" {
		t.Errorf("decision[0] = %+v, want allow CARD-001 alice allow_grant", d)
	}
	if d := snap.Decisions[1]; d.Allow || d.Cred != "NOPE" || d.Reason != policy.ReasonDenyUnknownCredential {
		t.Errorf("decision[1] = %+v, want deny NOPE unknown_credential", d)
	}
}

// A posture override surfaces in the portal's live view (posture + provenance +
// the override value), and a door with no input wired reads "unknown".
func TestSnapshotPortalLiveOverride(t *testing.T) {
	rt, _, _, _ := runtimeFor(t)
	at := ny(t, 2026, 1, 5, 8, 0)
	rt.SetPosture("lobby-main", policy.PostureUnlocked, "guard", "open house", at)

	p, ok := rt.Snapshot(at).Portals["lobby-main"]
	if !ok {
		t.Fatalf("lobby-main missing from snapshot portals")
	}
	if p.Posture != policy.PostureUnlocked || p.Source != statuskv.PostureSourceOverride || p.Override != policy.PostureUnlocked {
		t.Errorf("portal = %+v, want posture=unlocked source=override override=unlocked", p)
	}
	if p.Door != statuskv.DoorUnknown {
		t.Errorf("door = %q, want unknown (no input wired)", p.Door)
	}
}

// A forced open shows the door open in the snapshot and lands a forced alarm in
// the alarm ring.
func TestSnapshotDoorAndAlarm(t *testing.T) {
	rt, _, _ := monitorRuntime(t)
	at := ny(t, 2026, 1, 5, 9, 0)
	rt.handleDPS("lobby-main", false, at) // open with no grant/REX → forced

	snap := rt.Snapshot(at)
	if got := snap.Portals["lobby-main"].Door; got != statuskv.DoorOpen {
		t.Errorf("door = %q, want open", got)
	}
	if len(snap.Alarms) != 1 || snap.Alarms[0].Kind != AlarmForced || snap.Alarms[0].Portal != "lobby-main" {
		t.Errorf("alarms = %+v, want one forced for lobby-main", snap.Alarms)
	}
}

// The decision ring is bounded: it keeps the most recent recentRingSize entries,
// oldest-first, evicting the rest.
func TestDecisionRingWraps(t *testing.T) {
	rt, _, _, _ := runtimeFor(t)
	at := ny(t, 2026, 1, 5, 9, 0)
	for i := 0; i < recentRingSize+5; i++ {
		rt.handleTap(drivers.Tap{Portal: "lobby-main", Credential: fmt.Sprintf("C-%d", i), At: at})
	}

	d := rt.Snapshot(at).Decisions
	if len(d) != recentRingSize {
		t.Fatalf("decisions = %d, want %d", len(d), recentRingSize)
	}
	if d[0].Cred != "C-5" { // first 5 evicted
		t.Errorf("oldest cred = %q, want C-5", d[0].Cred)
	}
	if want := fmt.Sprintf("C-%d", recentRingSize+4); d[recentRingSize-1].Cred != want {
		t.Errorf("newest cred = %q, want %s", d[recentRingSize-1].Cred, want)
	}
}

// Ready reflects the boot default-deny window (false until the sync sentinel),
// and Counts reports what loaded.
func TestStoreReadyAndCounts(t *testing.T) {
	s := seeded(t)
	if s.Ready() {
		t.Errorf("Ready() = true before sync, want false (boot default-deny)")
	}
	if c := s.Counts(); c["portals"] != 1 || c["credentials"] != 1 || c["users"] != 1 ||
		c["controllers"] != 1 || c["schedules"] != 1 || c["bindings"] != 1 {
		t.Errorf("counts = %+v, want portals/credentials/users/controllers/schedules/bindings = 1", c)
	}

	// Closing the ready sentinel (as the watcher does on initial sync) flips Ready.
	s.readyOnce.Do(func() { close(s.ready) })
	if !s.Ready() {
		t.Errorf("Ready() = false after sync sentinel, want true")
	}
}
