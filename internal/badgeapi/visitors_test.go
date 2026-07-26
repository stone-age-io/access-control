package badgeapi

import (
	"strings"
	"testing"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	// Side-effect import registers the schema + fixture migrations.
	_ "github.com/stone-age-io/access-control/pbmigrations"
)

// `cardholders` is an auth collection, so every person is an auth record whether or
// not they ever sign in. These tests pin the three invariants that makes possible —
// a record can always be created, a login is never enabled in an unusable state, and
// deleting a person takes their cards with them.
//
// The tests this file replaced were all about keeping two records in step (a
// `badge_users` login and the cardholder it pointed at). There is nothing left to keep
// in step, which is the point of the collapse.

// newGuardedApp builds a test app with the startup hooks bound. main.go binds these
// before serving; a TestApp does not run main, so without this every cardholder create
// would fail PocketBase's password validation.
func newGuardedApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(app.Cleanup)
	RegisterGuards(app)
	return app
}

// mkCardholder saves a cardholder, applying `set` on top of the defaults.
func mkCardholder(t *testing.T, app core.App, name, email string, set map[string]any) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("cardholders")
	if err != nil {
		t.Fatalf("cardholders collection: %v", err)
	}
	r := core.NewRecord(col)
	r.Set("name", name)
	r.SetEmail(email)
	r.Set("status", "active")
	for k, v := range set {
		r.Set(k, v)
	}
	if err := app.Save(r); err != nil {
		t.Fatalf("save cardholder %q: %v", name, err)
	}
	return r
}

