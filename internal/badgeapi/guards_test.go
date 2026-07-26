package badgeapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// # What these tests used to cover, and why they changed shape
//
// The badge tier used to live in its own `badge_users` collection whose update rule was
// `id = @request.auth.id || <operator enroll>` — a holder could PATCH their own login,
// which the tier needed. A collection rule selects which RECORDS may be written and
// never which FIELDS, so that rule silently also permitted rewriting every non-system
// field on the record, three of which were load-bearing: `cardholder` (whose credentials
// this login resolves — repointing it inherited another person's doors), `kind` (whether
// the QR carries the credential VALUE or an inert identifier), and `password_set`
// (whether a password change must prove the old one). A field-level hook guarded all
// three, and each was a verified escalation before it existed.
//
// Collapsing the login into the cardholder removed the need for the self-update clause:
// `cardholders` is `enroll`-gated for writes with NO self clause, and a holder changes
// their password through POST /api/badge/password, which is an app.Save and bypasses
// collection rules by design. The escalation surface is gone rather than guarded — so
// what is left to test is that it is really gone, and that removing it did not also
// remove the self-READ the badge depends on.
//
// Fixed record ids (exactly 15 chars, PocketBase's id length) so the request URL is
// known before the app is built.
const (
	holderID = "cardholderself1"
	victimID = "cardholdervictm"
)

// seedGuardApp builds an app holding a badge holder plus another cardholder to covet,
// with the startup hooks bound, and returns it with the holder's auth token.
func seedGuardApp(t testing.TB) (*tests.TestApp, string) {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	RegisterGuards(app)

	col, err := app.FindCollectionByNameOrId("cardholders")
	if err != nil {
		t.Fatalf("cardholders collection: %v", err)
	}
	for id, spec := range map[string][2]string{
		holderID: {"Holder", "holder@test.dev"},
		victimID: {"Victim", "victim@test.dev"},
	} {
		ch := core.NewRecord(col)
		ch.Id = id
		ch.Set("name", spec[0])
		ch.SetEmail(spec[1])
		ch.Set("status", "active")
		ch.Set("kind", KindHolder)
		if id == holderID {
			ch.Set("badge_login", true)
			ch.SetPassword("holderpass123")
			ch.Set("password_set", true)
			ch.SetVerified(true)
		}
		if err := app.Save(ch); err != nil {
			t.Fatalf("save cardholder %s: %v", spec[0], err)
		}
	}

	holder, err := app.FindRecordById("cardholders", holderID)
	if err != nil {
		t.Fatalf("reload holder: %v", err)
	}
	token, err := holder.NewAuthToken()
	if err != nil {
		t.Fatalf("NewAuthToken: %v", err)
	}
	return app, token
}

