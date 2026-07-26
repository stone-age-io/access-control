package policy

import (
	"testing"
	"time"

	_ "time/tzdata"
)

// targetGraph builds a policy graph with one area, one aux output, and three groups
// that differ only in which rights they carry — so a test's name says exactly which
// dimension it is exercising.
//
//	warehouse-both     area warehouse, arm+disarm, business hours
//	warehouse-arm-only area warehouse, arm only,   business hours
//	warehouse-norights area warehouse, NO rights,  business hours  (misconfiguration)
//	gate-group         output gate-relay,          business hours
//	night-both         area warehouse, arm+disarm, Fri nights only
func targetGraph() *Policy {
	biz := Schedule{Windows: []Window{{Days: []int{1, 2, 3, 4, 5}, Start: "08:00", End: "17:00"}}}
	night := Schedule{Windows: []Window{{Days: []int{5}, Start: "22:00", End: "23:59"}}}

	return &Policy{
		Schedules: map[string]Schedule{"biz": biz, "night": night},
		Areas:     map[string]Area{"warehouse": {Code: "warehouse", Location: "dc"}},
		Outputs:   map[string]Output{"gate-relay": {Code: "gate-relay", Location: "dc"}},
		Groups: map[string]AccessGroup{
			"warehouse-both": {
				Code: "warehouse-both", Schedule: "biz",
				Areas: set("warehouse"), CanArm: true, CanDisarm: true,
			},
			"warehouse-arm-only": {
				Code: "warehouse-arm-only", Schedule: "biz",
				Areas: set("warehouse"), CanArm: true,
			},
			"warehouse-norights": {
				Code: "warehouse-norights", Schedule: "biz",
				Areas: set("warehouse"),
			},
			"gate-group": {
				Code: "gate-group", Schedule: "biz",
				Outputs: set("gate-relay"),
			},
			"night-both": {
				Code: "night-both", Schedule: "night",
				Areas: set("warehouse"), CanArm: true, CanDisarm: true,
			},
		},
		Roles: map[string]Role{
			"both":      {Code: "both", Groups: []string{"warehouse-both"}},
			"armonly":   {Code: "armonly", Groups: []string{"warehouse-arm-only"}},
			"norights":  {Code: "norights", Groups: []string{"warehouse-norights"}},
			"gate":      {Code: "gate", Groups: []string{"gate-group"}},
			"nightly":   {Code: "nightly", Groups: []string{"night-both"}},
			"doorsonly": {Code: "doorsonly", Groups: []string{}},
			// Two groups on one area, each with one right: the union must grant both.
			"split":    {Code: "split", Groups: []string{"warehouse-arm-only", "night-both"}},
			"dangling": {Code: "dangling", Groups: []string{"no-such-group"}},
		},
		Users: map[string]User{
			"u-both":      {ID: "u-both", Status: StatusActive, Roles: []string{"both"}},
			"u-armonly":   {ID: "u-armonly", Status: StatusActive, Roles: []string{"armonly"}},
			"u-norights":  {ID: "u-norights", Status: StatusActive, Roles: []string{"norights"}},
			"u-gate":      {ID: "u-gate", Status: StatusActive, Roles: []string{"gate"}},
			"u-nightly":   {ID: "u-nightly", Status: StatusActive, Roles: []string{"nightly"}},
			"u-doors":     {ID: "u-doors", Status: StatusActive, Roles: []string{"doorsonly"}},
			"u-dangling":  {ID: "u-dangling", Status: StatusActive, Roles: []string{"dangling"}},
			"u-suspended": {ID: "u-suspended", Status: StatusSuspended, Roles: []string{"both"}},
		},
		Creds: map[string]Credential{
			"C-BOTH":      {Value: "C-BOTH", User: "u-both", Status: StatusActive},
			"C-ARMONLY":   {Value: "C-ARMONLY", User: "u-armonly", Status: StatusActive},
			"C-NORIGHTS":  {Value: "C-NORIGHTS", User: "u-norights", Status: StatusActive},
			"C-GATE":      {Value: "C-GATE", User: "u-gate", Status: StatusActive},
			"C-NIGHTLY":   {Value: "C-NIGHTLY", User: "u-nightly", Status: StatusActive},
			"C-DOORS":     {Value: "C-DOORS", User: "u-doors", Status: StatusActive},
			"C-DANGLING":  {Value: "C-DANGLING", User: "u-dangling", Status: StatusActive},
			"C-SUSPENDED": {Value: "C-SUSPENDED", User: "u-suspended", Status: StatusActive},
			"C-REVOKED":   {Value: "C-REVOKED", User: "u-both", Status: StatusRevoked},
			"C-ORPHAN":    {Value: "C-ORPHAN", User: "u-nobody", Status: StatusActive},
			"C-EXPIRED": {
				Value: "C-EXPIRED", User: "u-both", Status: StatusActive,
				ValidUntil: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		},
		Holidays: map[string]HolidaySet{},
	}
}

func TestDecideArea(t *testing.T) {
	loc := mustNY(t)
	p := targetGraph()

	monMidday := local(loc, 2026, 1, 5, 12, 0) // Mon, inside business hours
	monNight := local(loc, 2026, 1, 5, 23, 0)  // Mon, outside business hours
	friNight := local(loc, 2026, 1, 9, 22, 30) // Fri 22:30, inside "night"

	tests := []struct {
		name       string
		cred       string
		area       string
		action     string
		at         time.Time
		wantAllow  bool
		wantReason string
	}{
		// The grant, both ways round.
		{"disarm with both rights in window", "C-BOTH", "warehouse", ArmActionDisarm, monMidday, true, ReasonAllowAreaGrant},
		{"arm with both rights in window", "C-BOTH", "warehouse", ArmActionArm, monMidday, true, ReasonAllowAreaGrant},

		// The asymmetry that justifies two rights: closing staff may arm, not disarm.
		{"arm-only may arm", "C-ARMONLY", "warehouse", ArmActionArm, monMidday, true, ReasonAllowAreaGrant},
		{"arm-only may NOT disarm", "C-ARMONLY", "warehouse", ArmActionDisarm, monMidday, false, ReasonDenyNoAreaRight},

		// Empty area_rights grants neither, and says so distinctly so the operator can
		// tell a misconfigured group from a person with no access.
		{"no rights cannot arm", "C-NORIGHTS", "warehouse", ArmActionArm, monMidday, false, ReasonDenyNoAreaRight},
		{"no rights cannot disarm", "C-NORIGHTS", "warehouse", ArmActionDisarm, monMidday, false, ReasonDenyNoAreaRight},

		// Schedule. deny_schedule_closed outranks deny_no_area_right only when a group
		// actually held the right — otherwise the right is the more useful complaint.
		{"right held but window closed", "C-BOTH", "warehouse", ArmActionDisarm, monNight, false, ReasonDenyScheduleClosed},
		{"night group in its window", "C-NIGHTLY", "warehouse", ArmActionDisarm, friNight, true, ReasonAllowAreaGrant},
		{"night group outside its window", "C-NIGHTLY", "warehouse", ArmActionDisarm, monMidday, false, ReasonDenyScheduleClosed},

		// No group mentions the area at all.
		{"no group has the area", "C-DOORS", "warehouse", ArmActionDisarm, monMidday, false, ReasonDenyNoAccess},
		{"dangling group reference", "C-DANGLING", "warehouse", ArmActionDisarm, monMidday, false, ReasonDenyNoAccess},

		// Unknown target and unknown action both fail closed.
		{"unknown area", "C-BOTH", "no-such-area", ArmActionDisarm, monMidday, false, ReasonDenyUnknownArea},
		{"unrecognized action", "C-BOTH", "warehouse", "silence", monMidday, false, ReasonDenyNoAreaRight},

		// The shared credential ladder, which must behave exactly as at a door.
		{"unknown credential", "C-NOPE", "warehouse", ArmActionDisarm, monMidday, false, ReasonDenyUnknownCredential},
		{"revoked credential", "C-REVOKED", "warehouse", ArmActionDisarm, monMidday, false, ReasonDenyRevoked},
		{"expired credential", "C-EXPIRED", "warehouse", ArmActionDisarm, monMidday, false, ReasonDenyExpired},
		{"suspended cardholder", "C-SUSPENDED", "warehouse", ArmActionDisarm, monMidday, false, ReasonDenyRevoked},
		{"credential names nobody", "C-ORPHAN", "warehouse", ArmActionDisarm, monMidday, false, ReasonDenyRevoked},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DecideArea(p, loc, tc.cred, tc.area, tc.action, tc.at)
			if got.Allow != tc.wantAllow || got.Reason != tc.wantReason {
				t.Errorf("DecideArea(%s, %s, %s) = {allow:%v reason:%q}, want {allow:%v reason:%q}",
					tc.cred, tc.area, tc.action, got.Allow, got.Reason, tc.wantAllow, tc.wantReason)
			}
		})
	}
}