// mkCredential saves an active credential for a cardholder.
func mkCredential(t *testing.T, app core.App, cardholderID, value string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("credentials")
	if err != nil {
		t.Fatalf("credentials collection: %v", err)
	}
	r := core.NewRecord(col)
	r.Set("value", value)
	r.Set("type", "mobile")
	r.Set("user", cardholderID)
	r.Set("status", "active")
	if err := app.Save(r); err != nil {
		t.Fatalf("save credential %q: %v", value, err)
	}
	return r
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

// TestCardholderCreatedWithoutAPassword is the invariant the whole collapse rests on.
// PocketBase force-re-enforces Required on an auth collection's password field
// (core.Collection.initPasswordField), so the ordinary operator cardholder form — which
// has no password box, because most people in a PACS never sign in — could not save
// anyone at all without bindPasswordFill.
//
// Proved by contrast rather than by inspecting the stored value, which PocketBase does
// not expose: the same save is attempted on an app WITHOUT the hooks bound, and must
// fail on exactly that validator.
func TestCardholderCreatedWithoutAPassword(t *testing.T) {
	bare, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	defer bare.Cleanup()
	col, err := bare.FindCollectionByNameOrId("cardholders")
	if err != nil {
		t.Fatalf("cardholders collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("name", "No Login")
	rec.Set("status", "active")
	switch err := bare.Save(rec); {
	case err == nil:
		t.Fatal("a cardholder saved with no password and no hooks; this test proves nothing")
	case !strings.Contains(err.Error(), "password"):
		t.Fatalf("expected a password validation failure, got: %v", err)
	}

	// With the hooks bound, the identical save works.
	app := newGuardedApp(t)
	ch := mkCardholder(t, app, "No Login", "nologin@test.dev", nil)

	// The fill must NOT claim the person knows the password, or a later change would
	// demand a proof they cannot give.
	if ch.GetBool("password_set") {
		t.Error("password_set = true after an automatic fill; nobody has seen that password")
	}
	// And having a password is not having an account: without badge_login the
	// collection's AuthRule refuses to issue a token no matter what is presented.
	if ch.GetBool("badge_login") {
		t.Error("badge_login defaulted to true; being a person must not mean having a login")
	}
}

// TestEmailVisibilityIsSetForOperators covers a silent failure mode: PocketBase strips
// `email` from an auth record's API response for any requester who is not the owner, a
// superuser, or a ManageRule match. An operator browsing the cardholder list is none of
// those unless they hold `enroll`, so without this the list shows blank emails.
func TestEmailVisibilityIsSetForOperators(t *testing.T) {
	app := newGuardedApp(t)
	ch := mkCardholder(t, app, "Visible", "visible@test.dev", nil)
	if !ch.GetBool("emailVisibility") {
		t.Error("emailVisibility = false; operators would see a blank email column")
	}
}

// TestBadgeLoginRequiresAnEmail pins bindLoginRequiresEmail. Email is the sole
// PasswordAuth identity field and the only delivery route for an OTP or a reset, so a
// login without one is unusable by every method — a checkbox that looks enabled and
// silently does nothing.
func TestBadgeLoginRequiresAnEmail(t *testing.T) {
	app := newGuardedApp(t)

	col, err := app.FindCollectionByNameOrId("cardholders")
	if err != nil {
		t.Fatalf("cardholders collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("name", "No Address")
	rec.Set("status", "active")
	rec.Set("badge_login", true)
	err = app.Save(rec)
	if err == nil {
		t.Fatal("saved a badge login with no email; it could never be signed into")
	}
	if !strings.Contains(err.Error(), "badge_login") {
		t.Errorf("error does not name the offending field, so the form cannot show it: %v", err)
	}

	// The same check on update: an address can be cleared as easily as omitted.
	ch := mkCardholder(t, app, "Had One", "hadone@test.dev", map[string]any{"badge_login": true})
	ch.SetEmail("")
	if err := app.Save(ch); err == nil {
		t.Error("cleared the email of a cardholder with a badge login; the login is now unusable")
	}

	// And a person with no email and no login saves fine — that is most of a PACS.
	mkCardholder(t, app, "Contractor", "", nil)
}

// TestDeletingACardholderTakesTheirCards covers migration 1750000036. Two things would
// otherwise be wrong at once: PocketBase refuses to delete the target of a required
// relation, so the delete would fail outright on anyone ever issued a card; and if it
// somehow succeeded, the credential would survive as a key that opens doors and
// resolves to nobody.
func TestDeletingACardholderTakesTheirCards(t *testing.T) {
	app := newGuardedApp(t)
	ch := mkCardholder(t, app, "Leaver", "leaver@test.dev", nil)
	mkCredential(t, app, ch.Id, "CARD-LEAVER1")
	mkCredential(t, app, ch.Id, "CARD-LEAVER2")

	if err := app.Delete(ch); err != nil {
		t.Fatalf("delete cardholder: %v", err)
	}
	if got := credStatuses(t, app, ch.Id); len(got) != 0 {
		t.Errorf("credentials surviving a deleted cardholder: %v — each is a key that resolves to nobody", got)
	}
}

// TestRevokingAVisitorKeepsThePerson is the end-of-visit contract. Revoke, not delete:
// the pass must stop working while the record that they were here survives, so a
// returning visitor is recognised rather than duplicated.
func TestRevokingAVisitorKeepsThePerson(t *testing.T) {
	app := newGuardedApp(t)
	ch := mkCardholder(t, app, "Guest", "guest@test.dev", map[string]any{
		"kind": KindVisitor, "badge_login": true,
	})
	mkCredential(t, app, ch.Id, "V-GUESTPASS1")

	if err := revokeCredentials(app, ch.Id); err != nil {
		t.Fatalf("revokeCredentials: %v", err)
	}

	for _, status := range credStatuses(t, app, ch.Id) {
		if status != "revoked" {
			t.Errorf("credential status = %q after revoking the visit, want %q — the pass still opens doors",
				status, "revoked")
		}
	}
	if _, err := app.FindRecordById("cardholders", ch.Id); err != nil {
		t.Errorf("the visitor record is gone after a revoke: %v — the visit must stay on the record", err)
	}
}
