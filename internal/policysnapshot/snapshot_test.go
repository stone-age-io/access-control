package policysnapshot

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stone-age-io/access-control/internal/policy"
	"github.com/stone-age-io/access-control/internal/policykv"
)

// at is a fixed evaluation instant; windows are built relative to its weekday so
// the test is deterministic without depending on host tzdata (location tz is UTC).
var at = time.Date(2026, 6, 25, 14, 0, 0, 0, time.UTC)

func isoWD(t time.Time) int {
	wd := int(t.Weekday())
	if wd == 0 {
		return 7
	}
	return wd
}

func mk(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// baseEntries is a minimal grant setup: cred C1 → user u1 → role r1 → group g1
// (contains door1) under an open schedule, portal door1 at location hq (UTC),
// posture secure. Scenarios clone and mutate this.
func baseEntries(t *testing.T) map[string][]byte {
	open := policykv.Window{Days: []int{isoWD(at)}, Start: "00:00", End: "24:00"}
	return map[string][]byte{
		policykv.PrefixLocation + "hq":   mk(t, policykv.Location{Code: "hq", Timezone: "UTC"}),
		policykv.PrefixSched + "s1":      mk(t, policykv.Schedule{Code: "s1", Windows: []policykv.Window{open}, ObserveHolidays: true}),
		policykv.PrefixPortal + "door1":  mk(t, policykv.Portal{Code: "door1", Type: "door", Location: "hq", Posture: "secure", PulseSeconds: 5}),
		policykv.PrefixGroup + "g1":      mk(t, policykv.AccessGroup{Code: "g1", Portals: []string{"door1"}, Schedule: "s1"}),
		policykv.PrefixRole + "r1":       mk(t, policykv.Role{Code: "r1", Groups: []string{"g1"}}),
		policykv.PrefixUser + "u1":       mk(t, policykv.User{ID: "u1", Status: "active", Roles: []string{"r1"}}),
		policykv.PrefixCred + "C1":       mk(t, policykv.Credential{Value: "C1", User: "u1", Status: "active"}),
	}
}

func TestSimulate(t *testing.T) {
	closed := policykv.Window{Days: []int{isoWD(at)}, Start: "20:00", End: "23:00"} // 14:00 not inside

	tests := []struct {
		name       string
		mutate     func(map[string][]byte)
		cred       string
		portal     string
		override   string
		wantAllow  bool
		wantReason string
		wantSource string
	}{
		{
			name: "grant", cred: "C1", portal: "door1",
			wantAllow: true, wantReason: policy.ReasonAllowGrant, wantSource: SourceStanding,
		},
		{
			name: "unknown portal", cred: "C1", portal: "nope",
			wantReason: policy.ReasonDenyUnknownPoint, wantSource: SourceStanding,
		},
		{
			name: "unknown credential", cred: "ZZZ", portal: "door1",
			wantReason: policy.ReasonDenyUnknownCredential, wantSource: SourceStanding,
		},
		{
			name: "no access — group lacks portal",
			mutate: func(e map[string][]byte) {
				e[policykv.PrefixGroup+"g1"] = mk(t, policykv.AccessGroup{Code: "g1", Portals: []string{"otherdoor"}, Schedule: "s1"})
			},
			cred: "C1", portal: "door1",
			wantReason: policy.ReasonDenyNoAccess,
		},
		{
			name: "schedule closed",
			mutate: func(e map[string][]byte) {
				e[policykv.PrefixSched+"s1"] = mk(t, policykv.Schedule{Code: "s1", Windows: []policykv.Window{closed}})
			},
			cred: "C1", portal: "door1",
			wantReason: policy.ReasonDenyScheduleClosed,
		},
		{
			name: "revoked credential",
			mutate: func(e map[string][]byte) {
				e[policykv.PrefixCred+"C1"] = mk(t, policykv.Credential{Value: "C1", User: "u1", Status: "revoked"})
			},
			cred: "C1", portal: "door1",
			wantReason: policy.ReasonDenyRevoked,
		},
		{
			name: "expired credential",
			mutate: func(e map[string][]byte) {
				e[policykv.PrefixCred+"C1"] = mk(t, policykv.Credential{
					Value: "C1", User: "u1", Status: "active",
					ValidUntil: at.Add(-time.Hour).Format(time.RFC3339),
				})
			},
			cred: "C1", portal: "door1",
			wantReason: policy.ReasonDenyExpired,
		},
		{
			name: "lockdown posture (standing) beats a valid credential",
			mutate: func(e map[string][]byte) {
				e[policykv.PrefixPortal+"door1"] = mk(t, policykv.Portal{Code: "door1", Type: "door", Location: "hq", Posture: "lockdown", PulseSeconds: 5})
			},
			cred: "C1", portal: "door1",
			wantReason: policy.ReasonDenyLockdown,
		},
		{
			name: "override forces lockdown on a would-grant portal",
			cred: "C1", portal: "door1", override: "lockdown",
			wantReason: policy.ReasonDenyLockdown, wantSource: SourceOverride,
		},
		{
			name: "scheduled posture: auto_posture unlocked while window open",
			mutate: func(e map[string][]byte) {
				openNow := policykv.Window{Days: []int{isoWD(at)}, Start: "00:00", End: "24:00"}
				e[policykv.PrefixSched+"auto"] = mk(t, policykv.Schedule{Code: "auto", Windows: []policykv.Window{openNow}})
				e[policykv.PrefixPortal+"door1"] = mk(t, policykv.Portal{
					Code: "door1", Type: "door", Location: "hq", Posture: "secure", PulseSeconds: 5,
					AutoPosture: "unlocked", AutoSchedule: "auto",
				})
			},
			cred: "C1", portal: "door1",
			wantAllow: true, wantReason: policy.ReasonAllowPostureUnlocked, wantSource: SourceScheduled,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := baseEntries(t)
			if tc.mutate != nil {
				tc.mutate(e)
			}
			got := Build(e).Simulate(tc.cred, tc.portal, at, tc.override)
			if got.Allow != tc.wantAllow {
				t.Errorf("Allow = %v, want %v (reason %q)", got.Allow, tc.wantAllow, got.Reason)
			}
			if got.Reason != tc.wantReason {
				t.Errorf("Reason = %q, want %q", got.Reason, tc.wantReason)
			}
			if tc.wantSource != "" && got.PostureSource != tc.wantSource {
				t.Errorf("PostureSource = %q, want %q", got.PostureSource, tc.wantSource)
			}
		})
	}
}

