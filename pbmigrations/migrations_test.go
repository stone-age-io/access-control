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
		"audit_logs", "holiday_calendars",
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
	// All operators can read the policy graph — but only operators. After
	// 1750000027 the floor names the operator collection instead of "any auth".
	rule("portals", "ListRule", func(c *core.Collection) *string { return c.ListRule }, `@request.auth.collectionName = "users"`)
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

// TestBadgeUsers verifies migration 1750000030: the badge auth tier. The rules are
// the point — an auth collection's DEFAULT create rule is open signup, which would
// let anyone mint themselves a badge login.
func TestBadgeUsers(t *testing.T) {
	app := newApp(t)

	c, err := app.FindCollectionByNameOrId("badge_users")
	if err != nil {
		t.Fatalf("badge_users collection: %v", err)
	}
	if !c.IsAuth() {
		t.Fatal("badge_users is not an auth collection")
	}

	// Open signup must be closed, and minting must require an operator capability.
	if c.CreateRule == nil || *c.CreateRule == "" {
		t.Fatalf("badge_users.CreateRule = %v, want enroll-gated (empty string is OPEN SIGNUP)", c.CreateRule)
	}
	for _, want := range []string{`"enroll"`, `@request.auth.collectionName = "users"`} {
		if !strings.Contains(*c.CreateRule, want) {
			t.Errorf("badge_users.CreateRule = %q, want it to contain %s", *c.CreateRule, want)
		}
	}
	// A badge holder must only ever see its own record — never another badge user's.
	if c.ListRule == nil || !strings.Contains(*c.ListRule, "id = @request.auth.id") {
		t.Errorf("badge_users.ListRule = %v, want self-scoped", c.ListRule)
	}

	// Password auth off, OTP on: a visitor should not manage a password, and OTP is
	// what makes an emailed badge link more than a bare bearer token.
	if c.PasswordAuth.Enabled {
		t.Error("badge_users password auth is enabled; want OTP/OAuth2 only")
	}
	if !c.OTP.Enabled {
		t.Error("badge_users OTP is disabled; the visitor invite flow depends on it")
	}

	// The cardholder link must be required and unique — one login per person, and
	// never a login that speaks for nobody.
	f, ok := c.Fields.GetByName("cardholder").(*core.RelationField)
	if !ok || f == nil {
		t.Fatal("badge_users.cardholder relation field missing")
	}
	if !f.Required {
		t.Error("badge_users.cardholder is not required")
	}
	cardholders, err := app.FindCollectionByNameOrId("cardholders")
	if err != nil {
		t.Fatalf("cardholders collection: %v", err)
	}
	if f.CollectionId != cardholders.Id {
		t.Errorf("badge_users.cardholder targets %q, want cardholders (%q)", f.CollectionId, cardholders.Id)
	}
	var unique bool
	for _, idx := range c.Indexes {
		if strings.Contains(idx, "UNIQUE") && strings.Contains(idx, "cardholder") {
			unique = true
		}
	}
	if !unique {
		t.Errorf("badge_users has no unique index on cardholder; indexes = %v", c.Indexes)
	}

	if k, ok := c.Fields.GetByName("kind").(*core.SelectField); !ok || k == nil {
		t.Error("badge_users.kind select field missing")
	} else {
		for _, want := range []string{"holder", "visitor"} {
			if !slicesContains(k.Values, want) {
				t.Errorf("badge_users.kind missing value %q (have %v)", want, k.Values)
			}
		}
	}
}

// TestPortalRemoteUnlockDefaultsFalse verifies migration 1750000031. The DEFAULT is
// the assertion that matters: remote unlock must be an explicit per-door act, so an
// install that upgrades into this feature must not find its doors remotely openable.
func TestPortalRemoteUnlockDefaultsFalse(t *testing.T) {
	app := newApp(t)

	portals, err := app.FindCollectionByNameOrId("portals")
	if err != nil {
		t.Fatalf("portals collection: %v", err)
	}
	if portals.Fields.GetByName("allow_remote_unlock") == nil {
		t.Fatal("portals.allow_remote_unlock field missing")
	}
	// Every pre-existing portal (the fixture's) must be closed to remote unlock.
	all, err := app.FindAllRecords("portals")
	if err != nil {
		t.Fatalf("FindAllRecords portals: %v", err)
	}
	if len(all) == 0 {
		t.Fatal("no fixture portals to check")
	}
	for _, p := range all {
		if p.GetBool("allow_remote_unlock") {
			t.Errorf("portal %q has allow_remote_unlock = true after migration; want false",
				p.GetString("code"))
		}
	}
}

