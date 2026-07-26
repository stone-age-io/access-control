package badgeapi

import (
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	// Side-effect import registers the schema + fixture migrations.
	_ "github.com/stone-age-io/access-control/pbmigrations"
)

// A visitor pass is one thing wearing two records — a `visitor` login and the
// credential minted with it. These tests pin the invariant that ends the visit:
// removing the login must never leave the credential live, because the credential is
// what opens doors and the login is only what shows it.
//
// The staff case is the mirror image and matters just as much: a holder's card must
// survive their login being removed, or "take their phone badge away" would silently
// mean "lock them out of the building".

// seedBadge saves a cardholder, an active credential, and a badge login of the given
// kind, returning the cardholder id and the badge record.
func seedBadge(t *testing.T, app core.App, kind, email, credValue string) (string, *core.Record) {
	t.Helper()
	ch := mkCardholder(t, app, "Seeded "+kind, email)

	creds, err := app.FindCollectionByNameOrId("credentials")
	if err != nil {
		t.Fatalf("credentials collection: %v", err)
	}
	cred := core.NewRecord(creds)
	cred.Set("value", credValue)
	cred.Set("type", "mobile")
	cred.Set("user", ch.Id)
	cred.Set("status", "active")
	if err := app.Save(cred); err != nil {
		t.Fatalf("save credential: %v", err)
	}

	badge, err := upsertHolderBadge(app, nil, ch.Id, email, "")
	if err != nil {
		t.Fatalf("upsertHolderBadge: %v", err)
	}
	if kind != KindHolder {
		badge.Set("kind", kind)
		if err := app.Save(badge); err != nil {
			t.Fatalf("set kind %q: %v", kind, err)
		}
	}
	return ch.Id, badge
}

// credStatuses returns every credential status for a cardholder, in insertion order.
func credStatuses(t *testing.T, app core.App, cardholderID string) []string {
	t.Helper()
	recs, err := app.FindRecordsByFilter("credentials", "user = {:user}", "created", 0, 0,
		dbx.Params{"user": cardholderID})
	if err != nil {
		t.Fatalf("load credentials: %v", err)
	}
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.GetString("status"))
	}
	return out
}

func newGuardedApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(app.Cleanup)
	// main.go binds these at startup; a TestApp does not run main.
	RegisterGuards(app)
	return app
}

// TestDeletingVisitorLoginRevokesTheirPass is the hole this closed. Before the hook,
// the visitors page's only action deleted the login and left the credential `active` —
// and because internal/badgesweep finds credentials to expire by enumerating visitor
// LOGINS, the delete also removed it from the sweep's view for good.
func TestDeletingVisitorLoginRevokesTheirPass(t *testing.T) {
	app := newGuardedApp(t)
	cardholderID, badge := seedBadge(t, app, KindVisitor, "visitor-delete@test.dev", "V-DELETEME01")

	if err := app.Delete(badge); err != nil {
		t.Fatalf("delete visitor login: %v", err)
	}

	for _, status := range credStatuses(t, app, cardholderID) {
		if status != "revoked" {
			t.Errorf("credential status = %q after deleting the visitor login, want %q — the pass still opens doors",
				status, "revoked")
		}
	}
}

// TestDeletingHolderLoginKeepsTheirCard is the other half of the contract: the
// asymmetry between the tiers is deliberate, not an oversight, so the hook must be
// visitor-only.
func TestDeletingHolderLoginKeepsTheirCard(t *testing.T) {
	app := newGuardedApp(t)
	cardholderID, badge := seedBadge(t, app, KindHolder, "holder-delete@test.dev", "CARD-KEEPME1")

	if err := app.Delete(badge); err != nil {
		t.Fatalf("delete holder login: %v", err)
	}

	for _, status := range credStatuses(t, app, cardholderID) {
		if status != "active" {
			t.Errorf("credential status = %q after deleting a STAFF login, want %q — removing a phone badge must not lock someone out of the building",
				status, "active")
		}
	}
}

// TestRevokeVisitorPassKeepsTheLogin covers the other verb. Revoking ends the visit but
// keeps the record of it, which is what lets the mint route recognise a returning
// visitor instead of duplicating them.
func TestRevokeVisitorPassKeepsTheLogin(t *testing.T) {
	app := newGuardedApp(t)
	cardholderID, badge := seedBadge(t, app, KindVisitor, "visitor-revoke@test.dev", "V-REVOKEME01")

	if err := revokeVisitorPass(app, badge); err != nil {
		t.Fatalf("revokeVisitorPass: %v", err)
	}

	for _, status := range credStatuses(t, app, cardholderID) {
		if status != "revoked" {
			t.Errorf("credential status = %q, want %q", status, "revoked")
		}
	}
	if _, err := app.FindRecordById(BadgeCollection, badge.Id); err != nil {
		t.Errorf("the badge login was removed by a revoke: %v", err)
	}
}

// TestDeletingCardholderCascadesToTheLogin covers migration 1750000035. An orphan login
// authenticates fine and then resolves no cardholder, which is a credential-shaped
// object outliving the person it was about.
//
// The cardholder here holds no credential, which is not a contrived setup: PocketBase
// refuses to delete a cardholder that has one at all (`credentials.user` is a required
// relation), so "cardholder with a badge login and no credential yet" is the only shape
// in which this orphan could ever be created.
func TestDeletingCardholderCascadesToTheLogin(t *testing.T) {
	app := newGuardedApp(t)
	ch := mkCardholder(t, app, "No Credentials", "cascade@test.dev")
	badge, err := upsertHolderBadge(app, nil, ch.Id, "cascade@test.dev", "")
	if err != nil {
		t.Fatalf("upsertHolderBadge: %v", err)
	}

	if err := app.Delete(ch); err != nil {
		t.Fatalf("delete cardholder: %v", err)
	}

	if _, err := app.FindRecordById(BadgeCollection, badge.Id); err == nil {
		t.Error("the badge login survived its cardholder — it can still sign in and resolve nobody")
	}
}
