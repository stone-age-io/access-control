package commandapi_test

import (
	"net/http"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// seedAlarmEvent creates an unacknowledged alarm row and returns its id.
func seedAlarmEvent(t *testing.T, app core.App) string {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("events")
	if err != nil {
		t.Fatalf("events collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("location", "hq")
	rec.Set("portal", "lobby-main")
	rec.Set("type", "door")
	rec.Set("kind", "alarm")
	rec.Set("acknowledged", false)
	if err := app.Save(rec); err != nil {
		t.Fatalf("seed event: %v", err)
	}
	return rec.Id
}

// An operator holding `command` can acknowledge an alarm; the row flips to
// acknowledged with the actor stamped, and an audit_logs row is written.
func TestAckSetsFieldsAndAudits(t *testing.T) {
	app := newApp(t)
	commander := seedUser(t, app, "ack@cmd.dev", "command")
	tok, err := commander.NewAuthToken()
	if err != nil {
		t.Fatalf("auth token: %v", err)
	}
	id := seedAlarmEvent(t, app)

	scenario := tests.ApiScenario{
		Name:                  "command operator acknowledges an alarm",
		Method:                http.MethodPost,
		URL:                   "/api/events/" + id + "/ack",
		Headers:               map[string]string{"Authorization": tok},
		ExpectedStatus:        http.StatusOK,
		ExpectedContent:       []string{`"ok":true`},
		TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		DisableTestAppCleanup: true,
		BeforeTestFunc:        func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) { registerBridge(e) },
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			rec, err := app.FindRecordById("events", id)
			if err != nil {
				t.Fatalf("reload event: %v", err)
			}
			if !rec.GetBool("acknowledged") {
				t.Error("event not marked acknowledged")
			}
			if rec.GetString("ack_by") != "ack@cmd.dev" {
				t.Errorf("ack_by = %q, want ack@cmd.dev", rec.GetString("ack_by"))
			}
			logs, err := app.FindAllRecords("audit_logs")
			if err != nil {
				t.Fatalf("audit_logs: %v", err)
			}
			found := false
			for _, l := range logs {
				if l.GetString("collection_name") == "events" && l.GetString("record_id") == id {
					found = true
				}
			}
			if !found {
				t.Error("no audit_logs row for the ack")
			}
		},
	}
	scenario.Test(t)
}

// An operator without `command` is rejected at the gate (403), before the save.
func TestAckBlockedWithoutCommand(t *testing.T) {
	app := newApp(t)
	enroller := seedUser(t, app, "enroll-ack@cmd.dev", "enroll")
	tok, err := enroller.NewAuthToken()
	if err != nil {
		t.Fatalf("auth token: %v", err)
	}
	id := seedAlarmEvent(t, app)

	scenario := tests.ApiScenario{
		Name:                  "enroll-only operator cannot acknowledge",
		Method:                http.MethodPost,
		URL:                   "/api/events/" + id + "/ack",
		Headers:               map[string]string{"Authorization": tok},
		ExpectedStatus:        http.StatusForbidden,
		ExpectedContent:       []string{"Insufficient permissions"},
		TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		DisableTestAppCleanup: true,
		BeforeTestFunc:        func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) { registerBridge(e) },
	}
	scenario.Test(t)
}
