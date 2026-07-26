package badgeapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/policykv"
	"github.com/stone-age-io/access-control/internal/policysnapshot"
)

// These cover the PROJECTION — what a badge holder is shown for the non-door targets.
// The authorization itself is tested where it lives (internal/policy's DecideArea /
// DecideOutput tables, and policysnapshot's AreasFor/OutputsFor); what is specific to
// this package is turning graph codes into a screen without leaking codes, hardware, or
// an area's peers, and without ever asserting an arm-state it cannot resolve.
//
// No NATS is needed: policysnapshot.Build is pure, so a snapshot can be constructed
// from wire values directly.

// armGraph builds a snapshot granting cardholderID one area (both rights) and one aux output, under an always-open schedule. `area` is spliced in as given, so a
// test can vary arm-state and scheduling.
func armGraph(t *testing.T, cardholderID string, area policykv.Area) *policysnapshot.Snapshot {
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
		policykv.PrefixArea + area.Code: mk(area),
		policykv.PrefixAuxOutput + "test-gate": mk(policykv.AuxOutput{
			Code: "test-gate", Location: "hq", RelayIndex: 2,
		}),
		policykv.PrefixGroup + "g1": mk(policykv.AccessGroup{
			Code: "g1", Schedule: "always",
			Areas: []string{area.Code}, AuxOutputs: []string{"test-gate"},
			AreaRights: []string{"arm", "disarm"},
		}),
		policykv.PrefixRole + "r1": mk(policykv.Role{Code: "r1", Groups: []string{"g1"}}),
		policykv.PrefixUser + cardholderID: mk(policykv.User{
			ID: cardholderID, Status: "active", Roles: []string{"r1"},
		}),
		policykv.PrefixCred + "C-1": mk(policykv.Credential{
			Value: "C-1", User: cardholderID, Status: "active",
		}),
	})
}

// mkArea saves an areas record at the fixture's hq location.
func mkArea(t *testing.T, app core.App, code, name string, set map[string]any) *core.Record {
	t.Helper()
	hq, err := app.FindFirstRecordByData("locations", "code", "hq")
	if err != nil {
		t.Fatalf("fixture location hq: %v", err)
	}
	col, err := app.FindCollectionByNameOrId("areas")
	if err != nil {
		t.Fatalf("areas collection: %v", err)
	}
	r := core.NewRecord(col)
	r.Set("code", code)
	r.Set("name", name)
	r.Set("location", hq.Id)
	for k, v := range set {
		r.Set(k, v)
	}
	if err := app.Save(r); err != nil {
		t.Fatalf("save area %q: %v", code, err)
	}
	return r
}

func mkAuxOutput(t *testing.T, app core.App, code, name string, set map[string]any) *core.Record {
	t.Helper()
	hq, err := app.FindFirstRecordByData("locations", "code", "hq")
	if err != nil {
		t.Fatalf("fixture location hq: %v", err)
	}
	col, err := app.FindCollectionByNameOrId("aux_output")
	if err != nil {
		t.Fatalf("aux_output collection: %v", err)
	}
	r := core.NewRecord(col)
	r.Set("code", code)
	r.Set("name", name)
	r.Set("location", hq.Id)
	for k, v := range set {
		r.Set(k, v)
	}
	if err := app.Save(r); err != nil {
		t.Fatalf("save aux_output %q: %v", code, err)
	}
	return r
}

func testHandler(app core.App) *handler {
	return &handler{app: app, log: logger.NewNopLogger()}
}

func TestAreasForBadge(t *testing.T) {
	app := newApp(t)
	h := testHandler(app)
	rec := mkArea(t, app, "test-zone", "Test Zone", map[string]any{
		"arm": "armed", "allow_remote_arm": true,
	})

	snap := armGraph(t, "ch-1", policykv.Area{Code: "test-zone", Location: "hq", Arm: "armed"})
	got := h.areasForBadge(snap, "ch-1", time.Now().UTC())

	if len(got) != 1 {
		t.Fatalf("areas = %+v, want one", got)
	}
	a := got[0]
	if a.ID != rec.Id {
		t.Errorf("id = %q, want the record id %q (the routes take an id, not a code)", a.ID, rec.Id)
	}
	if a.Name != "Test Zone" {
		t.Errorf("name = %q, want Test Zone", a.Name)
	}
	if a.Location != "Headquarters" {
		t.Errorf("location = %q, want the display NAME (Headquarters), not the code", a.Location)
	}
	if !a.CanArm || !a.CanDisarm {
		t.Errorf("rights = {%v %v}, want both", a.CanArm, a.CanDisarm)
	}
	if !a.Remote {
		t.Error("remote = false, want true (allow_remote_arm is set)")
	}
	if a.State != AreaArmed {
		t.Errorf("state = %q, want armed", a.State)
	}
}

