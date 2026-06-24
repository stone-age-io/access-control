package controller

import (
	"testing"
	"time"

	"github.com/stone-age-io/access-control/internal/drivers"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/statuskv"
	"github.com/stone-age-io/access-control/internal/subjects"

	_ "time/tzdata"
)

// withArea seeds an area + one intrusion aux input on ctrl-hq-1 onto the fixture
// store and returns it.
func withArea(t *testing.T, areaJSON string) *PolicyStore {
	t.Helper()
	s := seeded(t)
	s.apply("area.zone1", []byte(areaJSON))
	s.apply("auxin.motion-1", []byte(`{"code":"motion-1","location":"hq","controller":"ctrl-hq-1","area":"zone1","pointType":"intrusion"}`))
	return s
}

func TestResolveArmStateStanding(t *testing.T) {
	at := ny(t, 2026, 1, 5, 9, 0)

	// Default/empty arm ⇒ disarmed.
	s := withArea(t, `{"code":"zone1","location":"hq"}`)
	if armed, src, resolved := s.ResolveArmState("zone1", at); armed || src != statuskv.PostureSourceStanding || !resolved {
		t.Errorf("empty arm = (%v,%q,%v), want (false,standing,true)", armed, src, resolved)
	}

	// Standing armed.
	s = withArea(t, `{"code":"zone1","location":"hq","arm":"armed"}`)
	if armed, src, _ := s.ResolveArmState("zone1", at); !armed || src != statuskv.PostureSourceStanding {
		t.Errorf("standing armed = (%v,%q), want (true,standing)", armed, src)
	}
}

// A durable arm_override beats both standing and scheduled.
func TestResolveArmStateOverrideWins(t *testing.T) {
	at := ny(t, 2026, 1, 5, 9, 0)
	s := withArea(t, `{"code":"zone1","location":"hq","arm":"armed","armOverride":"disarmed","autoArm":"armed","autoSchedule":"business-hours"}`)
	if armed, src, _ := s.ResolveArmState("zone1", at); armed || src != statuskv.PostureSourceOverride {
		t.Errorf("override disarmed = (%v,%q), want (false,override)", armed, src)
	}
}

// While the auto_schedule window is open, auto_arm applies; outside it, standing.
func TestResolveArmStateScheduled(t *testing.T) {
	s := withArea(t, `{"code":"zone1","location":"hq","arm":"disarmed","autoArm":"armed","autoSchedule":"business-hours"}`)

	// Mon 09:00 NY — window open ⇒ scheduled armed.
	if armed, src, _ := s.ResolveArmState("zone1", ny(t, 2026, 1, 5, 9, 0)); !armed || src != statuskv.PostureSourceScheduled {
		t.Errorf("in-window = (%v,%q), want (true,scheduled)", armed, src)
	}
	// Mon 18:00 NY — window closed ⇒ standing disarmed.
	if armed, src, _ := s.ResolveArmState("zone1", ny(t, 2026, 1, 5, 18, 0)); armed || src != statuskv.PostureSourceStanding {
		t.Errorf("out-window = (%v,%q), want (false,standing)", armed, src)
	}
}

// A holiday the location observes closes the auto-arm window (the day falls back
// to standing).
func TestResolveArmStateHolidayClosesAutoArm(t *testing.T) {
	s := withArea(t, `{"code":"zone1","location":"hq","arm":"disarmed","autoArm":"armed","autoSchedule":"business-hours"}`)
	s.apply("location.hq", []byte(`{"code":"hq","timezone":"America/New_York","holidayCalendars":["us"]}`))
	s.apply("sched.business-hours", []byte(`{"code":"business-hours","windows":[{"days":[1,2,3,4,5],"start":"08:00","end":"17:00"}],"observeHolidays":true}`))
	mon := ny(t, 2026, 1, 5, 9, 0)

	if armed, _, _ := s.ResolveArmState("zone1", mon); !armed {
		t.Fatalf("precondition: expected armed before holiday")
	}
	s.apply("holiday.h1", []byte(`{"calendar":"us","date":"2026-01-05","recurring":false}`))
	if armed, src, _ := s.ResolveArmState("zone1", mon); armed || src != statuskv.PostureSourceStanding {
		t.Errorf("on holiday = (%v,%q), want (false,standing) — auto-arm window closed", armed, src)
	}
}

// An auto_schedule that isn't loaded yet ⇒ standing value with resolved=false.
func TestResolveArmStateUnresolved(t *testing.T) {
	s := withArea(t, `{"code":"zone1","location":"hq","arm":"armed","autoArm":"disarmed","autoSchedule":"ghost-schedule"}`)
	if armed, _, resolved := s.ResolveArmState("zone1", ny(t, 2026, 1, 5, 9, 0)); !armed || resolved {
		t.Errorf("unresolved = (armed=%v,resolved=%v), want (true,false) — keep standing", armed, resolved)
	}
}

