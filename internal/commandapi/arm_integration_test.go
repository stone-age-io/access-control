package commandapi_test

import (
	"net/http"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// seedArea creates an area and returns its id.
func seedArea(t *testing.T, app core.App) string {
	t.Helper()
	loc, err := app.FindFirstRecordByData("locations", "code", "hq")
	if err != nil {
		t.Fatalf("hq location: %v", err)
	}
	col, err := app.FindCollectionByNameOrId("areas")
	if err != nil {
		t.Fatalf("areas collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("code", "vault")
	rec.Set("location", loc.Id)
	rec.Set("arm", "disarmed")
	if err := app.Save(rec); err != nil {
		t.Fatalf("seed area: %v", err)
	}
	return rec.Id
}

// A `command` operator can arm an area: it sets the durable arm_override and
// writes an audit_logs row.
func TestArmSetsOverrideAndAudits(t *testing.T) {
	app := newApp(t)
	commander := seedUser(t, app, "arm@cmd.dev", "command")
	tok, err := commander.NewAuthToken()
	if err != nil {
		t.Fatalf("auth token: %v", err)
	}
	id := seedArea(t, app)

	scenario := tests.ApiScenario{
		Name:                  "command operator arms an area",
		Method:                http.MethodPost,
		URL:                   "/api/areas/" + id + "/arm",
		Headers:               map[string]string{"Authorization": tok},
		ExpectedStatus:        http.StatusOK,
		ExpectedContent:       []string{`"ok":true`},
		TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		DisableTestAppCleanup: true,
		BeforeTestFunc:        func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) { registerBridge(e) },
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			rec, err := app.FindRecordById("areas", id)
			if err != nil {
				t.Fatalf("reload area: %v", err)
			}
			if rec.GetString("arm_override") != "armed" {
				t.Errorf("arm_override = %q, want armed", rec.GetString("arm_override"))
			}
			logs, _ := app.FindAllRecords("audit_logs")
			found := false
			for _, l := range logs {
				if l.GetString("collection_name") == "areas" && l.GetString("record_id") == id {
					found = true
				}
			}
			if !found {
				t.Error("no audit_logs row for the arm action")
			}
		},
	}
	scenario.Test(t)
}

// arm-clear resets the override to empty.
func TestArmClearResetsOverride(t *testing.T) {
	app := newApp(t)
	commander := seedUser(t, app, "armclear@cmd.dev", "command")
	tok, err := commander.NewAuthToken()
	if err != nil {
		t.Fatalf("auth token: %v", err)
	}
	id := seedArea(t, app)
	// Pre-arm via the record so we can observe the clear.
	rec, _ := app.FindRecordById("areas", id)
	rec.Set("arm_override", "armed")
	if err := app.Save(rec); err != nil {
		t.Fatalf("pre-arm: %v", err)
	}

	scenario := tests.ApiScenario{
		Name:                  "command operator clears an area override",
		Method:                http.MethodPost,
		URL:                   "/api/areas/" + id + "/arm-clear",
		Headers:               map[string]string{"Authorization": tok},
		ExpectedStatus:        http.StatusOK,
		ExpectedContent:       []string{`"ok":true`},
		TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		DisableTestAppCleanup: true,
		BeforeTestFunc:        func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) { registerBridge(e) },
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			rec, _ := app.FindRecordById("areas", id)
			if rec.GetString("arm_override") != "" {
				t.Errorf("arm_override = %q, want empty after clear", rec.GetString("arm_override"))
			}
		},
	}
	scenario.Test(t)
}

// An operator without `command` cannot arm (rejected at the gate, before the save).
func TestArmBlockedWithoutCommand(t *testing.T) {
	app := newApp(t)
	topo := seedUser(t, app, "topo@cmd.dev", "topology")
	tok, err := topo.NewAuthToken()
	if err != nil {
		t.Fatalf("auth token: %v", err)
	}
	id := seedArea(t, app)

	scenario := tests.ApiScenario{
		Name:                  "topology-only operator cannot arm",
		Method:                http.MethodPost,
		URL:                   "/api/areas/" + id + "/arm",
		Headers:               map[string]string{"Authorization": tok},
		ExpectedStatus:        http.StatusForbidden,
		ExpectedContent:       []string{"Insufficient permissions"},
		TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		DisableTestAppCleanup: true,
		BeforeTestFunc:        func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) { registerBridge(e) },
	}
	scenario.Test(t)
}
