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
	operator := seedUser(t, app, "op@example.com", "enroll") // enroll → may write credentials
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

// An API-driven edit to an AREA is audited. Areas were added after the original
// allowlist (migration 1750000019) and were missed, so an operator could change an
// area's standing arm-state or its auto_schedule — both of which change intrusion
// behaviour — with no audit row.
//
// Note this is distinct from the arm/disarm command routes
// (POST /api/areas/{id}/arm|disarm|arm-clear), which were always audited: those are
// custom routes writing their own row via commandapi.writeAudit, precisely because a
// custom-route app.Save never trips the *Request hooks. This covers the ordinary CRUD
// path the UI's area form uses.
func TestAreaApiUpdateAudited(t *testing.T) {
	app := newApp(t)
	// areas writes require `topology` (migration 1750000019).
	operator := seedUser(t, app, "topo@example.com", "topology")
	token, err := operator.NewAuthToken()
	if err != nil {
		t.Fatalf("auth token: %v", err)
	}

	area, err := app.FindFirstRecordByData("areas", "code", "warehouse")
	if err != nil {
		t.Fatalf("find fixture area: %v", err)
	}

	scenario := tests.ApiScenario{
		Name:                  "operator arms an area by editing the standing arm value",
		Method:                http.MethodPatch,
		URL:                   "/api/collections/areas/records/" + area.Id,
		Body:                  strings.NewReader(`{"arm":"armed"}`),
		Headers:               map[string]string{"Authorization": token},
		ExpectedStatus:        http.StatusOK,
		ExpectedContent:       []string{`"arm":"armed"`},
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
			if got := row.GetString("collection_name"); got != "areas" {
				t.Errorf("collection_name = %q, want areas", got)
			}
			if got := row.GetString("record_id"); got != area.Id {
				t.Errorf("record_id = %q, want %q", got, area.Id)
			}
			if got := row.GetString("actor_email"); got != "topo@example.com" {
				t.Errorf("actor_email = %q, want topo@example.com", got)
			}
			// The arm-state transition must be legible from the row itself, or the
			// log cannot answer "who armed the warehouse".
			if before := row.GetString("before"); !strings.Contains(before, "disarmed") {
				t.Errorf("before = %q, want it to contain the prior arm 'disarmed'", before)
			}
			if after := row.GetString("after"); !strings.Contains(after, "armed") {
				t.Errorf("after = %q, want it to contain 'armed'", after)
			}
		},
	}
	scenario.Test(t)
}

// TestAuditedCoversControlPlane is the regression guard for the gap that let `areas`
// and `holiday_calendars` go unaudited: both were added to the schema by later
// migrations and nobody remembered this allowlist. It fails on the NEXT such
// omission rather than waiting for someone to notice a missing audit trail.
//
// A collection counts as control-plane — and therefore must be audited — when it has
// a WRITE rule gated on an operator capability (`@request.auth.permissions`). That is
// the established pattern for every operator-editable collection, so the check
// maintains itself: a new one written the normal way is picked up automatically.
//
// Two categories fall out of that definition rather than needing an exemption list,
// which is why there isn't one:
//
//   - The machine-written projections (`events`, `point_status`) and `audit_logs`
//     have NIL write rules — superuser-only, written by app.Save, never via the API.
//   - PocketBase's own collections (`_superusers`, `_otps`, …) and the demo fixtures
//     tests.NewTestApp seeds (`demo1`, `clients`, `view1`, …) carry no capability
//     rules at all.
func TestAuditedCoversControlPlane(t *testing.T) {
	app := newApp(t)

	inAudited := make(map[string]bool, len(audited))
	for _, name := range audited {
		inAudited[name] = true
	}

	cols, err := app.FindAllCollections()
	if err != nil {
		t.Fatalf("FindAllCollections: %v", err)
	}

	// capabilityGated reports whether any write rule references operator
	// capabilities, i.e. whether an operator can edit this collection via the API.
	capabilityGated := func(c *core.Collection) bool {
		for _, rule := range []*string{c.CreateRule, c.UpdateRule, c.DeleteRule} {
			if rule != nil && strings.Contains(*rule, "@request.auth.permissions") {
				return true
			}
		}
		return false
	}

	var checked int
	existing := make(map[string]bool, len(cols))
	for _, c := range cols {
		existing[c.Name] = true
		if c.System || !capabilityGated(c) {
			continue
		}
		checked++
		if !inAudited[c.Name] {
			t.Errorf("collection %q has capability-gated writes but is not in `audited` — "+
				"an operator can edit it through the API with no audit_logs row", c.Name)
		}
	}
	// Guard the guard: if the discriminator ever stops matching (a rule-syntax change,
	// say) this test would silently pass while checking nothing.
	if checked < 10 {
		t.Errorf("only %d capability-gated collections found; the discriminator looks broken", checked)
	}

	// The reverse: an entry in `audited` naming a collection that does not exist would
	// silently bind a hook to nothing.
	for _, name := range audited {
		if !existing[name] {
			t.Errorf("`audited` lists %q, which is not a collection in the schema", name)
		}
	}
}

// A non-admin operator may edit its own record (password/name) but must not be
// able to escalate its own permissions — the guard hook rejects the change with a
// 403 (even though the users updateRule lets it edit itself), and nothing is
// written or audited.
func TestPermissionEscalationBlocked(t *testing.T) {
	app := newApp(t)
	operator := seedUser(t, app, "op2@example.com", "enroll")
	token, err := operator.NewAuthToken()
	if err != nil {
		t.Fatalf("auth token: %v", err)
	}

	scenario := tests.ApiScenario{
		Name:                  "operator cannot self-grant the operators capability",
		Method:                http.MethodPatch,
		URL:                   "/api/collections/users/records/" + operator.Id,
		Body:                  strings.NewReader(`{"permissions":["enroll","operators"]}`),
		Headers:               map[string]string{"Authorization": token},
		ExpectedStatus:        http.StatusForbidden,
		ExpectedContent:       []string{"operators"},
		TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		DisableTestAppCleanup: true,
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			reloaded, err := app.FindRecordById("users", operator.Id)
			if err != nil {
				t.Fatalf("reload operator: %v", err)
			}
			if got := reloaded.GetStringSlice("permissions"); len(got) != 1 || got[0] != "enroll" {
				t.Errorf("permissions after blocked escalation = %v, want [enroll]", got)
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
	u := seedUser(t, app, "snap@example.com", "enroll")

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
	if _, ok := snap["permissions"]; !ok {
		t.Error("snapshot missing permissions field")
	}
}
