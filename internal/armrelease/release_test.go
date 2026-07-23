package armrelease

import (
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/stone-age-io/access-control/internal/logger"

	// Side-effect import applies the schema + fixture (incl. the hq location).
	_ "github.com/stone-age-io/access-control/pbmigrations"
)

func newApp(t *testing.T) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(app.Cleanup)
	return app
}

// seedArea creates an area with the given code and arm_override, returning its id.
func seedArea(t *testing.T, app core.App, code, override string) string {
	t.Helper()
	loc, err := app.FindFirstRecordByData("locations", "code", "hq")
	if err != nil {
		t.Fatalf("hq location: %v", err)
	}
	col, err := app.FindCollectionByNameOrId("areas")
	if err != nil {
		t.Fatalf("areas collection: %v", err)
	}
	rec := core.NewRecord(col)
	rec.Set("code", code)
	rec.Set("location", loc.Id)
	rec.Set("arm", "disarmed")
	rec.Set("arm_override", override)
	if err := app.Save(rec); err != nil {
		t.Fatalf("seed area %q: %v", code, err)
	}
	return rec.Id
}

// fakeResolver stands in for a policysnapshot: it returns a canned release decision per
// area code, so releaseStale can be exercised without NATS/KV.
type fakeResolver struct{ release map[string]bool }

func (f fakeResolver) ShouldReleaseDisarm(code string, _ time.Time) bool { return f.release[code] }

// releaseStale clears the override only on disarm-overridden areas the resolver says to
// release; it leaves other disarm overrides and any armed override untouched.
func TestReleaseStale(t *testing.T) {
	app := newApp(t)

	releaseID := seedArea(t, app, "vault", "disarmed")  // resolver: release
	keepID := seedArea(t, app, "closet", "disarmed")    // resolver: keep (base still armed / no schedule)
	armedID := seedArea(t, app, "lobby-area", "armed")  // not a disarm override: never touched

	resolver := fakeResolver{release: map[string]bool{"vault": true, "closet": false}}
	releaseStale(app, resolver, time.Now().UTC(), logger.NewNopLogger())

	cases := []struct {
		name, id, want string
	}{
		{"released area cleared", releaseID, ""},
		{"kept area unchanged", keepID, "disarmed"},
		{"armed override untouched", armedID, "armed"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec, err := app.FindRecordById("areas", c.id)
			if err != nil {
				t.Fatalf("reload area: %v", err)
			}
			if got := rec.GetString("arm_override"); got != c.want {
				t.Errorf("arm_override = %q, want %q", got, c.want)
			}
		})
	}
}
