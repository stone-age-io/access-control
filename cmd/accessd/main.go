// Command accessd is the central stone-access app: the system of record
// (embedded PocketBase), the KV mirror publisher (policy graph → NATS KV, one
// key per record), and the JetStream audit consumer (events → PocketBase).
//
// It is driven by PocketBase's CLI (e.g. `accessd serve`). Our SA_-prefixed
// config (NATS, policy bucket, audit stream, logging) is loaded from the file
// at $SA_CONFIG (default config/accessd.yaml) plus environment overrides.
//
// This build step boots PocketBase with the schema migrations and fixture and
// brings up the NATS connection (ensuring the policy KV bucket and audit
// stream) in the serve lifecycle. The mirror publisher and audit consumer are
// wired in later build steps.
package main

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/plugins/migratecmd"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stone-age-io/access-control/config"
	"github.com/stone-age-io/access-control/internal/audit"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/metrics"
	"github.com/stone-age-io/access-control/internal/mirror"
	"github.com/stone-age-io/access-control/internal/natsx"

	// Side-effect import: registers the schema + fixture migrations.
	_ "github.com/stone-age-io/access-control/pbmigrations"
)

func main() {
	boot := logger.NewBootstrapLogger()

	cfg, err := config.Load(configPath())
	if err != nil {
		boot.Fatal("failed to load config", "error", err)
	}

	log, err := logger.NewLogger(&cfg.Logging)
	if err != nil {
		boot.Fatal("failed to create logger", "error", err)
	}
	defer func() { _ = log.Sync() }()
	log = log.With("app", "accessd")

	m, err := metrics.NewMetrics(prometheus.NewRegistry())
	if err != nil {
		log.Fatal("failed to create metrics", "error", err)
	}

	pb := pocketbase.NewWithConfig(pocketbase.Config{
		DefaultDataDir: cfg.Accessd.DataDir,
	})

	// migratecmd exposes `accessd migrate ...` and, with Automigrate, snapshots
	// dashboard collection edits into Go files beside our hand-authored ones.
	migratecmd.MustRegister(pb, pb.RootCmd, migratecmd.Config{
		Dir:          "pbmigrations",
		Automigrate:  true,
		TemplateLang: migratecmd.TemplateLangGo,
	})

	// Resources brought up only when actually serving (not for migrate/superuser).
	var (
		nc         *natsx.Conn
		metricsSrv *http.Server
		collector  *metrics.Collector
		auditC     *audit.Consumer
	)

	pb.OnServe().BindFunc(func(e *core.ServeEvent) error {
		updateInterval, _ := time.ParseDuration(cfg.Metrics.UpdateInterval)
		collector = metrics.NewCollector(m, updateInterval)
		collector.Start()

		if cfg.Metrics.Enabled {
			metricsSrv = m.NewServer(cfg.Metrics.Address, cfg.Metrics.Path)
			go func() {
				log.Info("metrics server listening", "address", cfg.Metrics.Address, "path", cfg.Metrics.Path)
				if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					log.Error("metrics server error", "error", err)
				}
			}()
		}

		nc, err = natsx.Connect(&cfg.NATS, log, m, nil)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		kv, err := nc.EnsureKVBucket(ctx, cfg.Policy.Bucket)
		if err != nil {
			return err
		}
		if _, err := nc.EnsureStream(ctx, cfg.Events.Stream, cfg.Events.Subjects); err != nil {
			return err
		}

		// KV mirror publisher: PocketBase record changes → ACC_POLICY keys.
		// Register the hooks first, then reconcile existing records (migrations
		// seed data before the hooks bind) and prune stale keys.
		pub := mirror.Register(e.App, kv, log, m)
		if err := pub.SyncAll(ctx, e.App); err != nil {
			return err
		}

		// Audit consumer: ACC_EVENTS JetStream → events collection (UI timeline).
		auditC = audit.New(e.App, nc.JS, cfg.Events.Stream, log, m)
		if err := auditC.Start(ctx); err != nil {
			return err
		}

		log.Info("accessd serving",
			"policyBucket", cfg.Policy.Bucket,
			"eventsStream", cfg.Events.Stream,
			"dataDir", cfg.Accessd.DataDir)
		return e.Next()
	})

	pb.OnTerminate().BindFunc(func(e *core.TerminateEvent) error {
		log.Info("accessd terminating")
		if auditC != nil {
			auditC.Stop()
		}
		if collector != nil {
			collector.Stop()
		}
		if metricsSrv != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = metricsSrv.Shutdown(ctx)
			cancel()
		}
		if nc != nil {
			_ = nc.Close()
		}
		return e.Next()
	})

	if err := pb.Start(); err != nil {
		log.Fatal("pocketbase exited with error", "error", err)
	}
}

// configPath returns the SA_CONFIG path, defaulting to config/accessd.yaml.
// A missing file is tolerated by config.Load (defaults + env still apply).
func configPath() string {
	if p := os.Getenv("SA_CONFIG"); p != "" {
		return p
	}
	return "config/accessd.yaml"
}
