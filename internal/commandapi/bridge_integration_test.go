package commandapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/stone-age-io/access-control/internal/commandapi"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/subjects"

	// Side-effect import applies the schema + fixture (incl. users) when the
	// test app runs migrations.
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

// seedUser creates an operator with the given capabilities (programmatic save
// bypasses access rules).
func seedUser(t *testing.T, app core.App, email string, perms ...string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("users collection: %v", err)
	}
	u := core.NewRecord(col)
	u.SetEmail(email)
	u.SetPassword("password123")
	u.SetVerified(true)
	u.Set("permissions", perms)
	if err := app.Save(u); err != nil {
		t.Fatalf("seed user %q: %v", email, err)
	}
	return u
}

// registerBridge wires the command routes onto the test serve event. A nil NATS
// conn is fine: the capability gate runs before any publish, and the positive
// case 404s at portal lookup (also before publish).
func registerBridge(e *core.ServeEvent) {
	commandapi.Register(e, nil, subjects.New("acc"), logger.NewNopLogger())
}

// An operator without the `command` capability is rejected at the gate (403),
// before any portal lookup or NATS publish.
func TestPostureBlockedWithoutCommand(t *testing.T) {
	app := newApp(t)
	enroller := seedUser(t, app, "enroll@cmd.dev", "enroll")
	tok, err := enroller.NewAuthToken()
	if err != nil {
		t.Fatalf("auth token: %v", err)
	}

	scenario := tests.ApiScenario{
		Name:                  "enroll-only operator cannot post a posture command",
		Method:                http.MethodPost,
		URL:                   "/api/portals/anything/posture",
		Body:                  strings.NewReader(`{"posture":"lockdown"}`),
		Headers:               map[string]string{"Authorization": tok},
		ExpectedStatus:        http.StatusForbidden,
		ExpectedContent:       []string{"Insufficient permissions"},
		TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		DisableTestAppCleanup: true,
		BeforeTestFunc:        func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) { registerBridge(e) },
	}
	scenario.Test(t)
}

// An operator holding `command` passes the gate; the bogus portal id then 404s
// (before any NATS publish), which still proves the capability check succeeded
// rather than being rejected by it.
func TestPostureAllowedWithCommand(t *testing.T) {
	app := newApp(t)
	commander := seedUser(t, app, "cmd@cmd.dev", "command")
	tok, err := commander.NewAuthToken()
	if err != nil {
		t.Fatalf("auth token: %v", err)
	}

	scenario := tests.ApiScenario{
		Name:                  "command operator passes the gate (404 on bogus portal)",
		Method:                http.MethodPost,
		URL:                   "/api/portals/nonexistent/posture",
		Body:                  strings.NewReader(`{"posture":"lockdown"}`),
		Headers:               map[string]string{"Authorization": tok},
		ExpectedStatus:        http.StatusNotFound,
		ExpectedContent:       []string{"Portal not found"},
		TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		DisableTestAppCleanup: true,
		BeforeTestFunc:        func(t testing.TB, app *tests.TestApp, e *core.ServeEvent) { registerBridge(e) },
	}
	scenario.Test(t)
}
