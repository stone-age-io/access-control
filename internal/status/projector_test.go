package status

import (
	"testing"

	"github.com/stone-age-io/access-control/internal/statuskv"
)

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
