package authz_test

import (
	"testing"

	"github.com/pocketbase/pocketbase/tests"
	"github.com/stone-age-io/access-control/internal/authz"

	// Side-effect import registers the migrations that define users.permissions.
	_ "github.com/stone-age-io/access-control/pbmigrations"
)

func TestUpsertOperator(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(app.Cleanup)

	const email = "first@example.com"
	created, err := authz.UpsertOperator(app, email, "first-password")
	if err != nil || !created {
		t.Fatalf("first upsert: created=%v err=%v, want created", created, err)
	}

	// A second upsert resets the password of the same record and grants every
	// capability again, which is how a locked-out operator gets back in.
	rec, _ := app.FindAuthRecordByEmail("users", email)
	rec.Set("permissions", []string{authz.CapEnroll})
	if err := app.Save(rec); err != nil {
		t.Fatalf("narrow permissions: %v", err)
	}
	created, err = authz.UpsertOperator(app, email, "second-password")
	if err != nil || created {
		t.Fatalf("second upsert: created=%v err=%v, want an update", created, err)
	}

	rec, err = app.FindAuthRecordByEmail("users", email)
	if err != nil {
		t.Fatalf("operator not found: %v", err)
	}
	if !rec.ValidatePassword("second-password") || rec.ValidatePassword("first-password") {
		t.Error("password was not replaced")
	}
	if !rec.Verified() {
		t.Error("operator is not verified")
	}
	for _, c := range authz.AllCapabilities {
		if !authz.HasCapability(rec, c) {
			t.Errorf("operator lacks capability %q", c)
		}
	}
}
