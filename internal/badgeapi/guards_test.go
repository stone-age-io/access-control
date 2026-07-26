package badgeapi

import (
	"net/http"
	"strings"
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
)

// The badge_users update rule is `id = @request.auth.id || <operator enroll>`, so a
// signed-in badge holder may PATCH their OWN record. A collection rule selects which
// RECORDS may be written, never which FIELDS — so on its own it lets the holder rewrite
// every non-system field on that record. Three of them are load-bearing; see
// protectedBadgeFields in guards.go for what each one decides.
//
// Verified before the guard existed: all three PATCHes returned 200 and the stored
// record changed — repointing `cardholder` at another person made GET /api/badge/me
// resolve their credentials.
//
// Fixed record ids (exactly 15 chars, PocketBase's id length) so the request URL is
// known before the app is built.
const (
	guardBadgeID    = "badgeuserself01"
	guardVictimCh   = "chvictim0000001"
	guardAttackerCh = "chattacker00001"
)

// seedGuardApp builds an app holding an attacker's badge plus a victim cardholder, with
// the guard bound, and returns it with the attacker's auth token.
func seedGuardApp(t testing.TB) (*tests.TestApp, string) {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	// main.go binds this at startup; a TestApp does not run main.
	RegisterGuards(app)

	cardholders, err := app.FindCollectionByNameOrId("cardholders")
	if err != nil {
		t.Fatalf("cardholders collection: %v", err)
	}
	for id, name := range map[string]string{guardAttackerCh: "Attacker", guardVictimCh: "Victim"} {
		ch := core.NewRecord(cardholders)
		ch.Id = id
		ch.Set("name", name)
		ch.Set("status", "active")
		if err := app.Save(ch); err != nil {
			t.Fatalf("save cardholder %s: %v", name, err)
		}
	}

	badgeCol, err := app.FindCollectionByNameOrId(BadgeCollection)
	if err != nil {
		t.Fatalf("badge_users collection: %v", err)
	}
	badge := core.NewRecord(badgeCol)
	badge.Id = guardBadgeID
	badge.SetEmail("attacker@test.dev")
	badge.Set("cardholder", guardAttackerCh)
	badge.Set("kind", KindHolder)
	badge.Set("password_set", true)
	badge.SetPassword("a-real-password-they-know")
	if err := app.Save(badge); err != nil {
		t.Fatalf("save badge user: %v", err)
	}

	token, err := badge.NewAuthToken()
	if err != nil {
		t.Fatalf("NewAuthToken: %v", err)
	}
	return app, token
}

// TestBadgeHolderCannotEscalateViaSelfUpdate is the security boundary for badge-tier
// self-update. Each case PATCHes the holder's own record and asserts the protected
// field is unchanged afterwards — the assertion is on the STORED VALUE as well as the
// status, because a guard that 403s but still persisted the write would be no guard.
func TestBadgeHolderCannotEscalateViaSelfUpdate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		body  string
		check func(t testing.TB, rec *core.Record)
	}{
		{
			name: "cannot repoint the badge at another person's cardholder",
			body: `{"cardholder":"` + guardVictimCh + `"}`,
			check: func(t testing.TB, rec *core.Record) {
				if got := rec.GetString("cardholder"); got != guardAttackerCh {
					t.Errorf("cardholder = %q, want %q — the badge now resolves another person's credentials", got, guardAttackerCh)
				}
			},
		},
		{
			name: "cannot promote itself to a visitor badge to get a credential-bearing QR",
			body: `{"kind":"visitor"}`,
			check: func(t testing.TB, rec *core.Record) {
				if got := rec.GetString("kind"); got != KindHolder {
					t.Errorf("kind = %q, want %q — the QR would now carry the credential value", got, KindHolder)
				}
			},
		},
		{
			name: "cannot clear password_set to skip the old-password proof",
			body: `{"password_set":false}`,
			check: func(t testing.TB, rec *core.Record) {
				if !rec.GetBool("password_set") {
					t.Error("password_set was cleared — a stolen session could now change the password without proving the current one")
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, token := seedGuardApp(t)
			defer app.Cleanup()

			scenario := tests.ApiScenario{
				Name:                  tc.name,
				Method:                http.MethodPatch,
				URL:                   "/api/collections/" + BadgeCollection + "/records/" + guardBadgeID,
				Body:                  strings.NewReader(tc.body),
				Headers:               map[string]string{"Authorization": token},
				TestAppFactory:        func(testing.TB) *tests.TestApp { return app },
				DisableTestAppCleanup: true,
				ExpectedStatus:        http.StatusForbidden,
				ExpectedContent:       []string{`"message"`},
				AfterTestFunc: func(t testing.TB, app *tests.TestApp, _ *http.Response) {
					rec, err := app.FindRecordById(BadgeCollection, guardBadgeID)
					if err != nil {
						t.Fatalf("reload badge: %v", err)
					}
					tc.check(t, rec)
				},
			}
			scenario.Test(t)
		})
	}
}

// TestBadgeHolderCanStillUpdateHarmlessFields keeps the guard honest: it must block the
// escalation vectors without freezing the record. A self-update touching none of the
// protected fields has to keep working, or the guard has traded one bug for another.
func TestBadgeHolderCanStillUpdateHarmlessFields(t *testing.T) {
	app, token := seedGuardApp(t)
	defer app.Cleanup()

	scenario := tests.ApiScenario{
		Name:                  "self-update of an unprotected field succeeds",
		Method:                http.MethodPatch,
		URL:                   "/api/collections/" + BadgeCollection + "/records/" + guardBadgeID,
		Body:                  strings.NewReader(`{"emailVisibility":true}`),
		Headers:               map[string]string{"Authorization": token},
		TestAppFactory:        func(testing.TB) *tests.TestApp { return app },
		DisableTestAppCleanup: true,
		ExpectedStatus:        http.StatusOK,
		ExpectedContent:       []string{`"id":"` + guardBadgeID + `"`},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, _ *http.Response) {
			rec, err := app.FindRecordById(BadgeCollection, guardBadgeID)
			if err != nil {
				t.Fatalf("reload badge: %v", err)
			}
			if rec.GetString("cardholder") != guardAttackerCh || rec.GetString("kind") != KindHolder {
				t.Error("an unrelated self-update disturbed the protected fields")
			}
		},
	}
	scenario.Test(t)
}

// TestOperatorCanStillManageBadgeFields: the guard must not block the operator flows.
// Issuing and re-pointing a badge login is exactly what `enroll` is for, and the
// cardholder-page UI does it through this same collection API.
func TestOperatorCanStillManageBadgeFields(t *testing.T) {
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
		Name:                  "an enroll operator may repoint a badge login",
		Method:                http.MethodPatch,
		URL:                   "/api/collections/" + BadgeCollection + "/records/" + guardBadgeID,
		Body:                  strings.NewReader(`{"cardholder":"` + guardVictimCh + `"}`),
		Headers:               map[string]string{"Authorization": opToken},
		TestAppFactory:        func(testing.TB) *tests.TestApp { return app },
		DisableTestAppCleanup: true,
		ExpectedStatus:        http.StatusOK,
		ExpectedContent:       []string{`"cardholder":"` + guardVictimCh + `"`},
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
		t.Fatalf("badge_users collection: %v", err)
	}

	cases := []struct {
		name        string
		passwordSet bool
		verified    bool
		want        bool
	}{
		{"operator-set password, not yet verified", true, false, true},
		{"operator-set password, already verified", true, true, false},
		{"throwaway password only", false, false, false},
		{"throwaway password, verified", false, true, false},
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
