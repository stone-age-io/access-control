package diag

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stone-age-io/access-control/config"
	"github.com/stone-age-io/access-control/internal/controller"
	"github.com/stone-age-io/access-control/internal/drivers"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/subjects"
)

type emitterStub struct{}

func (emitterStub) Emit(string, any) error { return nil }

// The real Source aggregator, wired over real (empty, unsynced) controller
// components and rendered through the real template: a fresh box reports
// not-synced, NATS-disconnected (nil conn), no bound portals, and serves the
// empty-portals banner without panicking. This covers the production
// Source.Report() + template path the fake-source tests stub out.
func TestRealSourceRendersFreshController(t *testing.T) {
	log := logger.NewNopLogger()
	store := controller.NewPolicyStore(nil, log, nil)
	rt := controller.NewRuntime("hq", store, drivers.NewMockReader(1), nil, nil,
		emitterStub{}, subjects.Default(), log, nil)

	cfg := &config.Config{}
	cfg.Controller.Code = "ctrl-hq-1"
	cfg.Controller.Location = "hq"
	cfg.Subjects.App = "acc"

	src := NewSource(cfg, store, rt, nil, time.Unix(0, 0).UTC())

	rep := src.Report()
	if rep.Policy.Synced {
		t.Errorf("synced = true on a fresh store, want false (boot default-deny)")
	}
	if rep.NATS.Connected {
		t.Errorf("nats connected = true with a nil conn, want false")
	}
	if len(rep.Portals) != 0 {
		t.Errorf("portals = %d, want 0 (none bound)", len(rep.Portals))
	}
	if rep.Identity.Controller != "ctrl-hq-1" {
		t.Errorf("identity controller = %q, want ctrl-hq-1", rep.Identity.Controller)
	}

	rec := serve(t, src, http.MethodGet, "/status")
	if rec.Code != http.StatusOK {
		t.Fatalf("/status code = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "No portals bound") {
		t.Errorf("expected the empty-portals banner on a fresh controller")
	}
}