// TestBadgeHolderCannotWriteTheirOwnRecord is the property that replaced the field
// guard. The collection rule is the whole boundary now, so this asserts it directly: no
// PATCH from a badge token lands, whatever it carries. Each case is an escalation the
// old self-update rule made possible and the old hook had to intercept one by one; here
// they are all refused by the same rule, which is why there is no hook left to get
// wrong.
//
// The assertion is on the STORED VALUE as well as the status, because a rule that
// returns an error while still persisting the write would be no rule at all.
func TestBadgeHolderCannotWriteTheirOwnRecord(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    string
		suspend bool
		check   func(t testing.TB, rec *core.Record)
	}{
		{
			// `kind` decides whether the QR carries the credential VALUE. Flipping a
			// lanyard badge to `visitor` mints a permanent, photographable building key
			// out of an identifier that opened nothing.
			name: "cannot promote itself to a visitor badge for a credential-bearing QR",
			body: `{"kind":"visitor"}`,
			check: func(t testing.TB, rec *core.Record) {
				if got := rec.GetString("kind"); got != KindHolder {
					t.Errorf("kind = %q, want %q — the QR would now carry the credential value", got, KindHolder)
				}
			},
		},
		{
			// Clearing `password_set` lets a stolen session change the password with no
			// proof, locking the real holder out of their own badge.
			name: "cannot clear password_set to skip the old-password proof",
			body: `{"password_set":false}`,
			check: func(t testing.TB, rec *core.Record) {
				if !rec.GetBool("password_set") {
					t.Error("password_set was cleared — a stolen session could now change the password without proving the current one")
				}
			},
		},
		{
			// New surface, and the sharpest one: with the login collapsed into the
			// person, the record a holder might write is the same record that grants
			// doors. `roles` is the graph's entry point (cardholder → roles → groups →
			// portals), so a successful self-write here is a grant at the reader.
			name: "cannot grant itself a role",
			body: `{"roles":["anything"]}`,
			check: func(t testing.TB, rec *core.Record) {
				if got := rec.GetStringSlice("roles"); len(got) != 0 {
					t.Errorf("roles = %v — the holder granted themselves access at the door", got)
				}
			},
		},
		{
			// `status` is what withdraws a badge; policy.Decide denies a suspended
			// cardholder at the reader. Self-service un-suspension would undo it.
			name:    "cannot un-suspend itself",
			body:    `{"status":"active"}`,
			suspend: true,
			check: func(t testing.TB, rec *core.Record) {
				if got := rec.GetString("status"); got != "suspended" {
					t.Errorf("status = %q, want suspended — the holder reinstated their own access", got)
				}
			},
		},
		{
			// Repointing the old `cardholder` relation is impossible now (the holder IS
			// the record), so the equivalent is taking a different identity on the badge
			// face a guard would look at.
			name: "cannot rewrite the name on its own badge",
			body: `{"name":"Someone Else"}`,
			check: func(t testing.TB, rec *core.Record) {
				if got := rec.GetString("name"); got != "Holder" {
					t.Errorf("name = %q, want %q", got, "Holder")
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, token := seedGuardApp(t)
			defer app.Cleanup()

			if tc.suspend {
				rec, err := app.FindRecordById("cardholders", holderID)
				if err != nil {
					t.Fatalf("reload holder: %v", err)
				}
				rec.Set("status", "suspended")
				if err := app.Save(rec); err != nil {
					t.Fatalf("suspend holder: %v", err)
				}
			}

			scenario := tests.ApiScenario{
				Name:                  tc.name,
				Method:                http.MethodPatch,
				URL:                   "/api/collections/" + BadgeCollection + "/records/" + holderID,
				Body:                  strings.NewReader(tc.body),
				Headers:               map[string]string{"Authorization": token},
				TestAppFactory:        func(testing.TB) *tests.TestApp { return app },
				DisableTestAppCleanup: true,
				// 404 rather than 403: with no self clause in the update rule the record
				// is not merely unwritable, it is invisible TO A WRITE — PocketBase looks
				// for a record the rule admits and finds none. Either way nothing lands,
				// and 404 leaks less than 403 would.
				ExpectedStatus:  http.StatusNotFound,
				ExpectedContent: []string{`"message"`},
				AfterTestFunc: func(t testing.TB, app *tests.TestApp, _ *http.Response) {
					rec, err := app.FindRecordById("cardholders", holderID)
					if err != nil {
						t.Fatalf("reload holder: %v", err)
					}
					tc.check(t, rec)
				},
			}
			scenario.Test(t)
		})
	}
}

// TestBadgeHolderCanStillReadTheirOwnRecord is the other half, and it is not cosmetic:
// the read floor (1750000027) grants `id = @request.auth.id`, and that clause is what
// lets a holder's own PROTECTED photo download pass PocketBase's ViewRule check
// (apis/file.go). Without it the badge renders with no face on it — which is exactly
// what happened while the login was a separate record.
func TestBadgeHolderCanStillReadTheirOwnRecord(t *testing.T) {
	app, token := seedGuardApp(t)
	defer app.Cleanup()

	scenario := tests.ApiScenario{
		Name:                  "a badge holder reads their own cardholder record",
		Method:                http.MethodGet,
		URL:                   "/api/collections/" + BadgeCollection + "/records/" + holderID,
		Headers:               map[string]string{"Authorization": token},
		TestAppFactory:        func(testing.TB) *tests.TestApp { return app },
		DisableTestAppCleanup: true,
		ExpectedStatus:        http.StatusOK,
		ExpectedContent:       []string{`"id":"` + holderID + `"`},
	}
	scenario.Test(t)
}

