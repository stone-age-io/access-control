package pbmigrations_test

import (
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	// Side-effect import registers the schema + fixture migrations so the test
	// app applies them in RunAllMigrations.
	_ "github.com/stone-age-io/access-control/pbmigrations"
)

// newApp spins up a throwaway PocketBase that has run all migrations (system +
// ours). The clone is cleaned up by t.Cleanup.
func newApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(app.Cleanup)
	return app
}

func TestCollectionsExist(t *testing.T) {
	app := newApp(t)

	for _, name := range []string{
		"locations", "schedules", "portals", "access_groups",
		"roles", "cardholders", "credentials", "events", "holidays",
		"audit_logs",
	} {
		if _, err := app.FindCollectionByNameOrId(name); err != nil {
			t.Errorf("collection %q not found: %v", name, err)
		}
	}
}

// TestOperatorAuthTier checks the auth tier after migration 1750000016: the
// users.permissions multi-select (replacing role), the locked-down users rules,
// and the capability-based collection rule matrix.
func TestOperatorAuthTier(t *testing.T) {
	app := newApp(t)

	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("users collection: %v", err)
	}
	// role is gone; permissions is the single source of truth.
	if users.Fields.GetByName("role") != nil {
		t.Error("users.role field should be removed (replaced by permissions)")
	}
	perms, ok := users.Fields.GetByName("permissions").(*core.SelectField)
	if !ok || perms == nil {
		t.Fatal("users.permissions multi-select field missing")
	}
	if perms.MaxSelect <= 1 {
		t.Errorf("users.permissions MaxSelect = %d, want >1 (multi-select)", perms.MaxSelect)
	}
	for _, want := range []string{"enroll", "policy", "topology", "command", "operators"} {
		if !slicesContains(perms.Values, want) {
			t.Errorf("users.permissions missing value %q (have %v)", want, perms.Values)
		}
	}
	// Default open-signup ("") must be locked to the operators capability.
	if users.CreateRule == nil || *users.CreateRule == "" {
		t.Errorf("users.CreateRule = %v, want operators-only (not open signup)", users.CreateRule)
	}

	// rule asserts a collection's named rule is non-nil and contains substr.
	rule := func(name, which string, get func(*core.Collection) *string, substr string) {
		c, err := app.FindCollectionByNameOrId(name)
		if err != nil {
			t.Fatalf("%s collection: %v", name, err)
		}
		r := get(c)
		if r == nil || !strings.Contains(*r, substr) {
			t.Errorf("%s.%s = %v, want it to contain %q", name, which, r, substr)
		}
	}

	// People: credentials writable with the enroll capability.
	rule("credentials", "UpdateRule", func(c *core.Collection) *string { return c.UpdateRule }, `"enroll"`)
	// Topology: controllers writable with the topology capability.
	rule("controllers", "UpdateRule", func(c *core.Collection) *string { return c.UpdateRule }, `"topology"`)
	// Access logic: schedules writable with the policy capability.
	rule("schedules", "UpdateRule", func(c *core.Collection) *string { return c.UpdateRule }, `"policy"`)
	// All operators can read the policy graph.
	rule("portals", "ListRule", func(c *core.Collection) *string { return c.ListRule }, `@request.auth.id`)
	// audit_logs readable with the operators capability.
	rule("audit_logs", "ListRule", func(c *core.Collection) *string { return c.ListRule }, `"operators"`)

	// Machine projections: nobody writes via the API (superuser/system only).
	events, err := app.FindCollectionByNameOrId("events")
	if err != nil {
		t.Fatalf("events collection: %v", err)
	}
	if events.CreateRule != nil {
		t.Errorf("events.CreateRule = %v, want nil (superuser-only)", *events.CreateRule)
	}
}

func slicesContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// TestPermissionRuleEnforcement is the security-boundary spike: it confirms that
// `@request.auth.permissions ?= "x"` actually admits a user holding capability x
// and rejects one who doesn't, using PocketBase's own rule evaluator
// (CanAccessRecord). The write rules reference only @request.auth, so evaluating
// them against any existing record (the fixture's alice cardholder) exercises the
// multi-select membership semantics directly. If this fails, the `?=` operator in
// 1750000016 must be swapped for `~` (JSON-LIKE; safe given substring-free names).
func TestPermissionRuleEnforcement(t *testing.T) {
	app := newApp(t)

	usersCol, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("users collection: %v", err)
	}
	mk := func(email string, perms []string) *core.Record {
		u := core.NewRecord(usersCol)
		u.SetEmail(email)
		u.SetPassword("password123")
		u.SetVerified(true)
		u.Set("permissions", perms)
		if err := app.Save(u); err != nil {
			t.Fatalf("save user %s: %v", email, err)
		}
		return u
	}
	enrollUser := mk("enroll@test.dev", []string{"enroll"})
	topoUser := mk("topo@test.dev", []string{"topology"})
	multiUser := mk("multi@test.dev", []string{"enroll", "command"})
	emptyUser := mk("viewer@test.dev", []string{})

	// Any persisted record works since the write rules ignore record fields.
	alice, err := app.FindFirstRecordByData("cardholders", "external_id", "alice")
	if err != nil {
		t.Fatalf("fixture cardholder alice not found: %v", err)
	}
	cardholders, _ := app.FindCollectionByNameOrId("cardholders")
	controllers, _ := app.FindCollectionByNameOrId("controllers")

	check := func(label string, rule *string, auth *core.Record, want bool) {
		ok, err := app.CanAccessRecord(alice, &core.RequestInfo{Auth: auth, Method: "POST"}, rule)
		if err != nil {
			t.Fatalf("%s: CanAccessRecord error: %v", label, err)
		}
		if ok != want {
			t.Errorf("%s: access = %v, want %v (rule %q, perms %v)", label, ok, want, deref(rule), auth.GetStringSlice("permissions"))
		}
	}

	// cardholders write rule requires `enroll`.
	check("enroll→cardholders", cardholders.CreateRule, enrollUser, true)
	check("topology→cardholders", cardholders.CreateRule, topoUser, false)
	check("enroll+command→cardholders", cardholders.CreateRule, multiUser, true)
	check("viewer→cardholders", cardholders.CreateRule, emptyUser, false)

	// controllers write rule requires `topology`.
	check("enroll→controllers", controllers.CreateRule, enrollUser, false)
	check("topology→controllers", controllers.CreateRule, topoUser, true)
}

