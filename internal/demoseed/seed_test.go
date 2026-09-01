package demoseed_test

import (
	"testing"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"

	"github.com/stone-age-io/access-control/internal/demoseed"
	"github.com/stone-age-io/access-control/internal/policy"

	// Side-effect import registers the schema + fixture migrations so the test
	// app applies them in RunAllMigrations.
	_ "github.com/stone-age-io/access-control/pbmigrations"
)

// A modest history keeps the suite quick; every property below is independent
// of how many events there are.
const testEvents = 80

// A fixed instant, so the credential-validity assertions below have a window to
// be inside or outside of rather than depending on when the suite runs.
var testNow = time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

func seedOnce(t *testing.T) core.App {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(app.Cleanup)
	if _, err := demoseed.Run(app, demoseed.Options{Events: testEvents, Now: testNow}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return app
}

// northwindSites returns the ids of the three sites this seed owns.
//
// Assertions about field VALUES have to be scoped to them. The base fixture
// migrations (1750000001 and friends) seed `hq` with a deliberately minimal
// portal and aux input — no lock_type, no point_type, because those fields were
// added by later migrations that did not backfill. That is their business; a
// test for THIS seed that failed on THOSE records would be reporting someone
// else's decision as our bug.
func northwindSites(t *testing.T, app core.App) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, code := range []string{"KC-DC1", "KC-OFFICE", "SGF-XD2"} {
		loc, err := app.FindFirstRecordByFilter("locations", "code = {:c}", dbx.Params{"c": code})
		if err != nil {
			t.Fatalf("site %q: %v", code, err)
		}
		out[loc.Id] = true
	}
	return out
}

func count(t *testing.T, app core.App, collection string) int {
	t.Helper()
	recs, err := app.FindAllRecords(collection)
	if err != nil {
		t.Fatalf("count %s: %v", collection, err)
	}
	return len(recs)
}

func TestSeedPopulatesTheWholeGraph(t *testing.T) {
	app := seedOnce(t)

	for _, c := range []struct {
		name string
		min  int
	}{
		// The base fixture (1750000001) contributes `hq` and its records, so
		// these are floors rather than exact counts.
		{"locations", 4},
		{"schedules", 6},
		{"holiday_calendars", 1},
		{"holidays", 4},
		{"controllers", 5},
		{"areas", 4},
		{"portals", 11},
		{"aux_input", 8},
		{"aux_output", 4},
		{"access_groups", 7},
		{"roles", 9},
		{"cardholders", 15},
		{"credentials", 15},
		{"events", testEvents},
	} {
		if got := count(t, app, c.name); got < c.min {
			t.Errorf("%s: got %d records, want at least %d", c.name, got, c.min)
		}
	}
}

// The site codes are the contract with the platform's demo. If these drift, the
// two demos stop describing one company — which is the entire reason this seed
// does not invent its own site names.
func TestSiteCodesMatchThePlatformDemo(t *testing.T) {
	app := seedOnce(t)

	for _, code := range []string{"KC-DC1", "KC-OFFICE", "SGF-XD2"} {
		loc, err := app.FindFirstRecordByFilter("locations", "code = {:c}", dbx.Params{"c": code})
		if err != nil || loc == nil {
			t.Fatalf("no location with code %q", code)
		}
		if loc.GetString("timezone") == "" {
			t.Errorf("location %q has no timezone; the controller resolves it per evaluation", code)
		}
		if loc.GetGeoPoint("coordinates").Lat == 0 {
			t.Errorf("location %q has no coordinates, so the location map is blank", code)
		}
	}
}

