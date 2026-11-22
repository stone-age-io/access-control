package main

import (
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tests"
	"github.com/nats-io/nats.go"
	"github.com/stone-age-io/kit/health"
	"github.com/stone-age-io/kit/health/healthtest"

	_ "github.com/stone-age-io/access-control/pbmigrations"
)

// accessd's readiness on a migrated database with no NATS connection: the
// database checks pass, NATS fails, and the answer follows the shared wire
// format before and after the first probe.
func TestReadinessConforms(t *testing.T) {
	// A fresh data directory, not PocketBase's bundled test data, whose
	// migrations no binary registers.
	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Cleanup)

	var reg health.Registry
	registerReadiness(&reg, app, func() *nats.Conn { return nil })
	p := health.NewProber(&reg, "v0.0.0-test", time.Hour, 5*time.Second)
	healthtest.Conform(t, p.Handler())
	p.Start(t.Context())
	healthtest.Conform(t, p.Handler())

	rep := p.Snapshot()
	if rep.Ready {
		t.Error("ready with no NATS connection")
	}
	want := map[string]health.State{
		"database":       health.StateOK,
		"schema":         health.StateOK,
		"schema_version": health.StateOK,
		"nats":           health.StateFail,
	}
	for _, c := range rep.Checks {
		if c.State != want[c.Name] {
			t.Errorf("%s = %s (%s), want %s", c.Name, c.State, c.Detail, want[c.Name])
		}
	}
}