// An unknown area never arms (fail-safe disarmed).
func TestResolveArmStateUnknownArea(t *testing.T) {
	s := seeded(t)
	if armed, _, resolved := s.ResolveArmState("ghost", ny(t, 2026, 1, 5, 9, 0)); armed || !resolved {
		t.Errorf("unknown area = (armed=%v,resolved=%v), want (false,true)", armed, resolved)
	}
}

func TestAreasForControllerAndPeers(t *testing.T) {
	s := withArea(t, `{"code":"zone1","location":"hq","arm":"armed"}`)
	// A second member input on a different controller makes zone1 multi-controller.
	s.apply("auxin.motion-2", []byte(`{"code":"motion-2","location":"hq","controller":"ctrl-hq-2","area":"zone1","pointType":"intrusion"}`))

	got := s.AreasForController("ctrl-hq-1")
	if len(got) != 1 || got[0].Code != "zone1" {
		t.Fatalf("AreasForController(ctrl-hq-1) = %+v, want [zone1]", got)
	}
	if n := len(s.AreasForController("ctrl-other")); n != 0 {
		t.Errorf("AreasForController(ctrl-other) = %d, want 0", n)
	}

	peers := s.AreaControllers("zone1")
	if len(peers) != 2 || peers[0] != "ctrl-hq-1" || peers[1] != "ctrl-hq-2" {
		t.Errorf("AreaControllers(zone1) = %v, want sorted [ctrl-hq-1 ctrl-hq-2]", peers)
	}

	area, ptype, ok := s.AuxInputMeta("motion-1")
	if !ok || area != "zone1" || ptype != "intrusion" {
		t.Errorf("AuxInputMeta(motion-1) = (%q,%q,%v), want (zone1,intrusion,true)", area, ptype, ok)
	}
}

// A portal member (no aux_input) makes its area participated-in, and the portal's
// controller joins the peer set — membership unions portals with aux inputs.
func TestAreasForControllerPortalMember(t *testing.T) {
	s := seeded(t)
	s.apply("area.zone1", []byte(`{"code":"zone1","location":"hq","arm":"armed"}`))
	// lobby-main (ctrl-hq-1, seeded) joins zone1; a second box's portal too.
	s.apply("portal.lobby-main", []byte(`{"code":"lobby-main","type":"door","location":"hq","controller":"ctrl-hq-1","area":"zone1"}`))
	s.apply("portal.dock-door", []byte(`{"code":"dock-door","type":"door","location":"hq","controller":"ctrl-hq-2","area":"zone1"}`))

	got := s.AreasForController("ctrl-hq-1")
	if len(got) != 1 || got[0].Code != "zone1" {
		t.Fatalf("AreasForController(ctrl-hq-1) via portal = %+v, want [zone1]", got)
	}
	peers := s.AreaControllers("zone1")
	if len(peers) != 2 || peers[0] != "ctrl-hq-1" || peers[1] != "ctrl-hq-2" {
		t.Errorf("AreaControllers(zone1) = %v, want [ctrl-hq-1 ctrl-hq-2]", peers)
	}
}

// armedRuntime builds a runtime over a store seeded with an area + intrusion input
// and arms that input on the runtime. arm is the area's standing arm value.
func armedRuntime(t *testing.T, areaJSON, pointType string) (*Runtime, *fakeEmitter) {
	t.Helper()
	s := seeded(t)
	s.apply("area.zone1", []byte(areaJSON))
	s.apply("auxin.motion-1", []byte(`{"code":"motion-1","location":"hq","controller":"ctrl-hq-1","area":"zone1","pointType":"`+pointType+`"}`))
	emit := &fakeEmitter{}
	rt := NewRuntime("hq", s, drivers.NewMockReader(8), nil, nil, emit,
		subjects.Default(), logger.NewNopLogger(), nil)
	rt.ArmAuxInput("motion-1", "hq")
	return rt, emit
}

const intrusionSubject = "acc.hq.area.zone1.evt.alarm"

// An armed area's intrusion point raises an alarm on its rising edge.
func TestIntrusionArmedAlarms(t *testing.T) {
	rt, emit := armedRuntime(t, `{"code":"zone1","location":"hq","arm":"armed"}`, "intrusion")
	rt.handleInput(drivers.InputEvent{Kind: drivers.InputAux, Portal: "motion-1", Active: true, At: ny(t, 2026, 1, 5, 2, 0)})
	if !emit.hasSubject(intrusionSubject) {
		t.Errorf("armed intrusion trip did not emit %s", intrusionSubject)
	}
}

