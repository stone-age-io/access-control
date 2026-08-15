package status

import (
	"encoding/json"
	"testing"

	"github.com/pocketbase/pocketbase/tests"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/statuskv"
	"github.com/stone-age-io/access-control/internal/subjects"

	// Side-effect import registers the schema (point_status collection).
	_ "github.com/stone-age-io/access-control/pbmigrations"
)

// capturedEvent is one arm-transition event recorded by the test publisher.
type capturedEvent struct {
	subject string
	body    map[string]any
}

// applyProjector builds a projector over a test app with a recording publisher,
// so apply()'s transition detection can be driven directly (no NATS, no watcher).
func applyProjector(t *testing.T) (*Projector, *[]capturedEvent) {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(app.Cleanup)

	p := New(app, nil, nil, subjects.Default(), logger.NewNopLogger(), nil)
	got := &[]capturedEvent{}
	p.publish = func(subject string, data []byte) error {
		var body map[string]any
		_ = json.Unmarshal(data, &body)
		*got = append(*got, capturedEvent{subject: subject, body: body})
		return nil
	}
	return p, got
}

const zone1Key = statuskv.PrefixArea + "ctrl-hq-1.zone1"

func areaShadow(arm, source string) []byte {
	return []byte(`{"code":"zone1","location":"hq","controller":"ctrl-hq-1","arm":"` + arm +
		`","source":"` + source + `","peers":["ctrl-hq-1"],"updatedAt":"2026-01-05T18:00:00Z"}`)
}

// The first report of an area creates its row but is NOT a transition — otherwise
// a cold boot would manufacture one event per area.
func TestArmFirstReportIsNotATransition(t *testing.T) {
	p, got := applyProjector(t)
	p.apply(zone1Key, areaShadow("disarmed", "standing"))

	if len(*got) != 0 {
		t.Errorf("arm events = %d, want 0 (first report)", len(*got))
	}
}

// A changed arm state emits exactly one event, naming the controller whose shadow
// moved and the provenance of the new state.
func TestArmTransitionEmits(t *testing.T) {
	p, got := applyProjector(t)
	p.apply(zone1Key, areaShadow("disarmed", "standing"))
	p.apply(zone1Key, areaShadow("armed", "scheduled"))

	if len(*got) != 1 {
		t.Fatalf("arm events = %d, want 1", len(*got))
	}
	ev := (*got)[0]
	if want := "acc.hq.area.zone1.evt.state"; ev.subject != want {
		t.Errorf("subject = %q, want %q", ev.subject, want)
	}
	if ev.body["arm"] != "armed" || ev.body["previous"] != "disarmed" {
		t.Errorf("arm/previous = %v/%v, want armed/disarmed", ev.body["arm"], ev.body["previous"])
	}
	if ev.body["controller"] != "ctrl-hq-1" || ev.body["source"] != "scheduled" {
		t.Errorf("controller/source = %v/%v, want ctrl-hq-1/scheduled", ev.body["controller"], ev.body["source"])
	}
}

// A re-delivered identical shadow (every WatchAll re-sync replays every key) is not
// a transition and must not emit.
func TestArmUnchangedDoesNotEmit(t *testing.T) {
	p, got := applyProjector(t)
	p.apply(zone1Key, areaShadow("armed", "standing"))
	p.apply(zone1Key, areaShadow("armed", "standing"))
	p.apply(zone1Key, areaShadow("armed", "standing"))

	if len(*got) != 0 {
		t.Errorf("arm events = %d, want 0 (no state change)", len(*got))
	}
}

// Only areas emit transitions. A portal's door open/close is high-volume state that
// stays in the shadow; promoting it to an event is a separate, opt-in decision.
func TestPortalStateDoesNotEmit(t *testing.T) {
	p, got := applyProjector(t)
	closed := []byte(`{"code":"lobby-main","location":"hq","controller":"ctrl-hq-1","door":"closed","posture":"secure","source":"standing","updatedAt":"2026-01-05T09:00:00Z"}`)
	open := []byte(`{"code":"lobby-main","location":"hq","controller":"ctrl-hq-1","door":"open","posture":"secure","source":"standing","updatedAt":"2026-01-05T09:01:00Z"}`)
	p.apply(statuskv.PrefixPortal+"lobby-main", closed)
	p.apply(statuskv.PrefixPortal+"lobby-main", open)

	if len(*got) != 0 {
		t.Errorf("events = %d, want 0 (portal state is shadow-only)", len(*got))
	}
}