// TestBadgeHolderListSeesOnlyThemselves bounds that read to self, through the LIST
// endpoint — the one an enumeration attempt would actually use. Sharing one collection
// between the two tiers makes this the load-bearing test of the split, and a view-rule
// test does not cover it: list applies the rule as a FILTER and returns 200 with whatever
// passes, so a widened rule shows up here as extra rows rather than as an error.
func TestBadgeHolderListSeesOnlyThemselves(t *testing.T) {
	app, token := seedGuardApp(t)
	defer app.Cleanup()

	scenario := tests.ApiScenario{
		Name:                  "a badge holder's cardholder list contains only their own row",
		Method:                http.MethodGet,
		URL:                   "/api/collections/" + BadgeCollection + "/records",
		Headers:               map[string]string{"Authorization": token},
		TestAppFactory:        func(testing.TB) *tests.TestApp { return app },
		DisableTestAppCleanup: true,
		ExpectedStatus:        http.StatusOK,
		ExpectedContent: []string{
			`"totalItems":1`,
			`"id":"` + holderID + `"`,
		},
		NotExpectedContent: []string{victimID, "victim@test.dev"},
	}
	scenario.Test(t)
}

// TestOperatorCanStillManageBadgeLogins: removing the self-update clause must not have
// taken the operator flow with it. Enabling someone's badge login is exactly what
// `enroll` is for, and the cardholder form does it through this same collection API —
// which is the whole reason /api/badge/holders could be deleted.
func TestOperatorCanStillManageBadgeLogins(t *testing.T) {
	app, _ := seedGuardApp(t)
	defer app.Cleanup()

	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("users collection: %v", err)
	}
	op := core.NewRecord(users)
	op.SetEmail("enroller@test.dev")
	op.SetPassword("operator-password")
	op.Set("permissions", []string{"enroll"})
	if err := app.Save(op); err != nil {
		t.Fatalf("save operator: %v", err)
	}
	opToken, err := op.NewAuthToken()
	if err != nil {
		t.Fatalf("operator NewAuthToken: %v", err)
	}

	scenario := tests.ApiScenario{
		Name:                  "an enroll operator may enable a badge login on a cardholder",
		Method:                http.MethodPatch,
		URL:                   "/api/collections/" + BadgeCollection + "/records/" + victimID,
		Body:                  strings.NewReader(`{"badge_login":true}`),
		Headers:               map[string]string{"Authorization": opToken},
		TestAppFactory:        func(testing.TB) *tests.TestApp { return app },
		DisableTestAppCleanup: true,
		ExpectedStatus:        http.StatusOK,
		ExpectedContent:       []string{`"badge_login":true`},
	}
	scenario.Test(t)
}

// TestOTPWouldEraseAKnownPassword pins the predicate behind bindOTPPasswordPreservation.
//
// The bug it guards against is silent and unrecoverable by the holder: PocketBase
// randomises an auth record's password when a first OTP sign-in flips `verified`, so an
// operator-set initial password — the only path that works with no SMTP — is destroyed
// by the holder choosing the emailed code instead. `password_set` stays true, so the
// self-service change route then demands a password that no longer exists.
func TestOTPWouldEraseAKnownPassword(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	defer app.Cleanup()

	col, err := app.FindCollectionByNameOrId(BadgeCollection)
	if err != nil {
		t.Fatalf("cardholders collection: %v", err)
	}

	cases := []struct {
		name        string
		passwordSet bool
		verified    bool
		want        bool
	}{
		{"operator-set password, not yet verified", true, false, true},
		{"operator-set password, already verified", true, true, false},
		{"random fill only", false, false, false},
		{"random fill, verified", false, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := core.NewRecord(col)
			rec.Set("password_set", tc.passwordSet)
			rec.SetVerified(tc.verified)
			if got := otpWouldEraseAKnownPassword(rec); got != tc.want {
				t.Errorf("otpWouldEraseAKnownPassword() = %v, want %v", got, tc.want)
			}
		})
	}

	// A nil record must not panic: the hook runs on request traffic, and an event with
	// no resolved record is a shape the handler has to tolerate rather than crash on.
	if otpWouldEraseAKnownPassword(nil) {
		t.Error("otpWouldEraseAKnownPassword(nil) = true, want false")
	}
}