// A disarmed area's intrusion point is observe-only — no alarm.
func TestIntrusionDisarmedSilent(t *testing.T) {
	rt, emit := armedRuntime(t, `{"code":"zone1","location":"hq","arm":"disarmed"}`, "intrusion")
	rt.handleInput(drivers.InputEvent{Kind: drivers.InputAux, Portal: "motion-1", Active: true, At: ny(t, 2026, 1, 5, 2, 0)})
	if emit.hasSubject(intrusionSubject) {
		t.Errorf("disarmed intrusion trip emitted an alarm, want none")
	}
}

// A tamper_24h point alarms regardless of arm-state.
func TestIntrusionTamperAlwaysAlarms(t *testing.T) {
	rt, emit := armedRuntime(t, `{"code":"zone1","location":"hq","arm":"disarmed"}`, "tamper_24h")
	rt.handleInput(drivers.InputEvent{Kind: drivers.InputAux, Portal: "motion-1", Active: true, At: ny(t, 2026, 1, 5, 2, 0)})
	if !emit.hasSubject(intrusionSubject) {
		t.Errorf("tamper_24h trip did not emit an alarm while disarmed")
	}
}

// A monitor point never alarms, even while armed.
func TestIntrusionMonitorSilent(t *testing.T) {
	rt, emit := armedRuntime(t, `{"code":"zone1","location":"hq","arm":"armed"}`, "monitor")
	rt.handleInput(drivers.InputEvent{Kind: drivers.InputAux, Portal: "motion-1", Active: true, At: ny(t, 2026, 1, 5, 2, 0)})
	if emit.hasSubject(intrusionSubject) {
		t.Errorf("monitor point emitted an alarm, want none")
	}
}

// While the location fire input is active, intrusion alarms are suppressed (same
// gate as door alarms).
func TestIntrusionFireSuppressed(t *testing.T) {
	rt, emit := armedRuntime(t, `{"code":"zone1","location":"hq","arm":"armed"}`, "intrusion")
	rt.SetFire("hq", true, ny(t, 2026, 1, 5, 2, 0))
	rt.handleInput(drivers.InputEvent{Kind: drivers.InputAux, Portal: "motion-1", Active: true, At: ny(t, 2026, 1, 5, 2, 0)})
	if emit.hasSubject(intrusionSubject) {
		t.Errorf("intrusion alarm emitted while fire active, want suppressed")
	}
}

// A continuously-asserted point alarms once (the no-change dedup), not per report.
func TestIntrusionEdgeTriggeredOnce(t *testing.T) {
	rt, emit := armedRuntime(t, `{"code":"zone1","location":"hq","arm":"armed"}`, "intrusion")
	at := ny(t, 2026, 1, 5, 2, 0)
	rt.handleInput(drivers.InputEvent{Kind: drivers.InputAux, Portal: "motion-1", Active: true, At: at})
	rt.handleInput(drivers.InputEvent{Kind: drivers.InputAux, Portal: "motion-1", Active: true, At: at}) // still active
	if n := emit.countSubject(intrusionSubject); n != 1 {
		t.Errorf("intrusion alarms = %d, want 1 (dedup on no-change)", n)
	}
}

// forcedRuntime builds a runtime over a store seeded with an area and a lobby-main
// portal carrying the given KV record (so it can be made an area member). Ready to
// drive a DPS open with no grant (a forced condition).
func forcedRuntime(t *testing.T, areaJSON, portalJSON string) (*Runtime, *fakeEmitter) {
	t.Helper()
	s := seeded(t)
	s.apply("area.zone1", []byte(areaJSON))
	s.apply("portal.lobby-main", []byte(portalJSON))
	emit := &fakeEmitter{}
	rt := NewRuntime("hq", s, drivers.NewMockReader(8), nil, nil, emit,
		subjects.Default(), logger.NewNopLogger(), nil)
	return rt, emit
}

const memberPortal = `{"code":"lobby-main","type":"door","location":"hq","controller":"ctrl-hq-1","area":"zone1"}`

// A forced open on a member portal of an ARMED area escalates to an area intrusion
// alarm, in addition to the door-level forced event.
func TestForcedWhileArmedEscalatesToIntrusion(t *testing.T) {
	rt, emit := forcedRuntime(t, `{"code":"zone1","location":"hq","arm":"armed"}`, memberPortal)
	rt.handleDPS("lobby-main", false /* open */, ny(t, 2026, 1, 5, 2, 0))

	if got := countAlarm(emit, AlarmForced); got != 1 {
		t.Errorf("forced alarms = %d, want 1 (door-level forced still emits)", got)
	}
	if !emit.hasSubject(intrusionSubject) {
		t.Errorf("forced-while-armed did not escalate to %s", intrusionSubject)
	}
}