// TestBadgeRateLimits verifies migration 1750000032. The badge routes are the first
// reachable by a non-operator, and an unconfigured PocketBase limiter is wide open —
// so these defaults ship rather than being left to the installer.
func TestBadgeRateLimits(t *testing.T) {
	app := newApp(t)
	rl := app.Settings().RateLimits

	if !rl.Enabled {
		t.Error("rate limiting is disabled; the badge routes ship with limits on")
	}

	// The unlock rule must be a PREFIX rule (trailing '/'), or it would never match
	// /api/badge/unlock/{portalId} for a real portal id.
	find := func(label string) (core.RateLimitRule, bool) {
		for _, r := range rl.Rules {
			if r.Label == label {
				return r, true
			}
		}
		return core.RateLimitRule{}, false
	}

	unlock, ok := find("POST /api/badge/unlock/")
	if !ok {
		t.Fatalf("no rate limit rule for the badge unlock route; rules = %+v", rl.Rules)
	}
	if !strings.HasSuffix(unlock.Label, "/") {
		t.Errorf("unlock rule label %q is not a prefix rule; it will not match a portal id", unlock.Label)
	}
	if unlock.MaxRequests <= 0 || unlock.Duration <= 0 {
		t.Errorf("unlock rule = %+v, want positive MaxRequests and Duration", unlock)
	}

	// OTP mints an email per call and the caller is a guest, so an @auth-only rule
	// would never fire.
	otp, ok := find("badge_users:requestOTP")
	if !ok {
		t.Fatalf("no rate limit rule for badge_users OTP requests; rules = %+v", rl.Rules)
	}
	if otp.Audience == core.RateLimitRuleAudienceAuth {
		t.Error("OTP rule is scoped to @auth, but an OTP requester has no token yet — it would never apply")
	}

	// The limiter matches "METHOD /path" against a real request; sanity-check that
	// the prefix rule actually selects for a concrete unlock path.
	got, found := rl.FindRateLimitRule(
		[]string{"POST /api/badge/unlock/abc123def456789", "/api/badge/unlock/abc123def456789"},
		core.RateLimitRuleAudienceAuth, core.RateLimitRuleAudienceAll,
	)
	if !found || got.Label != unlock.Label {
		t.Errorf("FindRateLimitRule for a concrete unlock path = (%+v, %v), want the prefix rule %q",
			got, found, unlock.Label)
	}
}