// The badge must never carry an area's KV code or its hardware relationships — the
// same rule badgePortal follows. Asserted on the marshaled JSON, because that is what
// actually reaches the device.
func TestBadgeAreaJSONCarriesNoCodeOrTopology(t *testing.T) {
	app := newApp(t)
	h := testHandler(app)
	mkArea(t, app, "test-zone", "Test Zone", map[string]any{"allow_remote_arm": true})
	mkAuxOutput(t, app, "test-gate", "Vehicle Gate", map[string]any{"allow_remote": true})

	snap := armGraph(t, "ch-1", policykv.Area{Code: "test-zone", Location: "hq"})
	payload, err := json.Marshal(map[string]any{
		"areas":   h.areasForBadge(snap, "ch-1", time.Now().UTC()),
		"outputs": h.outputsForBadge(snap, "ch-1"),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(payload)
	// The KV codes, the wiring, and the location code — none of which a badge holder
	// has any business knowing, and all of which are on the records this projection
	// reads from.
	for _, leak := range []string{"test-zone", "test-gate", "relay_index", "relayIndex", "controller", "peers", `"hq"`} {
		if strings.Contains(body, leak) {
			t.Errorf("badge payload leaks %q: %s", leak, body)
		}
	}
}

// An area whose arm-state cannot be resolved must report "unknown", never a guess. A
// badge that said "disarmed" about an area it could not resolve would be telling the
// holder the building is unprotected.
func TestAreaStateUnknownWhenUnresolved(t *testing.T) {
	app := newApp(t)
	h := testHandler(app)
	mkArea(t, app, "test-zone", "Test Zone", map[string]any{"allow_remote_arm": true})

	// auto_schedule names a schedule that is not in the snapshot: configured but
	// unresolvable, which is exactly the "don't know" case.
	snap := armGraph(t, "ch-1", policykv.Area{
		Code: "test-zone", Location: "hq", AutoArm: "armed", AutoSchedule: "not-loaded",
	})
	got := h.areasForBadge(snap, "ch-1", time.Now().UTC())
	if len(got) != 1 {
		t.Fatalf("areas = %+v, want one", got)
	}
	if got[0].State != AreaUnknown {
		t.Errorf("state = %q, want unknown", got[0].State)
	}
}

// A durable override outranks the standing state, so a holder's own disarm reflects
// back on their next poll rather than appearing not to have worked.
func TestAreaStateReflectsOverride(t *testing.T) {
	app := newApp(t)
	h := testHandler(app)
	mkArea(t, app, "test-zone", "Test Zone", map[string]any{
		"allow_remote_arm": true, "arm": "armed", "arm_override": "disarmed",
	})

	snap := armGraph(t, "ch-1", policykv.Area{
		Code: "test-zone", Location: "hq", Arm: "armed", ArmOverride: "disarmed",
	})
	got := h.areasForBadge(snap, "ch-1", time.Now().UTC())
	if len(got) != 1 || got[0].State != AreaDisarmed {
		t.Errorf("state = %+v, want disarmed (the override beats the standing armed)", got)
	}
}

// The override must be read from the POCKETBASE RECORD, not the policy snapshot.
//
// This is the bug the shape exists to avoid, and it is invisible to a test that keeps the
// two in step: a holder taps Disarm (which writes the record), the client immediately
// re-reads the badge, and the snapshot — behind the KV mirror plus a few seconds of cache —
// still says "armed". The holder concludes it did not work. So this test deliberately
// DESYNCS them, with the record ahead of the snapshot, exactly as production is for a
// second or two after every arm/disarm.
func TestAreaStatePrefersTheRecordOverAStaleSnapshot(t *testing.T) {
	app := newApp(t)
	h := testHandler(app)
	rec := mkArea(t, app, "test-zone", "Test Zone", map[string]any{
		"allow_remote_arm": true, "arm": "armed", "arm_override": "disarmed",
	})

	// The snapshot is stale: it has no override yet and its standing state is armed.
	stale := armGraph(t, "ch-1", policykv.Area{Code: "test-zone", Location: "hq", Arm: "armed"})

	got := h.areasForBadge(stale, "ch-1", time.Now().UTC())
	if len(got) != 1 {
		t.Fatalf("areas = %+v, want one", got)
	}
	if got[0].State != AreaDisarmed {
		t.Errorf("state = %q, want disarmed — the just-written record must win over a lagging snapshot", got[0].State)
	}

	// And the reverse direction, so this is about the record winning rather than about
	// "disarmed" winning.
	rec.Set("arm_override", "armed")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save: %v", err)
	}
	staleDisarmed := armGraph(t, "ch-1", policykv.Area{
		Code: "test-zone", Location: "hq", Arm: "disarmed",
	})
	got = h.areasForBadge(staleDisarmed, "ch-1", time.Now().UTC())
	if len(got) != 1 || got[0].State != AreaArmed {
		t.Errorf("state = %+v, want armed from the record", got)
	}
}

// With no override, the base state comes from the snapshot — that tier needs the schedule
// graph, the location's timezone and its holidays, none of which the record carries.
func TestAreaStateFallsBackToTheSnapshotBase(t *testing.T) {
	app := newApp(t)
	h := testHandler(app)
	mkArea(t, app, "test-zone", "Test Zone", map[string]any{"allow_remote_arm": true})

	snap := armGraph(t, "ch-1", policykv.Area{Code: "test-zone", Location: "hq", Arm: "armed"})
	got := h.areasForBadge(snap, "ch-1", time.Now().UTC())
	if len(got) != 1 || got[0].State != AreaArmed {
		t.Errorf("state = %+v, want armed from the snapshot's standing value", got)
	}
}

// A granted area with no PocketBase row (KV ahead of the DB, or mid-rename) is dropped
// rather than rendered as a nameless area with buttons.
func TestAreasForBadgeSkipsUnknownRecords(t *testing.T) {
	app := newApp(t)
	h := testHandler(app)
	// The graph grants an area code with no matching PocketBase row. (The dev fixture
	// seeds its own `warehouse` area, so this deliberately names one nothing creates.)
	snap := armGraph(t, "ch-1", policykv.Area{Code: "ghost-zone", Location: "hq"})
	if got := h.areasForBadge(snap, "ch-1", time.Now().UTC()); len(got) != 0 {
		t.Errorf("areas = %+v, want none (no matching record)", got)
	}
}

// The opt-in is reported, not enforced by omission: a holder whose grant is real but
// whose area is not remotely armable still sees it, because their badge does work at a
// keypad and hiding it would misinform them.
func TestAreaWithoutRemoteOptInStillListed(t *testing.T) {
	app := newApp(t)
	h := testHandler(app)
	mkArea(t, app, "test-zone", "Test Zone", nil) // allow_remote_arm defaults false

	snap := armGraph(t, "ch-1", policykv.Area{Code: "test-zone", Location: "hq"})
	got := h.areasForBadge(snap, "ch-1", time.Now().UTC())
	if len(got) != 1 {
		t.Fatalf("areas = %+v, want the area listed", got)
	}
	if got[0].Remote {
		t.Error("remote = true, want false (no opt-in)")
	}
	if !got[0].CanArm {
		t.Error("canArm = false; the grant is real, only remote use is withheld")
	}
}

func TestOutputsForBadge(t *testing.T) {
	app := newApp(t)
	h := testHandler(app)
	rec := mkAuxOutput(t, app, "test-gate", "Vehicle Gate", map[string]any{"allow_remote": true})

	snap := armGraph(t, "ch-1", policykv.Area{Code: "test-zone", Location: "hq"})
	got := h.outputsForBadge(snap, "ch-1")
	if len(got) != 1 {
		t.Fatalf("outputs = %+v, want one", got)
	}
	if got[0].ID != rec.Id || got[0].Name != "Vehicle Gate" || !got[0].Remote {
		t.Errorf("output = %+v, want the gate relay with remote enabled", got[0])
	}

	// An unknown cardholder gets nothing, not an error.
	if got := h.outputsForBadge(snap, "nobody"); len(got) != 0 {
		t.Errorf("outputs for an unknown cardholder = %+v, want none", got)
	}
}
