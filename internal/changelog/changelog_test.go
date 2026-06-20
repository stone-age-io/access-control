package changelog

import (
	"net/http"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/stone-age-io/access-control/internal/logger"

	// Side-effect import applies the schema + fixture (incl. users, audit_logs,
	// and the seeded credential CARD-001) when the test app runs migrations.
	_ "github.com/stone-age-io/access-control/pbmigrations"
)

// newApp returns a migrated test app with the changelog hooks bound. Pruning is
// disabled (retentionDays <= 0) so no cron runs during the test.
func newApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(app.Cleanup)
	Register(app, -1, logger.NewNopLogger())
	return app
}

// seedUser creates an operator account directly (programmatic save bypasses both
// the access rules and the *Request hooks, so it doesn't itself get audited).
func seedUser(t *testing.T, app core.App, email, role string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("users collection: %v", err)
	}
	u := core.NewRecord(col)
	u.SetEmail(email)
	u.SetPassword("password123")
	u.SetVerified(true)
	u.Set("role", role)
	if err := app.Save(u); err != nil {
		t.Fatalf("seed user %q: %v", email, err)
	}
	return u
}

func auditCount(t testing.TB, app core.App) int {
	t.Helper()
	rows, err := app.FindAllRecords("audit_logs")
	if err != nil {
		t.Fatalf("read audit_logs: %v", err)
	}
	return len(rows)
}

// A programmatic app.Save() — the shape of a controller heartbeat, the
// events/point_status projections, and the KV mirror — must NOT be audited. This
// is the core of why the *Request hooks were chosen over commit-success hooks.
func TestMachineWriteNotAudited(t *testing.T) {
	app := newApp(t)

	ctrl, err := app.FindFirstRecordByData("controllers", "code", "ctrl-hq-1")
	if err != nil {
		t.Fatalf("find controller: %v", err)
	}
	ctrl.Set("status", "online")
	if err := app.Save(ctrl); err != nil {
		t.Fatalf("save controller: %v", err)
	}

	if n := auditCount(t, app); n != 0 {
		t.Errorf("audit rows after programmatic save = %d, want 0", n)
	}
}

// An API-driven update to a control-plane collection writes exactly one audit row
// attributed to the operator, with before/after snapshots.
func TestApiUpdateAudited(t *testing.T) {
	app := newApp(t)
	operator := seedUser(t, app, "op@example.com", "operator")
	token, err := operator.NewAuthToken()
	if err != nil {
		t.Fatalf("auth token: %v", err)
	}

	cred, err := app.FindFirstRecordByData("credentials", "value", "CARD-001")
	if err != nil {
		t.Fatalf("find credential: %v", err)
	}

	scenario := tests.ApiScenario{
		Name:                  "operator revokes a credential",
		Method:                http.MethodPatch,
		URL:                   "/api/collections/credentials/records/" + cred.Id,
		Body:                  strings.NewReader(`{"status":"revoked"}`),
		Headers:               map[string]string{"Authorization": token},
		ExpectedStatus:        http.StatusOK,
		ExpectedContent:       []string{`"status":"revoked"`},
		TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		DisableTestAppCleanup: true,
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			rows, err := app.FindAllRecords("audit_logs")
			if err != nil {
				t.Fatalf("read audit_logs: %v", err)
			}
			if len(rows) != 1 {
				t.Fatalf("audit rows = %d, want 1", len(rows))
			}
			row := rows[0]
			if got := row.GetString("event_type"); got != "update" {
				t.Errorf("event_type = %q, want update", got)
			}
			if got := row.GetString("collection_name"); got != "credentials" {
				t.Errorf("collection_name = %q, want credentials", got)
			}
			if got := row.GetString("record_id"); got != cred.Id {
				t.Errorf("record_id = %q, want %q", got, cred.Id)
			}
			if got := row.GetString("actor_email"); got != "op@example.com" {
				t.Errorf("actor_email = %q, want op@example.com", got)
			}
			if got := row.GetString("actor_collection"); got != "users" {
				t.Errorf("actor_collection = %q, want users", got)
			}
			before := row.GetString("before")
			after := row.GetString("after")
			if !strings.Contains(before, "active") {
				t.Errorf("before = %q, want it to contain the prior status 'active'", before)
			}
			if !strings.Contains(after, "revoked") {
				t.Errorf("after = %q, want it to contain 'revoked'", after)
			}
		},
	}
	scenario.Test(t)
}

// A non-admin operator may edit its own record (password/name) but must not be
// able to escalate its own role — the guard hook rejects the change with a 403,
// and nothing is written or audited.
func TestRoleEscalationBlocked(t *testing.T) {
	app := newApp(t)
	operator := seedUser(t, app, "op2@example.com", "operator")
	token, err := operator.NewAuthToken()
	if err != nil {
		t.Fatalf("auth token: %v", err)
	}

	scenario := tests.ApiScenario{
		Name:                  "operator cannot self-promote to admin",
		Method:                http.MethodPatch,
		URL:                   "/api/collections/users/records/" + operator.Id,
		Body:                  strings.NewReader(`{"role":"admin"}`),
		Headers:               map[string]string{"Authorization": token},
		ExpectedStatus:        http.StatusForbidden,
		ExpectedContent:       []string{"admin"},
		TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		DisableTestAppCleanup: true,
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			reloaded, err := app.FindRecordById("users", operator.Id)
			if err != nil {
				t.Fatalf("reload operator: %v", err)
			}
			if got := reloaded.GetString("role"); got != "operator" {
				t.Errorf("role after blocked escalation = %q, want operator", got)
			}
			if n := auditCount(t, app); n != 0 {
				t.Errorf("audit rows after rejected update = %d, want 0", n)
			}
		},
	}
	scenario.Test(t)
}

func TestSnapshotStripsSecrets(t *testing.T) {
	app := newApp(t)
	u := seedUser(t, app, "snap@example.com", "viewer")

	snap := snapshot(u)
	if _, ok := snap["password"]; ok {
		t.Error("snapshot leaked password")
	}
	if _, ok := snap["tokenKey"]; ok {
		t.Error("snapshot leaked tokenKey")
	}
	if snap["email"] != "snap@example.com" {
		t.Errorf("snapshot email = %v, want snap@example.com", snap["email"])
	}
	if snap["role"] != "viewer" {
		t.Errorf("snapshot role = %v, want viewer", snap["role"])
	}
}