// Every location and portal code becomes a NATS subject token
// (acc.{location}.{type}.{thing}), and the mirror rejects one that cannot. A
// record that fails that check saves happily and silently never reaches a
// controller, so it is worth catching here.
func TestCodesAreValidSubjectTokens(t *testing.T) {
	app := seedOnce(t)

	for _, collection := range []string{"locations", "portals", "controllers", "areas", "aux_input", "aux_output"} {
		recs, err := app.FindAllRecords(collection)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range recs {
			code := r.GetString("code")
			if code == "" {
				t.Errorf("%s %s has no code", collection, r.Id)
				continue
			}
			for _, bad := range []string{".", " ", "\t", "*", ">"} {
				if contains(code, bad) {
					t.Errorf("%s code %q contains %q, which the mirror rejects as a subject token",
						collection, code, bad)
				}
			}
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// A portal's I/O has to fit the board its controller is. Both supported models
// are 8 relays and 8 inputs, so an over-subscribed panel is a fixture bug that
// would only surface on real hardware.
func TestPortalIOFitsTheBoardAndDoesNotCollide(t *testing.T) {
	app := seedOnce(t)

	const maxLine = 8

	type line struct {
		ctrl, kind string
		idx        int
	}
	used := map[line]string{}

	claim := func(t *testing.T, ctrl, kind string, idx int, owner string) {
		t.Helper()
		if idx < 1 || idx > maxLine {
			t.Errorf("%s: %s index %d is outside 1..%d", owner, kind, idx, maxLine)
			return
		}
		k := line{ctrl, kind, idx}
		if prev, taken := used[k]; taken {
			t.Errorf("%s and %s both claim %s %d on controller %s", owner, prev, kind, idx, ctrl)
			return
		}
		used[k] = owner
	}

	portals, err := app.FindAllRecords("portals")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range portals {
		ctrl := p.GetString("controller")
		if ctrl == "" {
			continue
		}
		owner := "portal " + p.GetString("code")
		claim(t, ctrl, "relay", p.GetInt("lock_relay"), owner)
		claim(t, ctrl, "input", p.GetInt("dps_input"), owner)
		claim(t, ctrl, "input", p.GetInt("rex_input"), owner)
	}

	inputs, err := app.FindAllRecords("aux_input")
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range inputs {
		if ctrl := in.GetString("controller"); ctrl != "" {
			claim(t, ctrl, "input", in.GetInt("input_index"), "aux input "+in.GetString("code"))
		}
	}

	outputs, err := app.FindAllRecords("aux_output")
	if err != nil {
		t.Fatal(err)
	}
	for _, out := range outputs {
		if ctrl := out.GetString("controller"); ctrl != "" {
			claim(t, ctrl, "relay", out.GetInt("relay_index"), "aux output "+out.GetString("code"))
		}
	}
}

// Aux inputs must carry a point_type, and the three kinds must all appear.
//
// This test exists because its absence let a real bug through: the seeder set
// `kind` where the schema calls the field `point_type`, PocketBase discarded the
// write without an error, and every assertion still passed — the records were
// there, just untyped. An untyped point is not merely cosmetic: a `monitor`
// point raises nothing, so a glassbreak that silently became one is a detector
// that never alarms. Assert the VALUES landed, not that the rows exist.
func TestAuxInputsAreTypedAndCoverEveryPointKind(t *testing.T) {
	app := seedOnce(t)

	sites := northwindSites(t, app)
	inputs, err := app.FindAllRecords("aux_input")
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int{}
	for _, in := range inputs {
		if !sites[in.GetString("location")] {
			continue
		}
		pt := in.GetString("point_type")
		if pt == "" {
			t.Errorf("aux input %q has no point_type; it would raise nothing", in.GetString("code"))
			continue
		}
		kinds[pt]++
		if in.GetString("contact") == "" {
			t.Errorf("aux input %q has no contact sense", in.GetString("code"))
		}
		// An intrusion point is only meaningful inside an area — that is what
		// decides whether it is armed. A tamper_24h point alarms regardless.
		if pt == "intrusion" && in.GetString("area") == "" {
			t.Errorf("intrusion point %q belongs to no area, so nothing arms it", in.GetString("code"))
		}
	}
	for _, want := range []string{"monitor", "intrusion", "tamper_24h"} {
		if kinds[want] == 0 {
			t.Errorf("no aux input of point_type %q was seeded", want)
		}
	}
}

// Same class of failure, one level up: a portal whose area, posture or lock type
// silently failed to write looks complete in the list and behaves differently on
// the door.
func TestPortalsCarryTheFieldsTheControllerActsOn(t *testing.T) {
	app := seedOnce(t)

	sites := northwindSites(t, app)
	portals, err := app.FindAllRecords("portals")
	if err != nil {
		t.Fatal(err)
	}
	var withArea, unlockedOnSchedule, disarmOnGrant, maglocks int
	for _, p := range portals {
		if !sites[p.GetString("location")] {
			continue
		}
		code := p.GetString("code")
		if p.GetString("posture") == "" {
			t.Errorf("portal %q has no posture", code)
		}
		if p.GetString("lock_type") == "" {
			t.Errorf("portal %q has no lock type", code)
		}
		if p.GetInt("lock_relay") == 0 {
			t.Errorf("portal %q has no lock relay, so nothing pulses", code)
		}
		if p.GetString("area") != "" {
			withArea++
		}
		if p.GetString("auto_posture") != "" && p.GetString("auto_schedule") != "" {
			unlockedOnSchedule++
		}
		if p.GetBool("disarm_on_grant") {
			disarmOnGrant++
		}
		if p.GetString("lock_type") == "maglock" {
			maglocks++
		}
	}

	if withArea == 0 {
		t.Error("no portal belongs to an area, so a forced open can never raise an intrusion")
	}
	if unlockedOnSchedule == 0 {
		t.Error("no portal has a scheduled posture, so allow_posture_unlocked is unreachable")
	}
	if disarmOnGrant == 0 {
		t.Error("no portal is disarm_on_grant, so the entry-disarm sink has nothing to act on")
	}
	if maglocks == 0 {
		t.Error("every lock is a strike; the maglock case is unrepresented")
	}
}

// The arm-rights asymmetry is the thing this demo exists to show: the cleaning
// crew can arm the warehouse and cannot disarm it. If a well-meaning edit gives
// ag-nw-cleaning both rights, the demo still runs and stops teaching anything.
func TestCleaningCrewCanArmButNotDisarm(t *testing.T) {
	app := seedOnce(t)

	g, err := app.FindFirstRecordByFilter("access_groups", "code = {:c}", dbx.Params{"c": "ag-nw-cleaning"})
	if err != nil {
		t.Fatalf("ag-nw-cleaning: %v", err)
	}
	rights := g.GetStringSlice("area_rights")
	if len(rights) != 1 || rights[0] != policy.ArmActionArm {
		t.Errorf("ag-nw-cleaning area_rights = %v, want exactly [%q]", rights, policy.ArmActionArm)
	}
	if len(g.GetStringSlice("areas")) == 0 {
		t.Error("ag-nw-cleaning grants no areas, so the arm-only right is unreachable")
	}

	warehouse, err := app.FindFirstRecordByFilter("access_groups", "code = {:c}", dbx.Params{"c": "ag-nw-warehouse"})
	if err != nil {
		t.Fatalf("ag-nw-warehouse: %v", err)
	}
	if len(warehouse.GetStringSlice("area_rights")) != 2 {
		t.Errorf("ag-nw-warehouse should hold both rights, got %v", warehouse.GetStringSlice("area_rights"))
	}
}

// A group with areas and empty rights reports deny_no_area_right, which is a
// misconfiguration rather than a decision. The fixtures must not contain one.
func TestNoGroupHasAreasWithoutRights(t *testing.T) {
	app := seedOnce(t)

	groups, err := app.FindAllRecords("access_groups")
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups {
		if len(g.GetStringSlice("areas")) > 0 && len(g.GetStringSlice("area_rights")) == 0 {
			t.Errorf("access group %q grants areas with no rights — every arm would report %s",
				g.GetString("code"), policy.ReasonDenyNoAreaRight)
		}
	}
}

// Two people who look identical at a reader and are not: a suspended PERSON with
// a good card, and an active person with a revoked CARD. Both are needed for the
// events history to show the same reason code arising two ways.
func TestBothHalvesOfTheDenyLadderAreRepresented(t *testing.T) {
	app := seedOnce(t)

	suspended, err := app.FindFirstRecordByFilter("cardholders", "external_id = {:x}", dbx.Params{"x": "nw-glen"})
	if err != nil {
		t.Fatalf("nw-glen: %v", err)
	}
	if suspended.GetString("status") != "suspended" {
		t.Error("nw-glen should be suspended (a person-level denial)")
	}
	glenCred, err := app.FindFirstRecordByFilter("credentials", "user = {:u}", dbx.Params{"u": suspended.Id})
	if err != nil {
		t.Fatalf("nw-glen credential: %v", err)
	}
	if glenCred.GetString("status") != "active" {
		t.Error("nw-glen's card should be ACTIVE — the denial has to come from the person")
	}

	active, err := app.FindFirstRecordByFilter("cardholders", "external_id = {:x}", dbx.Params{"x": "nw-brett"})
	if err != nil {
		t.Fatalf("nw-brett: %v", err)
	}
	if active.GetString("status") != "active" {
		t.Error("nw-brett should be active — the denial has to come from the card")
	}
	brettCred, err := app.FindFirstRecordByFilter("credentials", "user = {:u}", dbx.Params{"u": active.Id})
	if err != nil {
		t.Fatalf("nw-brett credential: %v", err)
	}
	if brettCred.GetString("status") != "revoked" {
		t.Error("nw-brett's card should be revoked")
	}
}

// A cardholder with no email cannot sign in by any method. That is the intended
// state for a card that lives in a drawer, and it is only true if badge_login
// and password_set stay off.
func TestDrawerCardsCannotSignIn(t *testing.T) {
	app := seedOnce(t)

	for _, id := range []string{"nw-spare-dock", "nw-fire-lockbox"} {
		ch, err := app.FindFirstRecordByFilter("cardholders", "external_id = {:x}", dbx.Params{"x": id})
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if ch.GetString("email") != "" {
			t.Errorf("%s has an email; it is meant to be unable to sign in", id)
		}
		if ch.GetBool("badge_login") {
			t.Errorf("%s has badge_login set", id)
		}
	}
}

// The three visitor states — live, expired, revoked — are what makes
// "troubleshoot a badge" a real exercise rather than a happy path.
func TestVisitorPassesCoverLiveExpiredAndRevoked(t *testing.T) {
	app := seedOnce(t)

	check := func(externalID string, wantLive bool, wantCredStatus string) {
		t.Helper()
		ch, err := app.FindFirstRecordByFilter("cardholders", "external_id = {:x}", dbx.Params{"x": externalID})
		if err != nil {
			t.Fatalf("%s: %v", externalID, err)
		}
		if ch.GetString("kind") != "visitor" {
			t.Errorf("%s kind = %q, want visitor", externalID, ch.GetString("kind"))
		}
		cred, err := app.FindFirstRecordByFilter("credentials", "user = {:u}", dbx.Params{"u": ch.Id})
		if err != nil {
			t.Fatalf("%s credential: %v", externalID, err)
		}
		if got := cred.GetString("status"); got != wantCredStatus {
			t.Errorf("%s credential status = %q, want %q", externalID, got, wantCredStatus)
		}
		until := cred.GetDateTime("valid_until").Time()
		if until.IsZero() {
			t.Fatalf("%s has no valid_until; a visitor pass must be bounded", externalID)
		}
		if live := until.After(testNow); live != wantLive {
			t.Errorf("%s valid_until = %s, want live=%v as of %s", externalID, until, wantLive, testNow)
		}
	}

	check("nw-visit-live", true, "active")
	check("nw-visit-expired", false, "active") // expired by DATE, not by revocation
	check("nw-visit-revoked", true, "revoked") // still in date, killed by status
}

// An events list of nothing but grants shows a working system and teaches
// nothing. Every reason a reader will meet should be in the history.
func TestEventHistoryCoversAllowsAndEveryDenialCause(t *testing.T) {
	app := seedOnce(t)

	events, err := app.FindAllRecords("events")
	if err != nil {
		t.Fatal(err)
	}
	reasons := map[string]int{}
	kinds := map[string]int{}
	var allows, denies, openAlarms int
	for _, e := range events {
		reasons[e.GetString("reason")]++
		kinds[e.GetString("kind")]++
		if e.GetString("kind") == "alarm" && !e.GetBool("acknowledged") {
			openAlarms++
		}
		if e.GetString("kind") != "tap" {
			continue
		}
		if e.GetBool("allow") {
			allows++
		} else {
			denies++
		}
	}

	if allows == 0 || denies == 0 {
		t.Errorf("history has %d allows and %d denies; both are needed", allows, denies)
	}
	for _, want := range []string{
		policy.ReasonAllowGrant,
		policy.ReasonAllowPostureUnlocked,
		policy.ReasonAllowCommandGrant,
		policy.ReasonDenyScheduleClosed,
		policy.ReasonDenyNoAccess,
		policy.ReasonDenyRevoked,
		policy.ReasonDenyExpired,
		policy.ReasonDenyUnknownCredential,
	} {
		if reasons[want] == 0 {
			t.Errorf("no event carries reason %q", want)
		}
	}
	if kinds["alarm"] == 0 {
		t.Error("no alarm events, so the Alarm Console is empty")
	}
	if openAlarms == 0 {
		t.Error("every alarm is acknowledged, so the Alarm Console has nothing to act on")
	}
}

// Every event names a portal and location that actually exist. A dangling name
// renders as a row pointing at nothing.
func TestEventsReferenceRealPortals(t *testing.T) {
	app := seedOnce(t)

	known := map[string]bool{}
	portals, err := app.FindAllRecords("portals")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range portals {
		known[p.GetString("code")] = true
	}

	events, err := app.FindAllRecords("events")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		code := e.GetString("portal")
		if code != "" && !known[code] {
			t.Errorf("event references unknown portal %q", code)
		}
	}
}

// Re-running must converge, not duplicate.
func TestSeedingTwiceChangesNothing(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(app.Cleanup)

	if _, err := demoseed.Run(app, demoseed.Options{Events: testEvents, Now: testNow}); err != nil {
		t.Fatalf("first run: %v", err)
	}
	before := map[string]int{}
	for _, c := range []string{"locations", "portals", "areas", "aux_input", "aux_output",
		"access_groups", "roles", "cardholders", "credentials", "events"} {
		before[c] = count(t, app, c)
	}

	second, err := demoseed.Run(app, demoseed.Options{Events: testEvents, Now: testNow})
	if err != nil {
		t.Fatalf("second run: %v", err)
	}

	for c, n := range before {
		if got := count(t, app, c); got != n {
			t.Errorf("%s: %d records after one run, %d after two", c, n, got)
		}
	}
	if len(second.Created) != 0 {
		t.Errorf("second run created %v, want nothing", second.Created)
	}
}
