package mirror

import (
	"encoding/json"
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