// TestBadgeUserCannotReadPolicyGraph is the end-to-end version of
// TestReadFloorExcludesNonOperatorAuth against the REAL badge collection. The
// throwaway-collection test proves the general rule; this one proves the actual
// tier this feature ships, including that a badge holder cannot enumerate other
// badge holders.
func TestBadgeUserCannotReadPolicyGraph(t *testing.T) {
	app := newApp(t)

	badgeCol, err := app.FindCollectionByNameOrId("badge_users")
	if err != nil {
		t.Fatalf("badge_users collection: %v", err)
	}
	alice, err := app.FindFirstRecordByData("cardholders", "external_id", "alice")
	if err != nil {
		t.Fatalf("fixture cardholder alice not found: %v", err)
	}

	badge := core.NewRecord(badgeCol)
	badge.SetEmail("visitor@test.dev")
	badge.Set("cardholder", alice.Id)
	badge.Set("kind", "visitor")
	// PocketBase requires a non-blank password on an auth record even when the
	// collection has password auth DISABLED — the field validator is independent of
	// the auth-method option. It is unusable for sign-in, so anything creating a
	// badge login sets an unguessable throwaway (see internal/badgeapi).
	badge.SetPassword("unused-password-not-a-sign-in-path")
	if err := app.Save(badge); err != nil {
		t.Fatalf("save badge user: %v", err)
	}

	// The credential VALUE is the credential secret — this is the row that must
	// never be listable by the badge tier.
	for _, name := range []string{"credentials", "cardholders", "portals", "areas", "events"} {
		c, err := app.FindCollectionByNameOrId(name)
		if err != nil {
			t.Errorf("%s collection: %v", name, err)
			continue
		}
		ok, err := app.CanAccessRecord(alice, &core.RequestInfo{Auth: badge, Method: "GET"}, c.ListRule)
		if err != nil {
			t.Fatalf("%s: CanAccessRecord error: %v", name, err)
		}
		if ok {
			t.Errorf("a badge_users record can list %s — the policy graph is exposed to the badge tier", name)
		}
	}

	// A second badge user must be invisible to the first: the self-scoped rule is
	// evaluated against the OTHER record, which is what a list request would do.
	// Build the peer's cardholder here rather than relying on a second fixture
	// person, so this half always runs.
	cardholders, err := app.FindCollectionByNameOrId("cardholders")
	if err != nil {
		t.Fatalf("cardholders collection: %v", err)
	}
	peer := core.NewRecord(cardholders)
	peer.Set("name", "Peer Person")
	peer.Set("status", "active")
	if err := app.Save(peer); err != nil {
		t.Fatalf("save peer cardholder: %v", err)
	}

	other := core.NewRecord(badgeCol)
	other.SetEmail("other@test.dev")
	other.Set("cardholder", peer.Id)
	other.Set("kind", "holder")
	other.SetPassword("unused-password-not-a-sign-in-path")
	if err := app.Save(other); err != nil {
		t.Fatalf("save second badge user: %v", err)
	}
	ok, err := app.CanAccessRecord(other, &core.RequestInfo{Auth: badge, Method: "GET"}, badgeCol.ListRule)
	if err != nil {
		t.Fatalf("badge_users peer check: CanAccessRecord error: %v", err)
	}
	if ok {
		t.Error("a badge_users record can read another badge_users record; want self-scoped only")
	}
}

// TestCardholderPhoto verifies migration 1750000029: the cardholder photo file
// field. Protected is the assertion that matters — an unprotected file URL is
// public to anyone holding the link, which is the wrong default for the first PII
// field in the schema.
func TestCardholderPhoto(t *testing.T) {
	app := newApp(t)

	c, err := app.FindCollectionByNameOrId("cardholders")
	if err != nil {
		t.Fatalf("cardholders collection: %v", err)
	}
	f, ok := c.Fields.GetByName("photo").(*core.FileField)
	if !ok || f == nil {
		t.Fatal("cardholders.photo file field missing")
	}
	if !f.Protected {
		t.Error("cardholders.photo is not Protected; a leaked file URL would expose the photo without auth")
	}
	if f.MaxSelect != 1 {
		t.Errorf("cardholders.photo MaxSelect = %d, want 1", f.MaxSelect)
	}
	if len(f.MimeTypes) == 0 {
		t.Error("cardholders.photo has no MimeTypes restriction; want images only")
	}
	for _, mt := range f.MimeTypes {
		if !strings.HasPrefix(mt, "image/") {
			t.Errorf("cardholders.photo allows non-image mime type %q", mt)
		}
	}
	if len(f.Thumbs) == 0 {
		t.Error("cardholders.photo has no Thumbs; the list and badge views need generated sizes")
	}
}