// A forced open while the area is DISARMED stays a plain forced event — no
// intrusion escalation.
func TestForcedWhileDisarmedNoIntrusion(t *testing.T) {
	rt, emit := forcedRuntime(t, `{"code":"zone1","location":"hq","arm":"disarmed"}`, memberPortal)
	rt.handleDPS("lobby-main", false, ny(t, 2026, 1, 5, 2, 0))

	if got := countAlarm(emit, AlarmForced); got != 1 {
		t.Errorf("forced alarms = %d, want 1", got)
	}
	if emit.hasSubject(intrusionSubject) {
		t.Errorf("disarmed forced open escalated to intrusion, want none")
	}
}

// A forced open on a portal with no area never escalates (membership gates it).
func TestForcedNonMemberPortalNoIntrusion(t *testing.T) {
	rt, emit := forcedRuntime(t, `{"code":"zone1","location":"hq","arm":"armed"}`,
		`{"code":"lobby-main","type":"door","location":"hq","controller":"ctrl-hq-1"}`) // no area
	rt.handleDPS("lobby-main", false, ny(t, 2026, 1, 5, 2, 0))

	if got := countAlarm(emit, AlarmForced); got != 1 {
		t.Errorf("forced alarms = %d, want 1", got)
	}
	if emit.hasSubject(intrusionSubject) {
		t.Errorf("non-member portal escalated to intrusion, want none")
	}
}

// An AUTHORIZED open (a prior grant) on an armed member portal is normal passage:
// no forced, no intrusion. The reader is what distinguishes a portal from a bare
// contact — a valid entry is not a break-in.
func TestGrantedOpenWhileArmedNoIntrusion(t *testing.T) {
	rt, emit := forcedRuntime(t, `{"code":"zone1","location":"hq","arm":"armed"}`, memberPortal)
	at := ny(t, 2026, 1, 5, 2, 0)
	rt.noteGrant("lobby-main", at)
	rt.handleDPS("lobby-main", false, at)

	if got := countAlarm(emit, AlarmForced); got != 0 {
		t.Errorf("forced alarms = %d, want 0 (authorized open)", got)
	}
	if emit.hasSubject(intrusionSubject) {
		t.Errorf("authorized open escalated to intrusion, want none")
	}
}

// fakeAreaShadow records the area-shadow writes the AreaManager makes.
type fakeAreaShadow struct {
	set     map[string]statuskv.AreaStatus
	deleted []string
}

func newFakeAreaShadow() *fakeAreaShadow {
	return &fakeAreaShadow{set: make(map[string]statuskv.AreaStatus)}
}

func (f *fakeAreaShadow) SetArea(code, location, arm, source string, peers []string, _ time.Time) {
	f.set[code] = statuskv.AreaStatus{Code: code, Location: location, Arm: arm, Source: source, Peers: peers}
	for i := range f.deleted { // a re-set un-deletes
		if f.deleted[i] == code {
			f.deleted = append(f.deleted[:i], f.deleted[i+1:]...)
			break
		}
	}
}

func (f *fakeAreaShadow) DeleteArea(code string) {
	delete(f.set, code)
	f.deleted = append(f.deleted, code)
}

// The AreaManager writes one shadow per participated-in area, stamped with the
// full peer set, and drops the shadow when the area leaves this box.
func TestAreaManagerReconcileWritesAndPrunes(t *testing.T) {
	s := withArea(t, `{"code":"zone1","location":"hq","arm":"armed"}`)
	s.apply("auxin.motion-2", []byte(`{"code":"motion-2","location":"hq","controller":"ctrl-hq-2","area":"zone1","pointType":"intrusion"}`))
	fake := newFakeAreaShadow()
	am := NewAreaManager("ctrl-hq-1", "hq", s, fake, logger.NewNopLogger())

	am.reconcile()
	got, ok := fake.set["zone1"]
	if !ok {
		t.Fatalf("no shadow written for zone1")
	}
	if got.Arm != statuskv.AreaArmed {
		t.Errorf("shadow arm = %q, want armed", got.Arm)
	}
	if len(got.Peers) != 2 || got.Peers[0] != "ctrl-hq-1" || got.Peers[1] != "ctrl-hq-2" {
		t.Errorf("shadow peers = %v, want [ctrl-hq-1 ctrl-hq-2]", got.Peers)
	}

	// Reassign both member inputs away from zone1: ctrl-hq-1 no longer participates.
	s.remove("auxin.motion-1")
	s.remove("auxin.motion-2")
	am.reconcile()
	if _, ok := fake.set["zone1"]; ok {
		t.Errorf("shadow for zone1 still present after the area left this box")
	}
	if len(fake.deleted) == 0 || fake.deleted[len(fake.deleted)-1] != "zone1" {
		t.Errorf("expected DeleteArea(zone1), deletes = %v", fake.deleted)
	}
}