// An area grant must never pulse a strike: Decision.Pulse is a door field, and a
// caller that piped an area decision into a lock driver should get zero.
func TestDecideAreaNeverPulses(t *testing.T) {
	loc := mustNY(t)
	got := DecideArea(targetGraph(), loc, "C-BOTH", "warehouse", ArmActionDisarm, local(loc, 2026, 1, 5, 12, 0))
	if !got.Allow {
		t.Fatalf("precondition failed: want an allow, got %q", got.Reason)
	}
	if got.Pulse != 0 {
		t.Errorf("Pulse = %d, want 0 (an area has no strike)", got.Pulse)
	}
}

// Two groups granting one right each must union to both, exactly as two groups
// granting one portal each union their schedules.
func TestDecideAreaUnionsRightsAcrossGroups(t *testing.T) {
	loc := mustNY(t)
	p := targetGraph()
	p.Users["u-split"] = User{ID: "u-split", Status: StatusActive, Roles: []string{"split"}}
	p.Creds["C-SPLIT"] = Credential{Value: "C-SPLIT", User: "u-split", Status: StatusActive}

	// warehouse-arm-only (biz hours, arm) + night-both (Fri nights, arm+disarm).
	monMidday := local(loc, 2026, 1, 5, 12, 0)
	friNight := local(loc, 2026, 1, 9, 22, 30)

	if d := DecideArea(p, loc, "C-SPLIT", "warehouse", ArmActionArm, monMidday); !d.Allow {
		t.Errorf("arm during business hours = %q, want allow (arm-only group covers it)", d.Reason)
	}
	if d := DecideArea(p, loc, "C-SPLIT", "warehouse", ArmActionDisarm, monMidday); d.Allow {
		t.Error("disarm during business hours allowed; only the NIGHT group grants disarm")
	}
	if d := DecideArea(p, loc, "C-SPLIT", "warehouse", ArmActionDisarm, friNight); !d.Allow {
		t.Errorf("disarm on Friday night = %q, want allow (night group grants disarm)", d.Reason)
	}
}

