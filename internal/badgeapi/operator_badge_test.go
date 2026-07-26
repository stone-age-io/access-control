package badgeapi

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/stone-age-io/access-control/internal/authz"
)

// One human can hold an account in both tiers. These cover the one place the tiers
// meet — resolving an operator to their OWN cardholder — and the boundary that keeps it
// from becoming a second way to act.

// mkOperator saves a users record with the given capabilities.
func mkOperator(t *testing.T, app core.App, email string, caps ...string) *core.Record {
	t.Helper()
	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("users collection: %v", err)
	}
	op := core.NewRecord(users)
	op.SetEmail(email)
	op.SetPassword("operator-password")
	if len(caps) > 0 {
		op.Set("permissions", caps)
	}
	if err := app.Save(op); err != nil {
		t.Fatalf("save operator %q: %v", email, err)
	}
	return op
}

// authEvent fakes the minimum of a RequestEvent that subjectCardholder reads.
func authEvent(auth *core.Record) *core.RequestEvent {
	e := &core.RequestEvent{}
	e.Auth = auth
	return e
}

func TestSubjectCardholderResolvesBothTiers(t *testing.T) {
	app := newGuardedApp(t)
	h := testHandler(app)

	op := mkOperator(t, app, "guard@test.dev", "command")
	holder := mkCardholder(t, app, "Sam Guard", "sam@test.dev", map[string]any{
		"badge_login": true, "operator": op.Id,
	})

	// A badge token is its own subject — the holder IS the cardholder.
	got, ok := h.subjectCardholder(authEvent(holder))
	if !ok || got.Id != holder.Id {
		t.Errorf("badge token resolved to %v (ok=%v), want the record itself", got, ok)
	}

	// An operator token resolves through cardholders.operator.
	got, ok = h.subjectCardholder(authEvent(op))
	if !ok {
		t.Fatal("operator token resolved to no cardholder despite a link")
	}
	if got.Id != holder.Id {
		t.Errorf("operator resolved to %q, want the linked cardholder %q", got.Id, holder.Id)
	}
}

// An operator with no linked cardholder has no badge. That must be a clean "nothing
// here", never a fallback to some other person's record.
func TestSubjectCardholderUnlinkedOperatorHasNoBadge(t *testing.T) {
	app := newGuardedApp(t)
	h := testHandler(app)

	// A cardholder exists, linked to nobody — the tempting wrong answer.
	mkCardholder(t, app, "Someone Else", "else@test.dev", map[string]any{"badge_login": true})
	op := mkOperator(t, app, "unlinked@test.dev", "policy")

	if got, ok := h.subjectCardholder(authEvent(op)); ok {
		t.Errorf("unlinked operator resolved to %q, want no badge", got.Id)
	}
}

// A nil auth resolves to nothing rather than panicking. The route binding makes this
// unreachable; the check is what keeps it unreachable.
func TestSubjectCardholderNilAuth(t *testing.T) {
	app := newGuardedApp(t)
	h := testHandler(app)
	if _, ok := h.subjectCardholder(authEvent(nil)); ok {
		t.Error("nil auth resolved to a cardholder")
	}
}

// Two cardholders must not claim one operator: without the unique index, an `enroll`
// holder could point a second person at an operator and make "my badge" ambiguous.
func TestOneOperatorLinksToOneCardholder(t *testing.T) {
	app := newGuardedApp(t)
	op := mkOperator(t, app, "shared@test.dev", "enroll")

	mkCardholder(t, app, "First Claim", "first@test.dev", map[string]any{"operator": op.Id})

	col, err := app.FindCollectionByNameOrId("cardholders")
	if err != nil {
		t.Fatalf("cardholders collection: %v", err)
	}
	second := core.NewRecord(col)
	second.Set("name", "Second Claim")
	second.SetEmail("second@test.dev")
	second.Set("status", "active")
	second.Set("operator", op.Id)
	if err := app.Save(second); err == nil {
		t.Error("a second cardholder claimed the same operator; want the unique index to refuse it")
	}

	// But TWO cardholders with NO operator must coexist — the index has to be partial,
	// or the overwhelming majority of people (who have no console account) would collide.
	mkCardholder(t, app, "No Console A", "nca@test.dev", nil)
	mkCardholder(t, app, "No Console B", "ncb@test.dev", nil)
}

// The operator tier may READ a badge and nothing more. This pins the collection list
// that decides it, so widening the badge tier's reach has to be a deliberate edit that
// breaks a test rather than a quiet addition.
func TestOnlyReadIsSharedWithTheOperatorTier(t *testing.T) {
	want := map[string]bool{BadgeCollection: false}
	for _, c := range authz.OperatorCollections {
		want[c] = false
	}
	for _, c := range meCollections {
		if _, known := want[c]; !known {
			t.Errorf("meCollections admits an unexpected collection %q", c)
			continue
		}
		want[c] = true
	}
	for c, present := range want {
		if !present {
			t.Errorf("meCollections is missing %q", c)
		}
	}
	// The break-glass superuser must be in there: PocketBase's requireAuth is plain
	// collection-name membership with no superuser exemption, so omitting it would lock
	// the account out of a route its own operators can use.
	if !containsString(meCollections, core.CollectionNameSuperusers) {
		t.Error("meCollections excludes superusers")
	}
}

func containsString(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}