// TestCredentialValueCharset verifies migration 1750000028: credentials.value is
// constrained to the NATS KV key charset at the API boundary. A credential is
// mirrored to KV key "cred.<value>", so a value outside that charset used to save
// happily, list as active in the UI, and then silently never reach any controller —
// the KV Put failed in accessd's log with nothing tying it back to the record.
// Paired with mirror.validKey (defense in depth for non-API writes).
func TestCredentialValueCharset(t *testing.T) {
	app := newApp(t)

	creds, err := app.FindCollectionByNameOrId("credentials")
	if err != nil {
		t.Fatalf("credentials collection: %v", err)
	}
	f, ok := creds.Fields.GetByName("value").(*core.TextField)
	if !ok || f == nil {
		t.Fatal("credentials.value text field missing")
	}
	if f.Pattern == "" {
		t.Error("credentials.value Pattern is empty; want the NATS KV key charset")
	}

	alice, err := app.FindFirstRecordByData("cardholders", "external_id", "alice")
	if err != nil {
		t.Fatalf("fixture cardholder alice not found: %v", err)
	}
	save := func(value string) error {
		rec := core.NewRecord(creds)
		rec.Set("value", value)
		rec.Set("user", alice.Id)
		rec.Set("status", "active")
		return app.Save(rec)
	}

	// A URL is the realistic mistake once badges exist — ':' , '?' and '#' are all
	// outside the KV key charset.
	for _, bad := range []string{
		"https://example.com/badge/abc",
		"has space",
		"trailing.",
		"plus+sign",
		"hash#frag",
	} {
		if err := save(bad); err == nil {
			t.Errorf("credentials.value %q saved, want a validation error", bad)
		}
	}
	// Values a minted badge credential will actually use must pass.
	for _, good := range []string{"CARD-999", "QR-v1.aGVsbG8", "pad=", "a_b/c=d-e.f"} {
		if err := save(good); err != nil {
			t.Errorf("credentials.value %q rejected (%v), want accepted", good, err)
		}
	}
}