// A holiday closes an area grant's window the same way it closes a door's, evaluated
// against the AREA's location rather than a portal's.
func TestDecideAreaObservesAreaLocationHolidays(t *testing.T) {
	loc := mustNY(t)
	p := targetGraph()
	p.Schedules["biz"] = Schedule{
		Windows:         []Window{{Days: []int{1, 2, 3, 4, 5}, Start: "08:00", End: "17:00"}},
		ObserveHolidays: true,
	}
	monMidday := local(loc, 2026, 1, 5, 12, 0)

	if d := DecideArea(p, loc, "C-BOTH", "warehouse", ArmActionDisarm, monMidday); !d.Allow {
		t.Fatalf("precondition: want allow on an ordinary Monday, got %q", d.Reason)
	}
	// A holiday at a DIFFERENT site must not close it...
	p.Holidays["hq"] = HolidaySet{Dates: map[string]struct{}{"2026-01-05": {}}}
	if d := DecideArea(p, loc, "C-BOTH", "warehouse", ArmActionDisarm, monMidday); !d.Allow {
		t.Errorf("a holiday at hq closed an area at dc: %q", d.Reason)
	}
	// ...but one at the area's own site must.
	p.Holidays["dc"] = HolidaySet{Dates: map[string]struct{}{"2026-01-05": {}}}
	if d := DecideArea(p, loc, "C-BOTH", "warehouse", ArmActionDisarm, monMidday); d.Allow {
		t.Error("a holiday at the area's own location did not close the window")
	}
}

