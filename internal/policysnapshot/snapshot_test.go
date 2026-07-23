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

// armEntries builds the minimal snapshot BaseArmState needs: a location, an area,
// and any schedules it references.
func armEntries(t *testing.T, loc policykv.Location, area policykv.Area, scheds ...policykv.Schedule) map[string][]byte {
	t.Helper()
	e := map[string][]byte{
		policykv.PrefixLocation + loc.Code: mk(t, loc),
		policykv.PrefixArea + area.Code:    mk(t, area),
	}
	for _, s := range scheds {
		e[policykv.PrefixSched+s.Code] = mk(t, s)
	}
	return e
}

// BaseArmState resolves the scheduled/standing arm-state (override excluded), the
// same tiers as the controller's ResolveArmState. It backs accessd's one-shot disarm
// release, so getting these cases right is the safety-relevant part.
func TestBaseArmState(t *testing.T) {
	hq := policykv.Location{Code: "hq", Timezone: "UTC"}
	openNow := policykv.Window{Days: []int{isoWD(at)}, Start: "00:00", End: "24:00"}
	closedNow := policykv.Window{Days: []int{isoWD(at)}, Start: "20:00", End: "23:00"} // 14:00 outside
	midnightOpen := policykv.Window{Days: []int{isoWD(at)}, Start: "12:00", End: "02:00"} // crosses midnight, open at 14:00

	tests := []struct {
		name         string
		entries      map[string][]byte
		area         string
		wantArmed    bool
		wantResolved bool
	}{
		{
			name:      "no schedule, standing armed",
			entries:   armEntries(t, hq, policykv.Area{Code: "a1", Location: "hq", Arm: "armed"}),
			area:      "a1", wantArmed: true, wantResolved: true,
		},
		{
			name:      "no schedule, standing disarmed",
			entries:   armEntries(t, hq, policykv.Area{Code: "a1", Location: "hq", Arm: "disarmed"}),
			area:      "a1", wantArmed: false, wantResolved: true,
		},
		{
			name:      "no schedule, empty arm defaults disarmed",
			entries:   armEntries(t, hq, policykv.Area{Code: "a1", Location: "hq"}),
			area:      "a1", wantArmed: false, wantResolved: true,
		},
		{
			name: "window open → auto_arm armed",
			entries: armEntries(t, hq,
				policykv.Area{Code: "a1", Location: "hq", Arm: "disarmed", AutoArm: "armed", AutoSchedule: "s1"},
				policykv.Schedule{Code: "s1", Windows: []policykv.Window{openNow}, ObserveHolidays: true}),
			area: "a1", wantArmed: true, wantResolved: true,
		},
		{
			name: "midnight-crossing window open → auto_arm armed",
			entries: armEntries(t, hq,
				policykv.Area{Code: "a1", Location: "hq", Arm: "disarmed", AutoArm: "armed", AutoSchedule: "s1"},
				policykv.Schedule{Code: "s1", Windows: []policykv.Window{midnightOpen}}),
			area: "a1", wantArmed: true, wantResolved: true,
		},
		{
			name: "window closed → standing (disarmed)",
			entries: armEntries(t, hq,
				policykv.Area{Code: "a1", Location: "hq", Arm: "disarmed", AutoArm: "armed", AutoSchedule: "s1"},
				policykv.Schedule{Code: "s1", Windows: []policykv.Window{closedNow}}),
			area: "a1", wantArmed: false, wantResolved: true,
		},
		{
			name: "window closed → standing (armed)",
			entries: armEntries(t, hq,
				policykv.Area{Code: "a1", Location: "hq", Arm: "armed", AutoArm: "disarmed", AutoSchedule: "s1"},
				policykv.Schedule{Code: "s1", Windows: []policykv.Window{closedNow}}),
			area: "a1", wantArmed: true, wantResolved: true,
		},
		{
			name: "holiday closes schedule → standing (disarmed)",
			entries: func() map[string][]byte {
				e := armEntries(t,
					policykv.Location{Code: "hq", Timezone: "UTC", HolidayCalendars: []string{"cal1"}},
					policykv.Area{Code: "a1", Location: "hq", Arm: "disarmed", AutoArm: "armed", AutoSchedule: "s1"},
					policykv.Schedule{Code: "s1", Windows: []policykv.Window{openNow}, ObserveHolidays: true})
				e[policykv.PrefixHoliday+"h1"] = mk(t, policykv.Holiday{Calendar: "cal1", Date: at.Format("2006-01-02")})
				return e
			}(),
			area: "a1", wantArmed: false, wantResolved: true,
		},
		{
			name: "auto_schedule set but schedule missing → unresolved",
			entries: armEntries(t, hq,
				policykv.Area{Code: "a1", Location: "hq", Arm: "armed", AutoArm: "armed", AutoSchedule: "ghost"}),
			area: "a1", wantArmed: true, wantResolved: false, // returns standing, resolved=false
		},
		{
			name:      "unknown area → unresolved",
			entries:   armEntries(t, hq, policykv.Area{Code: "a1", Location: "hq", Arm: "armed"}),
			area:      "other", wantArmed: false, wantResolved: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			armed, resolved := Build(tc.entries).baseArmState(tc.area, at)
			if resolved != tc.wantResolved {
				t.Errorf("resolved = %v, want %v", resolved, tc.wantResolved)
			}
			if armed != tc.wantArmed {
				t.Errorf("armed = %v, want %v", armed, tc.wantArmed)
			}
		})
	}
}

