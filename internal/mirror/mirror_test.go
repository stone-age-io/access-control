package mirror

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/stone-age-io/access-control/internal/policykv"

	// Side-effect import registers the schema + fixture migrations.
	_ "github.com/stone-age-io/access-control/pbmigrations"
)

func newApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(app.Cleanup)
	return app
}

func find(t *testing.T, app core.App, collection, field, value string) *core.Record {
	t.Helper()
	rec, err := app.FindFirstRecordByData(collection, field, value)
	if err != nil {
		t.Fatalf("find %s %s=%s: %v", collection, field, value, err)
	}
	return rec
}

// The credential key/value is the hot tap-path lookup. Verify the key and that
// the user field is the cardholder id (not a code).
func TestKeyAndValue_Credential(t *testing.T) {
	app := newApp(t)
	cred := find(t, app, "credentials", "value", "CARD-001")
	alice := find(t, app, "cardholders", "external_id", "alice")

	key, val, err := keyAndValue(app, cred)
	if err != nil {
		t.Fatalf("keyAndValue: %v", err)
	}
	if key != "cred.CARD-001" {
		t.Errorf("key = %q, want cred.CARD-001", key)
	}
	var got policykv.Credential
	if err := json.Unmarshal(val, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Value != "CARD-001" || got.Status != "active" || got.User != alice.Id {
		t.Errorf("credential = %+v, want value=CARD-001 status=active user=%s", got, alice.Id)
	}
}

// Relations must be resolved to stable codes, not PocketBase ids.
func TestKeyAndValue_GroupResolvesCodes(t *testing.T) {
	app := newApp(t)
	group := find(t, app, "access_groups", "code", "lobby-group")

	key, val, err := keyAndValue(app, group)
	if err != nil {
		t.Fatalf("keyAndValue: %v", err)
	}
	if key != "group.lobby-group" {
		t.Errorf("key = %q, want group.lobby-group", key)
	}
	var got policykv.AccessGroup
	if err := json.Unmarshal(val, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Schedule != "business-hours" {
		t.Errorf("schedule = %q, want business-hours", got.Schedule)
	}
	if len(got.Portals) != 1 || got.Portals[0] != "lobby-main" {
		t.Errorf("portals = %v, want [lobby-main]", got.Portals)
	}
}

func TestKeyAndValue_PortalFieldsAndDefaults(t *testing.T) {
	app := newApp(t)
	portal := find(t, app, "portals", "code", "lobby-main")

	key, val, err := keyAndValue(app, portal)
	if err != nil {
		t.Fatalf("keyAndValue: %v", err)
	}
	if key != "portal.lobby-main" {
		t.Errorf("key = %q, want portal.lobby-main", key)
	}
	var got policykv.Portal
	if err := json.Unmarshal(val, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Location != "hq" || got.Type != "door" || got.Posture != "secure" || got.PulseSeconds != 5 {
		t.Errorf("portal = %+v, want location=hq type=door posture=secure pulse=5", got)
	}
	// Hardware binding: controller relation resolves to a code, indices pass through.
	if got.Controller != "ctrl-hq-1" || got.LockRelay != 1 || got.DpsInput != 1 || got.RexInput != 2 || got.HeldOpenSeconds != 30 {
		t.Errorf("portal binding = %+v, want controller=ctrl-hq-1 relay=1 dps=1 rex=2 held=30", got)
	}
}

// A controller record keys under controller.<code> and resolves its location to
// a code.
func TestKeyAndValue_Controller(t *testing.T) {
	app := newApp(t)
	ctrl := find(t, app, "controllers", "code", "ctrl-hq-1")

	key, val, err := keyAndValue(app, ctrl)
	if err != nil {
		t.Fatalf("keyAndValue: %v", err)
	}
	if key != "controller.ctrl-hq-1" {
		t.Errorf("key = %q, want controller.ctrl-hq-1", key)
	}
	var got policykv.Controller
	if err := json.Unmarshal(val, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Code != "ctrl-hq-1" || got.Location != "hq" || got.Model != "kincony-server-mini" {
		t.Errorf("controller = %+v, want code=ctrl-hq-1 location=hq model=kincony-server-mini", got)
	}
}

func TestKeyAndValue_ScheduleWindows(t *testing.T) {
	app := newApp(t)
	sched := find(t, app, "schedules", "code", "business-hours")

	_, val, err := keyAndValue(app, sched)
	if err != nil {
		t.Fatalf("keyAndValue: %v", err)
	}
	var got policykv.Schedule
	if err := json.Unmarshal(val, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Windows) != 1 {
		t.Fatalf("windows = %v, want 1 window", got.Windows)
	}
	w := got.Windows[0]
	if w.Start != "08:00" || w.End != "17:00" || len(w.Days) != 5 {
		t.Errorf("window = %+v, want 08:00-17:00 on 5 days", w)
	}
	// ignore_holidays defaults false, so a schedule observes holidays by default.
	if !got.ObserveHolidays {
		t.Errorf("ObserveHolidays = false, want true (default observe)")
	}
}

// A holiday keys under holiday.<id>, resolves its calendar to a code, and emits
// the calendar-date part only.
func TestKeyAndValue_Holiday(t *testing.T) {
	app := newApp(t)
	cal := newCalendar(t, app, "us", "US Holidays")

	col, err := app.FindCollectionByNameOrId("holidays")
	if err != nil {
		t.Fatalf("collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("calendar", cal.Id)
	rec.Set("date", "2026-12-25 00:00:00.000Z")
	rec.Set("name", "Christmas")
	rec.Set("recurring", true)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save: %v", err)
	}

	key, val, err := keyAndValue(app, rec)
	if err != nil {
		t.Fatalf("keyAndValue: %v", err)
	}
	if want := "holiday." + rec.Id; key != want {
		t.Errorf("key = %q, want %q", key, want)
	}
	var got policykv.Holiday
	if err := json.Unmarshal(val, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Calendar != "us" || got.Date != "2026-12-25" || !got.Recurring {
		t.Errorf("holiday = %+v, want calendar=us date=2026-12-25 recurring=true", got)
	}
}

// A location resolves its holiday_calendars relation to a slice of calendar codes.
func TestKeyAndValue_LocationHolidayCalendars(t *testing.T) {
	app := newApp(t)
	us := newCalendar(t, app, "us", "US Holidays")
	plant := newCalendar(t, app, "plant-a", "Plant A shutdowns")

	location := find(t, app, "locations", "code", "hq")
	location.Set("holiday_calendars", []string{us.Id, plant.Id})
	if err := app.Save(location); err != nil {
		t.Fatalf("save location: %v", err)
	}

	_, val, err := keyAndValue(app, location)
	if err != nil {
		t.Fatalf("keyAndValue: %v", err)
	}
	var got policykv.Location
	if err := json.Unmarshal(val, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.HolidayCalendars) != 2 ||
		!slices.Contains(got.HolidayCalendars, "us") ||
		!slices.Contains(got.HolidayCalendars, "plant-a") {
		t.Errorf("holidayCalendars = %v, want [us plant-a]", got.HolidayCalendars)
	}
}

// newCalendar creates a holiday_calendars record and returns it.
func newCalendar(t *testing.T, app core.App, code, name string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("holiday_calendars")
	if err != nil {
		t.Fatalf("holiday_calendars collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("code", code)
	rec.Set("name", name)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save calendar %q: %v", code, err)
	}
	return rec
}

// An area keys under area.<code>, resolves location + auto_schedule to codes, and
// keeps a complete auto_arm/auto_schedule pair.
func TestKeyAndValue_Area(t *testing.T) {
	app := newApp(t)
	hq := find(t, app, "locations", "code", "hq")

	col, err := app.FindCollectionByNameOrId("areas")
	if err != nil {
		t.Fatalf("areas collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("code", "vault")
	rec.Set("name", "Vault")
	rec.Set("location", hq.Id)
	rec.Set("arm", "armed")
	sched := find(t, app, "schedules", "code", "business-hours")
	rec.Set("auto_arm", "armed")
	rec.Set("auto_schedule", sched.Id)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save area: %v", err)
	}

	key, val, err := keyAndValue(app, rec)
	if err != nil {
		t.Fatalf("keyAndValue: %v", err)
	}
	if key != "area.vault" {
		t.Errorf("key = %q, want area.vault", key)
	}
	var got policykv.Area
	if err := json.Unmarshal(val, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Location != "hq" || got.Arm != "armed" || got.AutoArm != "armed" || got.AutoSchedule != "business-hours" {
		t.Errorf("area = %+v, want location=hq arm=armed autoArm=armed autoSchedule=business-hours", got)
	}
}

// A half-configured auto_arm (no auto_schedule) is dropped — both-or-neither.
func TestKeyAndValue_AreaAutoArmBothOrNeither(t *testing.T) {
	app := newApp(t)
	hq := find(t, app, "locations", "code", "hq")

	col, err := app.FindCollectionByNameOrId("areas")
	if err != nil {
		t.Fatalf("areas collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("code", "dock-area")
	rec.Set("location", hq.Id)
	rec.Set("auto_arm", "armed") // auto_schedule intentionally empty
	if err := app.Save(rec); err != nil {
		t.Fatalf("save area: %v", err)
	}

	_, val, err := keyAndValue(app, rec)
	if err != nil {
		t.Fatalf("keyAndValue: %v", err)
	}
	var got policykv.Area
	if err := json.Unmarshal(val, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.AutoArm != "" || got.AutoSchedule != "" {
		t.Errorf("autoArm/autoSchedule = (%q,%q), want both empty (dropped)", got.AutoArm, got.AutoSchedule)
	}
}

// An aux_input resolves its area relation to a code and carries point_type.
func TestKeyAndValue_AuxInputAreaMembership(t *testing.T) {
	app := newApp(t)
	hq := find(t, app, "locations", "code", "hq")

	areaCol, _ := app.FindCollectionByNameOrId("areas")
	area := core.NewRecord(areaCol)
	area.Set("code", "zone1")
	area.Set("location", hq.Id)
	if err := app.Save(area); err != nil {
		t.Fatalf("save area: %v", err)
	}

	col, err := app.FindCollectionByNameOrId("aux_input")
	if err != nil {
		t.Fatalf("aux_input collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("code", "pir-1")
	rec.Set("location", hq.Id)
	rec.Set("area", area.Id)
	rec.Set("point_type", "intrusion")
	if err := app.Save(rec); err != nil {
		t.Fatalf("save aux_input: %v", err)
	}

	_, val, err := keyAndValue(app, rec)
	if err != nil {
		t.Fatalf("keyAndValue: %v", err)
	}
	var got policykv.AuxInput
	if err := json.Unmarshal(val, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Area != "zone1" || got.PointType != "intrusion" {
		t.Errorf("auxInput = %+v, want area=zone1 pointType=intrusion", got)
	}
}

// Cardholders are keyed by PocketBase id under the user. prefix.
func TestRecordKey_Cardholder(t *testing.T) {
	app := newApp(t)
	alice := find(t, app, "cardholders", "external_id", "alice")

	key, err := recordKey(alice)
	if err != nil {
		t.Fatalf("recordKey: %v", err)
	}
	if want := "user." + alice.Id; key != want {
		t.Errorf("key = %q, want %q", key, want)
	}
}

// A portal with no posture set defaults to "secure" at the mirror boundary.
func TestKeyAndValue_PostureDefault(t *testing.T) {
	app := newApp(t)
	location := find(t, app, "locations", "code", "hq")

	col, err := app.FindCollectionByNameOrId("portals")
	if err != nil {
		t.Fatalf("collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("code", "back-door")
	rec.Set("type", "door")
	rec.Set("location", location.Id)
	// posture intentionally left empty
	if err := app.Save(rec); err != nil {
		t.Fatalf("save: %v", err)
	}

	_, val, err := keyAndValue(app, rec)
	if err != nil {
		t.Fatalf("keyAndValue: %v", err)
	}
	var got policykv.Portal
	if err := json.Unmarshal(val, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Posture != "secure" {
		t.Errorf("posture = %q, want secure (default)", got.Posture)
	}
}
