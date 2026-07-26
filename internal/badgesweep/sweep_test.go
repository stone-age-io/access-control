package badgesweep

import (
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/types"

	"github.com/stone-age-io/access-control/internal/logger"

	// Side-effect import registers the schema + fixture migrations.
	_ "github.com/stone-age-io/access-control/pbmigrations"
)

var now = time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)

func newApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(app.Cleanup)
	return app
}

func newSweeper(t *testing.T, app core.App) *Sweeper {
	t.Helper()
	return New(app, logger.NewNopLogger())
}

// mkCardholder creates a cardholder of the given kind (empty kind = neither holder nor
// visitor, i.e. outside this sweep's scope).
//
// cardholders is an AUTH collection, so PocketBase requires a non-blank password on
// every record. accessd fills one automatically (badgeapi.RegisterGuards), but this
// package does not depend on the HTTP layer and a TestApp does not run main — so the
// password is set explicitly here. It is not a sign-in path: `badge_login` is unset, so
// the collection's AuthRule refuses to issue a token regardless.
func mkCardholder(t *testing.T, app core.App, name, email, kind string) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId(badgeCollection)
	if err != nil {
		t.Fatalf("%s collection: %v", badgeCollection, err)
	}
	ch := core.NewRecord(col)
	ch.Set("name", name)
	ch.Set("status", "active")
	ch.SetEmail(email)
	ch.SetPassword("unused-password-not-a-sign-in-path")
	if kind != "" {
		ch.Set("kind", kind)
	}
	if err := app.Save(ch); err != nil {
		t.Fatalf("save cardholder %s: %v", name, err)
	}
	return ch
}

// mkCred saves a credential for a cardholder. A zero `until` leaves valid_until empty
// (unbounded).
func mkCred(t *testing.T, app core.App, cardholderID, value, status string, until time.Time) *core.Record {
	t.Helper()
	col, err := app.FindCollectionByNameOrId("credentials")
	if err != nil {
		t.Fatalf("credentials collection: %v", err)
	}
	c := core.NewRecord(col)
	c.Set("value", value)
	c.Set("user", cardholderID)
	c.Set("status", status)
	if !until.IsZero() {
		dt, err := types.ParseDateTime(until)
		if err != nil {
			t.Fatalf("parse valid_until: %v", err)
		}
		c.Set("valid_until", dt)
	}
	if err := app.Save(c); err != nil {
		t.Fatalf("save credential %s: %v", value, err)
	}
	return c
}

func statusOf(t *testing.T, app core.App, id string) string {
	t.Helper()
	rec, err := app.FindRecordById("credentials", id)
	if err != nil {
		t.Fatalf("reload credential %s: %v", id, err)
	}
	return rec.GetString("status")
}

// TestSweepRevokesOnlyExpiredVisitorCredentials is the whole contract: expired
// visitor credentials are revoked; everything else is left strictly alone. The
// negative cases matter more than the positive one — silently revoking a staff
// credential or a still-valid pass would be a real outage.
func TestSweepRevokesOnlyExpiredVisitorCredentials(t *testing.T) {
	app := newApp(t)

	visitor := mkCardholder(t, app, "Vera Visitor", "vera@test.dev", "visitor")
	staff := mkCardholder(t, app, "Sam Staff", "sam@test.dev", "holder")
	noBadge := mkCardholder(t, app, "Nora NoBadge", "", "")

	expiredVisitor := mkCred(t, app, visitor.Id, "V-EXPIRED", "active", now.Add(-time.Hour))
	liveVisitor := mkCred(t, app, visitor.Id, "V-LIVE", "active", now.Add(time.Hour))
	unboundedVisitor := mkCred(t, app, visitor.Id, "V-UNBOUNDED", "active", time.Time{})
	alreadyRevoked := mkCred(t, app, visitor.Id, "V-ALREADYREVOKED", "revoked", now.Add(-time.Hour))
	// A staff credential past its window: an operator may be about to extend it, so
	// the sweep must not touch it.
	expiredStaff := mkCred(t, app, staff.Id, "CARD-EXPIRED-STAFF", "active", now.Add(-time.Hour))
	// A cardholder with no badge login at all is outside the sweep's scope.
	expiredNoBadge := mkCred(t, app, noBadge.Id, "CARD-EXPIRED-NOBADGE", "active", now.Add(-time.Hour))

	got := newSweeper(t, app).Sweep(now)
	if got != 1 {
		t.Errorf("Sweep revoked %d credentials, want 1 (only the expired visitor one)", got)
	}

	for _, tc := range []struct {
		label string
		id    string
		want  string
	}{
		{"expired visitor credential", expiredVisitor.Id, "revoked"},
		{"still-valid visitor credential", liveVisitor.Id, "active"},
		{"unbounded visitor credential", unboundedVisitor.Id, "active"},
		{"already-revoked visitor credential", alreadyRevoked.Id, "revoked"},
		{"expired STAFF credential", expiredStaff.Id, "active"},
		{"expired credential with no badge login", expiredNoBadge.Id, "active"},
	} {
		if got := statusOf(t, app, tc.id); got != tc.want {
			t.Errorf("%s: status = %q, want %q", tc.label, got, tc.want)
		}
	}
}

// TestSweepIsIdempotent — a second pass must find nothing left to do, or the sweep
// would rewrite the same rows (and log) every hour forever.
func TestSweepIsIdempotent(t *testing.T) {
	app := newApp(t)
	visitor := mkCardholder(t, app, "Vera Visitor", "vera@test.dev", "visitor")
	mkCred(t, app, visitor.Id, "V-EXPIRED", "active", now.Add(-time.Hour))

	s := newSweeper(t, app)
	if first := s.Sweep(now); first != 1 {
		t.Fatalf("first sweep revoked %d, want 1", first)
	}
	if second := s.Sweep(now); second != 0 {
		t.Errorf("second sweep revoked %d, want 0 (already revoked)", second)
	}
}

// TestSweepNoVisitors is the common case for an install that has never issued a
// visitor pass: no badges, no queries against credentials, no work.
func TestSweepNoVisitors(t *testing.T) {
	app := newApp(t)
	if got := newSweeper(t, app).Sweep(now); got != 0 {
		t.Errorf("Sweep revoked %d with no visitor badges, want 0", got)
	}
}