func deref(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

func TestFixtureSeeded(t *testing.T) {
	app := newApp(t)

	// location hq carries the timezone.
	location, err := app.FindFirstRecordByData("locations", "code", "hq")
	if err != nil {
		t.Fatalf("location hq not found: %v", err)
	}
	if got := location.GetString("timezone"); got != "America/New_York" {
		t.Errorf("location timezone = %q, want America/New_York", got)
	}

	// credential CARD-001 resolves to cardholder alice (active).
	cred, err := app.FindFirstRecordByData("credentials", "value", "CARD-001")
	if err != nil {
		t.Fatalf("credential CARD-001 not found: %v", err)
	}
	holder, err := app.FindRecordById("cardholders", cred.GetString("user"))
	if err != nil {
		t.Fatalf("cardholder for CARD-001 not found: %v", err)
	}
	if got := holder.GetString("external_id"); got != "alice" {
		t.Errorf("cardholder external_id = %q, want alice", got)
	}
	if got := holder.GetString("status"); got != "active" {
		t.Errorf("cardholder status = %q, want active", got)
	}

	// access group lobby-group binds schedule business-hours and contains lobby-main.
	group, err := app.FindFirstRecordByData("access_groups", "code", "lobby-group")
	if err != nil {
		t.Fatalf("access group lobby-group not found: %v", err)
	}
	sched, err := app.FindRecordById("schedules", group.GetString("schedule"))
	if err != nil {
		t.Fatalf("schedule for lobby-group not found: %v", err)
	}
	if got := sched.GetString("code"); got != "business-hours" {
		t.Errorf("lobby-group schedule = %q, want business-hours", got)
	}

	portal, err := app.FindFirstRecordByData("portals", "code", "lobby-main")
	if err != nil {
		t.Fatalf("portal lobby-main not found: %v", err)
	}
	portalIDs := group.GetStringSlice("portals")
	if len(portalIDs) != 1 || portalIDs[0] != portal.Id {
		t.Errorf("lobby-group portals = %v, want [%s]", portalIDs, portal.Id)
	}
	if got := portal.GetString("posture"); got != "secure" {
		t.Errorf("lobby-main posture = %q, want secure", got)
	}
	if got := portal.GetString("type"); got != "door" {
		t.Errorf("lobby-main type = %q, want door", got)
	}
}

// TestFixtureExtras verifies the post-schema demonstration data: a recurring
// Christmas holiday at hq and the lobby-public auto-unlock door.
func TestFixtureExtras(t *testing.T) {
	app := newApp(t)

	holiday, err := app.FindFirstRecordByData("holidays", "name", "Christmas")
	if err != nil {
		t.Fatalf("holiday Christmas not found: %v", err)
	}
	if !holiday.GetBool("recurring") {
		t.Errorf("Christmas holiday recurring = false, want true")
	}

	pub, err := app.FindFirstRecordByData("portals", "code", "lobby-public")
	if err != nil {
		t.Fatalf("portal lobby-public not found: %v", err)
	}
	if got := pub.GetString("auto_posture"); got != "unlocked" {
		t.Errorf("lobby-public auto_posture = %q, want unlocked", got)
	}
	sched, err := app.FindRecordById("schedules", pub.GetString("auto_schedule"))
	if err != nil || sched.GetString("code") != "business-hours" {
		t.Errorf("lobby-public auto_schedule = %v, want business-hours", pub.GetString("auto_schedule"))
	}
}

// TestFixtureSingleLocation re-runs the fixture migration's seeding guard logic:
// the migration no-ops when locations already exist, so a second
// RunAllMigrations still yields exactly one hq.
func TestFixtureSingleLocation(t *testing.T) {
	app := newApp(t)
	locations, err := app.FindAllRecords("locations")
	if err != nil {
		t.Fatalf("FindAllRecords locations: %v", err)
	}
	if len(locations) != 1 {
		t.Errorf("locations count = %d, want 1", len(locations))
	}
}