// Holidays must close an observing schedule, mirroring the controller — proves the
// per-location calendar join in rebuildHolidays works end to end.
func TestSimulate_HolidayClosesSchedule(t *testing.T) {
	e := baseEntries(t)
	e[policykv.PrefixLocation+"hq"] = mk(t, policykv.Location{Code: "hq", Timezone: "UTC", HolidayCalendars: []string{"cal1"}})
	e[policykv.PrefixHoliday+"h1"] = mk(t, policykv.Holiday{Calendar: "cal1", Date: at.Format("2006-01-02")})

	got := Build(e).Simulate("C1", "door1", at, "")
	if got.Allow || got.Reason != policy.ReasonDenyScheduleClosed {
		t.Fatalf("holiday should close the schedule: got allow=%v reason=%q", got.Allow, got.Reason)
	}
}

// A malformed value must be skipped (fail closed), not crash the build.
func TestBuild_MalformedValueSkipped(t *testing.T) {
	e := baseEntries(t)
	e[policykv.PrefixCred+"C1"] = []byte("{not json")
	got := Build(e).Simulate("C1", "door1", at, "")
	if got.CredKnown || got.Reason != policy.ReasonDenyUnknownCredential {
		t.Fatalf("malformed cred should be absent: got credKnown=%v reason=%q", got.CredKnown, got.Reason)
	}
}
