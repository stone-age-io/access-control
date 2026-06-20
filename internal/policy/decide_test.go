package policy

import (
	"testing"
	"time"

	// Embed the timezone database so tz-dependent tests are hermetic on any OS
	// (Windows has no system zoneinfo).
	_ "time/tzdata"
)

func mustNY(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load America/New_York: %v", err)
	}
	return loc
}

// local builds the UTC instant corresponding to a given New_York wall-clock time.
// windowOpen/Decide convert back to local via loc, so this round-trips through tz.
func local(loc *time.Location, y int, mo time.Month, d, h, mi int) time.Time {
	return time.Date(y, mo, d, h, mi, 0, 0, loc).UTC()
}

func set(codes ...string) map[string]struct{} {
	m := make(map[string]struct{}, len(codes))
	for _, c := range codes {
		m[c] = struct{}{}
	}
	return m
}

func TestWindowOpen(t *testing.T) {
	loc := mustNY(t)
	// Reference dates (America/New_York): 2026-01-05 Mon, 01-09 Fri, 01-10 Sat,
	// 01-11 Sun; 2026-07-06 Mon (EDT).
	biz := Schedule{Windows: []Window{{Days: []int{1, 2, 3, 4, 5}, Start: "08:00", End: "17:00"}}}
	night := Schedule{Windows: []Window{{Days: []int{5}, Start: "22:00", End: "06:00"}}} // Fri nights
	allDay := Schedule{Windows: []Window{{Days: []int{1, 2, 3, 4, 5, 6, 7}, Start: "00:00", End: "24:00"}}}
	empty := Schedule{}

	tests := []struct {
		name string
		sch  Schedule
		at   time.Time
		want bool
	}{
		// same-day window, inclusive start / exclusive end
		{"biz mon 09:00", biz, local(loc, 2026, 1, 5, 9, 0), true},
		{"biz mon 08:00 start inclusive", biz, local(loc, 2026, 1, 5, 8, 0), true},
		{"biz mon 07:59 before", biz, local(loc, 2026, 1, 5, 7, 59), false},
		{"biz mon 16:59 in", biz, local(loc, 2026, 1, 5, 16, 59), true},
		{"biz mon 17:00 end exclusive", biz, local(loc, 2026, 1, 5, 17, 0), false},
		{"biz sat excluded day", biz, local(loc, 2026, 1, 10, 9, 0), false},
		{"biz sun excluded day", biz, local(loc, 2026, 1, 11, 9, 0), false},

		// DST: same wall-clock, different UTC offset (EST vs EDT) -> both open
		{"biz jan mon 09:00 EST", biz, local(loc, 2026, 1, 5, 9, 0), true},
		{"biz jul mon 09:00 EDT", biz, local(loc, 2026, 7, 6, 9, 0), true},

		// cross-midnight window (Fri 22:00 -> Sat 06:00)
		{"night fri 22:00 tail start", night, local(loc, 2026, 1, 9, 22, 0), true},
		{"night fri 21:59 before", night, local(loc, 2026, 1, 9, 21, 59), false},
		{"night fri 23:30 tail", night, local(loc, 2026, 1, 9, 23, 30), true},
		{"night sat 00:01 head", night, local(loc, 2026, 1, 10, 0, 1), true},
		{"night sat 05:59 head", night, local(loc, 2026, 1, 10, 5, 59), true},
		{"night sat 06:00 head end exclusive", night, local(loc, 2026, 1, 10, 6, 0), false},
		{"night sat 22:00 tail wrong day", night, local(loc, 2026, 1, 10, 22, 0), false},
		{"night sun 02:00 head wrong yesterday", night, local(loc, 2026, 1, 11, 2, 0), false},

		// all-day window
		{"allday sat 03:00", allDay, local(loc, 2026, 1, 10, 3, 0), true},
		{"allday mon 23:59", allDay, local(loc, 2026, 1, 5, 23, 59), true},

		// no windows
		{"empty mon 09:00", empty, local(loc, 2026, 1, 5, 9, 0), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := windowOpen(tc.sch, loc, tc.at, HolidaySet{}); got != tc.want {
				t.Errorf("windowOpen = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWindowOpenHolidays(t *testing.T) {
	loc := mustNY(t)
	// 2026-01-05 is a Monday; 2026-12-25 is a Friday; 2026-07-03 is a Friday.
	biz := Schedule{Windows: []Window{{Days: []int{1, 2, 3, 4, 5}, Start: "08:00", End: "17:00"}}, ObserveHolidays: true}
	bizNoObserve := Schedule{Windows: []Window{{Days: []int{1, 2, 3, 4, 5}, Start: "08:00", End: "17:00"}}} // ObserveHolidays false

	hol := HolidaySet{
		Dates:     map[string]struct{}{"2026-01-05": {}}, // explicit: this Monday
		Recurring: map[string]struct{}{"12-25": {}},      // recurring: Christmas every year
	}

	tests := []struct {
		name string
		sch  Schedule
		hol  HolidaySet
		at   time.Time
		want bool
	}{
		{"explicit holiday closes window", biz, hol, local(loc, 2026, 1, 5, 9, 0), false},
		{"non-holiday weekday open", biz, hol, local(loc, 2026, 1, 6, 9, 0), true},
		{"recurring holiday closes (Dec 25)", biz, hol, local(loc, 2026, 12, 25, 9, 0), false},
		{"recurring matches other years too", biz, HolidaySet{Recurring: map[string]struct{}{"12-25": {}}}, local(loc, 2030, 12, 25, 9, 0), false},
		{"observe=false ignores holiday", bizNoObserve, hol, local(loc, 2026, 1, 5, 9, 0), true},
		{"empty holiday set, observe=true, open", biz, HolidaySet{}, local(loc, 2026, 1, 6, 9, 0), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := windowOpen(tc.sch, loc, tc.at, tc.hol); got != tc.want {
				t.Errorf("windowOpen = %v, want %v", got, tc.want)
			}
		})
	}
}

// basePolicy: user u1 (active) -> role r1 -> group g1 (lobby, biz hours).
// vault exists but is in no group; garage does not exist.
func basePolicy() *Policy {
	return &Policy{
		Schedules: map[string]Schedule{
			"biz": {Windows: []Window{{Days: []int{1, 2, 3, 4, 5}, Start: "08:00", End: "17:00"}}},
		},
		Portals: map[string]Portal{
			"lobby": {Code: "lobby", Type: "door", Location: "hq", Posture: PostureSecure, PulseSeconds: 5},
			"vault": {Code: "vault", Type: "door", Location: "hq", Posture: PostureSecure, PulseSeconds: 3},
		},
		Groups: map[string]AccessGroup{
			"g1": {Code: "g1", Portals: set("lobby"), Schedule: "biz"},
		},
		Roles: map[string]Role{"r1": {Code: "r1", Groups: []string{"g1"}}},
		Users: map[string]User{"u1": {ID: "u1", Status: StatusActive, Roles: []string{"r1"}}},
		Creds: map[string]Credential{"C1": {Value: "C1", User: "u1", Status: StatusActive}},
	}
}

func TestDecide(t *testing.T) {
	loc := mustNY(t)
	mon0900 := local(loc, 2026, 1, 5, 9, 0)  // in business hours
	mon1800 := local(loc, 2026, 1, 5, 18, 0) // after hours
	sat0900 := local(loc, 2026, 1, 10, 9, 0) // weekend

	// variant policies for status-based denies
	revoked := basePolicy()
	revoked.Creds["C1"] = Credential{Value: "C1", User: "u1", Status: StatusRevoked}
	suspendedUser := basePolicy()
	suspendedUser.Users["u1"] = User{ID: "u1", Status: StatusSuspended, Roles: []string{"r1"}}

	// variant policies for credential validity bounds (evaluated at mon0900).
	notYetValid := basePolicy()
	notYetValid.Creds["C1"] = Credential{Value: "C1", User: "u1", Status: StatusActive,
		ValidFrom: local(loc, 2026, 2, 1, 0, 0)} // activates in February
	expired := basePolicy()
	expired.Creds["C1"] = Credential{Value: "C1", User: "u1", Status: StatusActive,
		ValidUntil: local(loc, 2025, 12, 31, 23, 59)} // expired last year
	withinValidity := basePolicy()
	withinValidity.Creds["C1"] = Credential{Value: "C1", User: "u1", Status: StatusActive,
		ValidFrom: local(loc, 2026, 1, 1, 0, 0), ValidUntil: local(loc, 2026, 12, 31, 23, 59)}

	tests := []struct {
		name       string
		p          *Policy
		posture    string
		cred       string
		portal     string
		at         time.Time
		wantAllow  bool
		wantReason string
		wantUser   string
		wantPulse  int
	}{
		{"grant in hours", basePolicy(), PostureSecure, "C1", "lobby", mon0900, true, ReasonAllowGrant, "u1", 5},
		{"schedule closed after hours", basePolicy(), PostureSecure, "C1", "lobby", mon1800, false, ReasonDenyScheduleClosed, "u1", 0},
		{"schedule closed weekend", basePolicy(), PostureSecure, "C1", "lobby", sat0900, false, ReasonDenyScheduleClosed, "u1", 0},
		{"unknown credential", basePolicy(), PostureSecure, "NOPE", "lobby", mon0900, false, ReasonDenyUnknownCredential, "", 0},
		{"unknown point", basePolicy(), PostureSecure, "C1", "garage", mon0900, false, ReasonDenyUnknownPoint, "", 0},
		{"no access to point", basePolicy(), PostureSecure, "C1", "vault", mon0900, false, ReasonDenyNoAccess, "u1", 0},
		{"revoked credential", revoked, PostureSecure, "C1", "lobby", mon0900, false, ReasonDenyRevoked, "u1", 0},
		{"suspended user", suspendedUser, PostureSecure, "C1", "lobby", mon0900, false, ReasonDenyRevoked, "u1", 0},
		{"lockdown beats grant", basePolicy(), PostureLockdown, "C1", "lobby", mon0900, false, ReasonDenyLockdown, "", 0},
		{"unlocked free passage", basePolicy(), PostureUnlocked, "NOPE", "lobby", mon0900, true, ReasonAllowPostureUnlocked, "", 5},
		{"free_access any tap opens", basePolicy(), PostureFreeAccess, "NOPE", "lobby", mon0900, true, ReasonAllowPostureFreeAccess, "", 5},
		{"disabled point", basePolicy(), PostureDisabled, "C1", "lobby", mon0900, false, ReasonDenyPointDisabled, "", 0},

		// credential validity bounds (deny-overrides, before the grant walk)
		{"credential not yet valid", notYetValid, PostureSecure, "C1", "lobby", mon0900, false, ReasonDenyNotYetValid, "u1", 0},
		{"credential expired", expired, PostureSecure, "C1", "lobby", mon0900, false, ReasonDenyExpired, "u1", 0},
		{"credential within validity grants", withinValidity, PostureSecure, "C1", "lobby", mon0900, true, ReasonAllowGrant, "u1", 5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Decide(tc.p, loc, tc.posture, tc.cred, tc.portal, tc.at)
			if got.Allow != tc.wantAllow {
				t.Errorf("Allow = %v, want %v", got.Allow, tc.wantAllow)
			}
			if got.Reason != tc.wantReason {
				t.Errorf("Reason = %q, want %q", got.Reason, tc.wantReason)
			}
			if got.User != tc.wantUser {
				t.Errorf("User = %q, want %q", got.User, tc.wantUser)
			}
			if got.Pulse != tc.wantPulse {
				t.Errorf("Pulse = %d, want %d", got.Pulse, tc.wantPulse)
			}
		})
	}
}
