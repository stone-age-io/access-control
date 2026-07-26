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

// TestCardholderAuthCollection verifies that `cardholders` is the badge tier: one
// collection holding both the person and their login (1750000000).
//
// The rules are the point. An auth collection's DEFAULT create rule is OPEN SIGNUP,
// which here would let anyone mint themselves a person record in a physical access
// control system; and because this same collection is the policy graph's entry point
// (cardholder -> roles -> groups -> portals), a write rule that admitted a badge token
// would be a self-service grant of doors.
func TestCardholderAuthCollection(t *testing.T) {
	app := newApp(t)

	c, err := app.FindCollectionByNameOrId("cardholders")
	if err != nil {
		t.Fatalf("cardholders collection: %v", err)
	}
	if !c.IsAuth() {
		t.Fatal("cardholders is not an auth collection; the badge tier has nothing to sign in to")
	}

	// --- create must never be open signup ---
	if c.CreateRule == nil || *c.CreateRule == "" {
		t.Fatalf("cardholders.CreateRule = %v, want enroll-gated (empty string is OPEN SIGNUP)", c.CreateRule)
	}
	if !strings.Contains(*c.CreateRule, "enroll") {
		t.Errorf("cardholders.CreateRule = %q, want it to require the enroll capability", *c.CreateRule)
	}

	// --- writes must exclude the badge tier entirely ---
	// No self clause, which is what let the old badge_users field guard be deleted: a
	// holder who cannot PATCH their own row cannot escalate through it.
	for name, rule := range map[string]*string{
		"UpdateRule": c.UpdateRule,
		"DeleteRule": c.DeleteRule,
	} {
		if rule == nil {
			continue // superuser-only is stricter still
		}
		if strings.Contains(*rule, "id = @request.auth.id") {
			t.Errorf("cardholders.%s = %q allows self-write; a holder could grant themselves roles", name, *rule)
		}
	}

	// --- reads: the operator floor OR self ---
	// The self clause is load-bearing beyond the API: PocketBase checks a PROTECTED
	// file download against the record's ViewRule, and cardholders.photo is protected
	// (1750000029). Without it a holder's own badge renders with no face on it.
	for name, rule := range map[string]*string{"ListRule": c.ListRule, "ViewRule": c.ViewRule} {
		if rule == nil {
			t.Errorf("cardholders.%s is nil; operators cannot read the people list", name)
			continue
		}
		for _, want := range []string{`id = @request.auth.id`, `@request.auth.collectionName = "users"`} {
			if !strings.Contains(*rule, want) {
				t.Errorf("cardholders.%s = %q, want it to contain %s", name, *rule, want)
			}
		}
	}

	// --- AuthRule: being a person is not having an account ---
	// Every cardholder is an auth record carrying a random password nobody has seen, so
	// this rule is the only thing separating the two.
	if c.AuthRule == nil || *c.AuthRule == "" {
		t.Fatalf("cardholders.AuthRule = %v, want it gated on badge_login (empty = any record may sign in)", c.AuthRule)
	}
	for _, want := range []string{"badge_login = true", "active"} {
		if !strings.Contains(*c.AuthRule, want) {
			t.Errorf("cardholders.AuthRule = %q, want it to contain %s", *c.AuthRule, want)
		}
	}
	// NOT `verified`: PocketBase writes that field itself on a first OTP sign-in, and
	// requestOTP does not consult the AuthRule at all -- so a verified-gated rule opens
	// itself on first use.
	if strings.Contains(*c.AuthRule, "verified") {
		t.Error("cardholders.AuthRule gates on `verified`, which PocketBase sets itself during OTP sign-in")
	}

	// --- auth methods: all three coexist ---
	// Password matters most where SMTP does not exist: with OTP as the only method,
	// every sign-in is an emailed code and the tier is inert.
	if !c.PasswordAuth.Enabled {
		t.Error("cardholders password auth is disabled; the tier is inert on an install with no SMTP")
	}
	if len(c.PasswordAuth.IdentityFields) != 1 || c.PasswordAuth.IdentityFields[0] != "email" {
		t.Errorf("cardholders identity fields = %v, want [email]", c.PasswordAuth.IdentityFields)
	}
	if !c.OTP.Enabled {
		t.Error("cardholders OTP is disabled; it is the visitor's only sign-in path")
	}
	if !c.OAuth2.Enabled {
		t.Error("cardholders OAuth2 is disabled; all three methods are meant to coexist")
	}

	// --- email stays OPTIONAL ---
	// It is UNIQUE, so requiring it would force a synthetic address onto every
	// contractor, hourly worker and non-person card that has no inbox, and would
	// hard-fail an LDAP/CSV import with a sparse email column.
	if f, ok := c.Fields.GetByName("email").(*core.EmailField); !ok || f == nil {
		t.Error("cardholders.email is not an email field")
	} else if f.Required {
		t.Error("cardholders.email is required; a cardholder with no inbox could not be enrolled")
	}

	// --- badge fields ---
	for _, name := range []string{"badge_login", "password_set", "kind"} {
		if c.Fields.GetByName(name) == nil {
			t.Errorf("cardholders.%s field missing", name)
		}
	}
	if k, ok := c.Fields.GetByName("kind").(*core.SelectField); ok && k != nil {
		for _, want := range []string{"holder", "visitor"} {
			if !slicesContains(k.Values, want) {
				t.Errorf("cardholders.kind missing value %q (have %v)", want, k.Values)
			}
		}
	}

	// ManageRule lets an enroll operator reset a stuck login without the old password,
	// and is also what unhides `email` for them in list responses.
	if c.ManageRule == nil || !strings.Contains(*c.ManageRule, "enroll") {
		t.Errorf("cardholders.ManageRule = %v, want enroll-gated", c.ManageRule)
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
	otp, ok := find("cardholders:requestOTP")
	if !ok {
		t.Fatalf("no rate limit rule for badge-tier OTP requests; rules = %+v", rl.Rules)
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

// TestBadgeHolderCannotReadPolicyGraph is the end-to-end version of
// TestReadFloorExcludesNonOperatorAuth against the REAL badge tier.
//
// It matters more now than it did, because the badge tier and the operator tier share a
// collection. `cardholders` is simultaneously the thing a holder signs in as and a node
// in the policy graph an operator reads, so the read floor is carrying two jobs at once:
// admit operators to everything, and admit a holder to exactly one row. If it slipped to
// the old auth-collection-agnostic form (`@request.auth.id != ""`), a lobby visitor
// could list `credentials` — whose `value` field is the credential secret in plaintext —
// and enumerate every card in the building.
func TestBadgeHolderCannotReadPolicyGraph(t *testing.T) {
	app := newApp(t)

	cardholders, err := app.FindCollectionByNameOrId("cardholders")
	if err != nil {
		t.Fatalf("cardholders collection: %v", err)
	}
	alice, err := app.FindFirstRecordByData("cardholders", "external_id", "alice")
	if err != nil {
		t.Fatalf("fixture cardholder alice not found: %v", err)
	}

	// A visitor: a cardholder with a badge login, which is all a badge token is.
	visitor := core.NewRecord(cardholders)
	visitor.SetEmail("visitor@test.dev")
	visitor.Set("name", "Lobby Visitor")
	visitor.Set("status", "active")
	visitor.Set("kind", "visitor")
	visitor.Set("badge_login", true)
	// PocketBase requires a non-blank password on every auth record. accessd fills one
	// automatically (badgeapi.RegisterGuards); this package does not bind those hooks.
	visitor.SetPassword("unused-password-not-a-sign-in-path")
	if err := app.Save(visitor); err != nil {
		t.Fatalf("save visitor: %v", err)
	}

	// The credential VALUE is the credential secret -- that row above all must never be
	// listable by the badge tier.
	for _, name := range []string{"credentials", "portals", "areas", "events", "roles", "access_groups"} {
		c, err := app.FindCollectionByNameOrId(name)
		if err != nil {
			t.Errorf("%s collection: %v", name, err)
			continue
		}
		ok, err := app.CanAccessRecord(alice, &core.RequestInfo{Auth: visitor, Method: "GET"}, c.ListRule)
		if err != nil {
			t.Fatalf("%s: CanAccessRecord error: %v", name, err)
		}
		if ok {
			t.Errorf("a badge holder can list %s -- the policy graph is exposed to the badge tier", name)
		}
	}

	// And the sharp one for a shared collection: another PERSON must be invisible. The
	// rule is evaluated against the other record, which is what a list request does.
	ok, err := app.CanAccessRecord(alice, &core.RequestInfo{Auth: visitor, Method: "GET"}, cardholders.ListRule)
	if err != nil {
		t.Fatalf("cardholders peer check: CanAccessRecord error: %v", err)
	}
	if ok {
		t.Error("a badge holder can read another cardholder; want self-scoped only")
	}
	// Self must still pass, or the badge cannot render its own photo (protected files
	// are checked against the ViewRule).
	ok, err = app.CanAccessRecord(visitor, &core.RequestInfo{Auth: visitor, Method: "GET"}, cardholders.ViewRule)
	if err != nil {
		t.Fatalf("cardholders self check: CanAccessRecord error: %v", err)
	}
	if !ok {
		t.Error("a badge holder cannot read their OWN cardholder record; their badge would have no photo")
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

// TestGroupTargets verifies migration 1750000037: an access group grants areas and
// aux outputs alongside portals, with arm and disarm as separate rights.
func TestGroupTargets(t *testing.T) {
	app := newApp(t)

	groups, err := app.FindCollectionByNameOrId("access_groups")
	if err != nil {
		t.Fatalf("access_groups collection: %v", err)
	}

	areas, ok := groups.Fields.GetByName("areas").(*core.RelationField)
	if !ok {
		t.Fatal("access_groups.areas is missing or not a relation")
	}
	if areas.MaxSelect < 2 {
		t.Errorf("areas.MaxSelect = %d, want many (a group grants a SET of areas)", areas.MaxSelect)
	}
	outputs, ok := groups.Fields.GetByName("aux_outputs").(*core.RelationField)
	if !ok {
		t.Fatal("access_groups.aux_outputs is missing or not a relation")
	}
	if outputs.MaxSelect < 2 {
		t.Errorf("aux_outputs.MaxSelect = %d, want many", outputs.MaxSelect)
	}

	// Both rights must be selectable together, or "may arm and disarm" — the common
	// case — would be unrepresentable.
	rights, ok := groups.Fields.GetByName("area_rights").(*core.SelectField)
	if !ok {
		t.Fatal("access_groups.area_rights is missing or not a select")
	}
	if rights.MaxSelect < 2 {
		t.Errorf("area_rights.MaxSelect = %d, want 2", rights.MaxSelect)
	}
	want := map[string]bool{"arm": false, "disarm": false}
	for _, v := range rights.Values {
		if _, known := want[v]; !known {
			t.Errorf("unexpected area_rights value %q", v)
			continue
		}
		want[v] = true
	}
	for v, present := range want {
		if !present {
			t.Errorf("area_rights is missing the %q value", v)
		}
	}

	// The rules are unchanged: granting an area is the same class of decision as
	// granting a door, so it stays `policy`-gated rather than becoming `command`.
	if groups.UpdateRule == nil || *groups.UpdateRule != `@request.auth.permissions ~ "policy"` {
		t.Errorf("access_groups.UpdateRule = %v, want the unchanged policy capability", groups.UpdateRule)
	}
}

// TestGroupTargetsFixture verifies 1750000038 — the demo group actually grants the
// demo area, so a fresh checkout can exercise badge arm/disarm without hand-building
// a graph. It is also what backs the mirror's code-resolution test.
func TestGroupTargetsFixture(t *testing.T) {
	app := newApp(t)

	group, err := app.FindFirstRecordByData("access_groups", "code", "lobby-group")
	if err != nil {
		t.Fatalf("fixture group: %v", err)
	}
	if len(group.GetStringSlice("areas")) != 1 {
		t.Errorf("lobby-group areas = %v, want the warehouse area", group.GetStringSlice("areas"))
	}
	if len(group.GetStringSlice("aux_outputs")) != 1 {
		t.Errorf("lobby-group aux_outputs = %v, want the lobby gate", group.GetStringSlice("aux_outputs"))
	}
	if len(group.GetStringSlice("area_rights")) != 2 {
		t.Errorf("lobby-group area_rights = %v, want arm and disarm", group.GetStringSlice("area_rights"))
	}
	if _, err := app.FindFirstRecordByData("aux_output", "code", "lobby-gate"); err != nil {
		t.Errorf("fixture aux output lobby-gate missing: %v", err)
	}
}