func TestRowForPortal(t *testing.T) {
	val := []byte(`{"code":"lobby-main","location":"hq","controller":"ctrl-hq-1","door":"open","posture":"unlocked","source":"scheduled","held":true,"updatedAt":"2026-01-05T09:00:00Z"}`)
	r, ok := rowFor(statuskv.PrefixPortal+"lobby-main", val)
	if !ok {
		t.Fatal("rowFor portal returned ok=false")
	}
	if r.key != "portal.lobby-main" || r.code != "lobby-main" || r.kind != statuskv.KindPortal {
		t.Errorf("identity = %q/%q/%q", r.key, r.code, r.kind)
	}
	if r.state != "open" || r.posture != "unlocked" || r.postureSource != "scheduled" || !r.held {
		t.Errorf("state=%q posture=%q source=%q held=%v", r.state, r.posture, r.postureSource, r.held)
	}
	if r.controller != "ctrl-hq-1" || r.location != "hq" || r.changed != "2026-01-05T09:00:00Z" {
		t.Errorf("controller=%q location=%q changed=%q", r.controller, r.location, r.changed)
	}
	if r.payload["door"] != "open" {
		t.Errorf("payload not preserved: %v", r.payload)
	}
}

func TestRowForAuxOutput(t *testing.T) {
	val := []byte(`{"code":"gate-1","location":"hq","controller":"ctrl-hq-1","energized":true,"updatedAt":"2026-01-05T09:00:00Z"}`)
	r, ok := rowFor(statuskv.PrefixAuxOut+"gate-1", val)
	if !ok || r.kind != statuskv.KindAuxOutput || r.state != "energized" {
		t.Fatalf("aux output row = %+v (ok=%v)", r, ok)
	}
}

func TestRowForAuxInput(t *testing.T) {
	val := []byte(`{"code":"dock","location":"hq","controller":"ctrl-hq-1","active":false,"updatedAt":"2026-01-05T09:00:00Z"}`)
	r, ok := rowFor(statuskv.PrefixAuxIn+"dock", val)
	if !ok || r.kind != statuskv.KindAuxInput || r.state != "inactive" {
		t.Fatalf("aux input row = %+v (ok=%v)", r, ok)
	}
}

// An area shadow has a compound key (area.<controller>.<code>); rowFor takes the
// bare code/controller from the value, and peers/source ride the payload.
func TestRowForArea(t *testing.T) {
	val := []byte(`{"code":"zone1","location":"hq","controller":"ctrl-hq-1","arm":"armed","source":"override","peers":["ctrl-hq-1","ctrl-hq-2"],"updatedAt":"2026-01-05T09:00:00Z"}`)
	r, ok := rowFor(statuskv.PrefixArea+"ctrl-hq-1.zone1", val)
	if !ok {
		t.Fatal("rowFor area returned ok=false")
	}
	if r.kind != statuskv.KindArea || r.code != "zone1" || r.controller != "ctrl-hq-1" {
		t.Errorf("identity = kind=%q code=%q controller=%q, want area/zone1/ctrl-hq-1", r.kind, r.code, r.controller)
	}
	if r.state != "armed" || r.location != "hq" {
		t.Errorf("state=%q location=%q, want armed/hq", r.state, r.location)
	}
	peers, _ := r.payload["peers"].([]any)
	if len(peers) != 2 {
		t.Errorf("payload peers = %v, want 2 entries", r.payload["peers"])
	}
}

func TestRowForUnknownPrefix(t *testing.T) {
	if _, ok := rowFor("widget.foo", []byte(`{}`)); ok {
		t.Error("unknown prefix should not produce a row")
	}
}

func TestRowForMalformed(t *testing.T) {
	if _, ok := rowFor(statuskv.PrefixPortal+"x", []byte(`not json`)); ok {
		t.Error("malformed value should not produce a row")
	}
}