func TestDecideOutput(t *testing.T) {
	loc := mustNY(t)
	p := targetGraph()
	monMidday := local(loc, 2026, 1, 5, 12, 0)
	monNight := local(loc, 2026, 1, 5, 23, 0)

	tests := []struct {
		name       string
		cred       string
		output     string
		at         time.Time
		wantAllow  bool
		wantReason string
	}{
		{"granted in window", "C-GATE", "gate-relay", monMidday, true, ReasonAllowOutputGrant},
		{"granted, window closed", "C-GATE", "gate-relay", monNight, false, ReasonDenyScheduleClosed},
		{"no group has the output", "C-BOTH", "gate-relay", monMidday, false, ReasonDenyNoAccess},
		{"unknown output", "C-GATE", "no-such-relay", monMidday, false, ReasonDenyUnknownOutput},
		{"revoked credential", "C-REVOKED", "gate-relay", monMidday, false, ReasonDenyRevoked},
		{"unknown credential", "C-NOPE", "gate-relay", monMidday, false, ReasonDenyUnknownCredential},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DecideOutput(p, loc, tc.cred, tc.output, tc.at)
			if got.Allow != tc.wantAllow || got.Reason != tc.wantReason {
				t.Errorf("DecideOutput(%s, %s) = {allow:%v reason:%q}, want {allow:%v reason:%q}",
					tc.cred, tc.output, got.Allow, got.Reason, tc.wantAllow, tc.wantReason)
			}
		})
	}
}

// A door grant is not an area grant and vice versa. The three target kinds share a
// group and a schedule but not each other's membership, which is the whole point of
// keeping them as separate relations.
func TestTargetKindsDoNotLeakIntoEachOther(t *testing.T) {
	loc := mustNY(t)
	p := targetGraph()
	p.Portals = map[string]Portal{"dc-main": {Code: "dc-main", Location: "dc", Posture: PostureSecure}}
	// warehouse-both grants the AREA only — no portals, no outputs.
	monMidday := local(loc, 2026, 1, 5, 12, 0)

	if d := Decide(p, loc, PostureSecure, "C-BOTH", "dc-main", monMidday); d.Allow {
		t.Error("an area grant opened a door")
	}
	if d := DecideOutput(p, loc, "C-BOTH", "gate-relay", monMidday); d.Allow {
		t.Error("an area grant drove an aux output")
	}
	if d := DecideArea(p, loc, "C-GATE", "warehouse", ArmActionDisarm, monMidday); d.Allow {
		t.Error("an output grant disarmed an area")
	}
}

// A zero Policy must deny every target kind, not just portals.
func TestZeroPolicyDeniesEveryTarget(t *testing.T) {
	var p Policy
	at := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)

	if d := DecideArea(&p, time.UTC, "any", "any", ArmActionDisarm, at); d.Allow {
		t.Errorf("zero policy allowed an area disarm: %q", d.Reason)
	}
	if d := DecideOutput(&p, time.UTC, "any", "any", at); d.Allow {
		t.Errorf("zero policy allowed an output: %q", d.Reason)
	}
}

func TestArmRights(t *testing.T) {
	tests := []struct {
		name             string
		in               []string
		wantArm, wantDis bool
	}{
		{"nil grants neither", nil, false, false},
		{"empty grants neither", []string{}, false, false},
		{"arm only", []string{"arm"}, true, false},
		{"disarm only", []string{"disarm"}, false, true},
		{"both", []string{"arm", "disarm"}, true, true},
		{"order irrelevant", []string{"disarm", "arm"}, true, true},
		{"unknown entry ignored", []string{"silence"}, false, false},
		{"unknown alongside known", []string{"silence", "arm"}, true, false},
		{"duplicates harmless", []string{"arm", "arm"}, true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotArm, gotDis := ArmRights(tc.in)
			if gotArm != tc.wantArm || gotDis != tc.wantDis {
				t.Errorf("ArmRights(%v) = (%v, %v), want (%v, %v)", tc.in, gotArm, gotDis, tc.wantArm, tc.wantDis)
			}
		})
	}
}