// ShouldReleaseDisarm is the one-shot disarm gate: release only a SCHEDULED area whose
// base arm-state is now disarmed; a standing-only area (or an unresolved/unknown one) is
// never released here.
func TestShouldReleaseDisarm(t *testing.T) {
	hq := policykv.Location{Code: "hq", Timezone: "UTC"}
	openNow := policykv.Window{Days: []int{isoWD(at)}, Start: "00:00", End: "24:00"}
	closedNow := policykv.Window{Days: []int{isoWD(at)}, Start: "20:00", End: "23:00"}

	tests := []struct {
		name    string
		entries map[string][]byte
		area    string
		want    bool
	}{
		{
			name: "scheduled, window closed, standing disarmed → release",
			entries: armEntries(t, hq,
				policykv.Area{Code: "a1", Location: "hq", Arm: "disarmed", AutoArm: "armed", AutoSchedule: "s1"},
				policykv.Schedule{Code: "s1", Windows: []policykv.Window{closedNow}}),
			area: "a1", want: true,
		},
		{
			name: "scheduled, window open (base armed) → keep",
			entries: armEntries(t, hq,
				policykv.Area{Code: "a1", Location: "hq", Arm: "disarmed", AutoArm: "armed", AutoSchedule: "s1"},
				policykv.Schedule{Code: "s1", Windows: []policykv.Window{openNow}}),
			area: "a1", want: false,
		},
		{
			name:    "no schedule → never release (sticky)",
			entries: armEntries(t, hq, policykv.Area{Code: "a1", Location: "hq", Arm: "disarmed"}),
			area:    "a1", want: false,
		},
		{
			name: "schedule set but missing (unresolved) → keep",
			entries: armEntries(t, hq,
				policykv.Area{Code: "a1", Location: "hq", Arm: "disarmed", AutoArm: "armed", AutoSchedule: "ghost"}),
			area: "a1", want: false,
		},
		{
			name:    "unknown area → keep",
			entries: armEntries(t, hq, policykv.Area{Code: "a1", Location: "hq", Arm: "disarmed"}),
			area:    "other", want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Build(tc.entries).ShouldReleaseDisarm(tc.area, at); got != tc.want {
				t.Errorf("ShouldReleaseDisarm = %v, want %v", got, tc.want)
			}
		})
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