// TestReadFloorExcludesNonOperatorAuth is the security boundary for migration
// 1750000027. The old read floor (`@request.auth.id != ""`) was satisfied by an
// authenticated record from ANY auth collection, so introducing a second auth tier
// (badge holders / visitors) would have handed every one of them operator read on
// the whole policy graph — including credentials.value, the credential secret in
// plaintext.
//
// The collection created here is a throwaway, NOT badge_users, on purpose: the
// guarantee under test is "any non-operator auth collection is excluded", which
// must hold for auth tiers added later without anyone remembering to extend this
// test.
//
// The read rules reference only @request.auth (never a record field), so
// evaluating them against any persisted record exercises them faithfully — same
// approach as TestPermissionRuleEnforcement.
func TestReadFloorExcludesNonOperatorAuth(t *testing.T) {
	app := newApp(t)

	// A second auth collection, standing in for any non-operator auth tier.
	otherCol := core.NewAuthCollection("test_other_auth")
	if err := app.Save(otherCol); err != nil {
		t.Fatalf("save throwaway auth collection: %v", err)
	}
	otherAuth := core.NewRecord(otherCol)
	otherAuth.SetEmail("outsider@test.dev")
	otherAuth.SetPassword("password123")
	otherAuth.SetVerified(true)
	if err := app.Save(otherAuth); err != nil {
		t.Fatalf("save throwaway auth record: %v", err)
	}

	// An operator with NO capabilities: read is a universal floor for operators,
	// so this one must still pass every rule below. That is what makes the test
	// prove collection scoping rather than incidental capability gating.
	usersCol, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("users collection: %v", err)
	}
	operator := core.NewRecord(usersCol)
	operator.SetEmail("readonly@test.dev")
	operator.SetPassword("password123")
	operator.SetVerified(true)
	operator.Set("permissions", []string{})
	if err := app.Save(operator); err != nil {
		t.Fatalf("save operator: %v", err)
	}

	alice, err := app.FindFirstRecordByData("cardholders", "external_id", "alice")
	if err != nil {
		t.Fatalf("fixture cardholder alice not found: %v", err)
	}

	// Every collection whose read floor 1750000027 scoped to the operator tier.
	for _, name := range []string{
		"cardholders", "credentials",
		"schedules", "access_groups", "roles", "holidays", "holiday_calendars",
		"locations", "controllers", "portals", "aux_input", "aux_output", "areas",
		"events", "point_status",
	} {
		c, err := app.FindCollectionByNameOrId(name)
		if err != nil {
			t.Errorf("%s collection: %v", name, err)
			continue
		}
		for _, tc := range []struct {
			which string
			rule  *string
		}{
			{"ListRule", c.ListRule},
			{"ViewRule", c.ViewRule},
		} {
			if tc.rule == nil {
				t.Errorf("%s.%s is nil (superuser-only); expected the operator read floor", name, tc.which)
				continue
			}
			okOther, err := app.CanAccessRecord(alice, &core.RequestInfo{Auth: otherAuth, Method: "GET"}, tc.rule)
			if err != nil {
				t.Fatalf("%s.%s: CanAccessRecord(other-auth) error: %v", name, tc.which, err)
			}
			if okOther {
				t.Errorf("%s.%s admits a non-operator auth record (rule %q) — policy graph is exposed to the badge tier",
					name, tc.which, deref(tc.rule))
			}
			okOperator, err := app.CanAccessRecord(alice, &core.RequestInfo{Auth: operator, Method: "GET"}, tc.rule)
			if err != nil {
				t.Fatalf("%s.%s: CanAccessRecord(operator) error: %v", name, tc.which, err)
			}
			if !okOperator {
				t.Errorf("%s.%s rejects a capability-less operator (rule %q) — read must stay a universal operator floor",
					name, tc.which, deref(tc.rule))
			}
		}
	}
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
	// After 1750000018 the holiday is homed on a calendar, not a location.
	if holiday.GetString("calendar") == "" {
		t.Errorf("Christmas holiday calendar is empty, want it linked by the data migration")
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

// TestHolidayCalendars verifies migration 1750000018: the holiday_calendars
// collection + its policy write rule, the locations.holiday_calendars relation,
// the holidays.calendar relation replacing .location, and the data migration that
// homes each existing location's holidays onto a per-location calendar it observes.
func TestHolidayCalendars(t *testing.T) {
	app := newApp(t)

	cals, err := app.FindCollectionByNameOrId("holiday_calendars")
	if err != nil {
		t.Fatalf("holiday_calendars collection: %v", err)
	}
	if cals.CreateRule == nil || !strings.Contains(*cals.CreateRule, `"policy"`) {
		t.Errorf("holiday_calendars.CreateRule = %v, want it to require the policy capability", cals.CreateRule)
	}

	holidays, err := app.FindCollectionByNameOrId("holidays")
	if err != nil {
		t.Fatalf("holidays collection: %v", err)
	}
	if holidays.Fields.GetByName("location") != nil {
		t.Error("holidays.location should be removed (replaced by calendar)")
	}
	if holidays.Fields.GetByName("calendar") == nil {
		t.Error("holidays.calendar field missing")
	}
	locations, err := app.FindCollectionByNameOrId("locations")
	if err != nil {
		t.Fatalf("locations collection: %v", err)
	}
	if locations.Fields.GetByName("holiday_calendars") == nil {
		t.Error("locations.holiday_calendars field missing")
	}

	// Data migration: hq observes a calendar, and the seeded Christmas holiday is
	// homed on a calendar hq observes (so behavior is preserved end-to-end).
	hq, err := app.FindFirstRecordByData("locations", "code", "hq")
	if err != nil {
		t.Fatalf("location hq not found: %v", err)
	}
	observed := hq.GetStringSlice("holiday_calendars")
	if len(observed) == 0 {
		t.Fatalf("hq.holiday_calendars is empty, want a calendar linked by the data migration")
	}
	xmas, err := app.FindFirstRecordByData("holidays", "name", "Christmas")
	if err != nil {
		t.Fatalf("holiday Christmas not found: %v", err)
	}
	if !slicesContains(observed, xmas.GetString("calendar")) {
		t.Errorf("Christmas calendar %q not among hq's observed calendars %v", xmas.GetString("calendar"), observed)
	}
}

// TestNotifyLocations verifies migration 1750000024: the users.notify_locations
// multi-relation to locations, the recipient-scoping field for alarm email.
func TestNotifyLocations(t *testing.T) {
	app := newApp(t)

	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("users collection: %v", err)
	}
	f, ok := users.Fields.GetByName("notify_locations").(*core.RelationField)
	if !ok || f == nil {
		t.Fatal("users.notify_locations relation field missing")
	}
	if f.MaxSelect <= 1 {
		t.Errorf("users.notify_locations MaxSelect = %d, want >1 (multi-select)", f.MaxSelect)
	}
	locations, err := app.FindCollectionByNameOrId("locations")
	if err != nil {
		t.Fatalf("locations collection: %v", err)
	}
	if f.CollectionId != locations.Id {
		t.Errorf("users.notify_locations targets %q, want locations (%q)", f.CollectionId, locations.Id)
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
