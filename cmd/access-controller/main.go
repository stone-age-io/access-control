// Command access-controller is the edge stone-access app: it watches the policy
// KV keyspace into in-memory maps, decides credential presentations locally
// with the pure policy.Decide function, drives the reader/lock hardware (mocked
// in v1), and emits access events to JetStream.
//
// v1 drivers are mocks: taps are simulated by publishing to
// acc.tap.{site}.{point} and the lock just logs its pulse. The command handler
// and FAI suppression arrive in step 7.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stone-age-io/access-control/config"
	"github.com/stone-age-io/access-control/internal/controller"
	"github.com/stone-age-io/access-control/internal/drivers"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/metrics"
	"github.com/stone-age-io/access-control/internal/natsx"
)

func main() {
	configPath := flag.String("config", "config/controller.yaml", "path to config file")
	flag.Parse()

	boot := logger.NewBootstrapLogger()

	cfg, err := config.Load(*configPath)
	if err != nil {
		boot.Fatal("failed to load config", "error", err)
	}

	log, err := logger.NewLogger(&cfg.Logging)
	if err != nil {
		boot.Fatal("failed to create logger", "error", err)
	}
	defer func() { _ = log.Sync() }()
	log = log.With("app", "access-controller", "site", cfg.Controller.Site)

	m, err := metrics.NewMetrics(prometheus.NewRegistry())
	if err != nil {
		log.Fatal("failed to create metrics", "error", err)
	}

	updateInterval, _ := time.ParseDuration(cfg.Metrics.UpdateInterval)
	collector := metrics.NewCollector(m, updateInterval)
	collector.Start()
	defer collector.Stop()

	var metricsSrv *http.Server
	if cfg.Metrics.Enabled {
		metricsSrv = m.NewServer(cfg.Metrics.Address, cfg.Metrics.Path)
		go func() {
			log.Info("metrics server listening", "address", cfg.Metrics.Address, "path", cfg.Metrics.Path)
			if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Error("metrics server error", "error", err)
			}
		}()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// resync is wired to the policy store after it exists; a reconnect before
	// then is a harmless no-op.
	var resync func()
	nc, err := natsx.Connect(&cfg.NATS, log, m, func() {
		if resync != nil {
			resync()
		}
	})
	if err != nil {
		log.Fatal("failed to connect to NATS", "error", err)
	}
	defer func() { _ = nc.Close() }()

	kv, err := nc.EnsureKVBucket(ctx, cfg.Policy.Bucket)
	if err != nil {
		log.Fatal("failed to ensure policy KV bucket", "error", err)
	}

	// PolicyStore watches ACC_POLICY into in-memory maps and decides locally.
	store := controller.NewPolicyStore(kv, log, m)
	resync = store.Resync
	if err := store.Watch(ctx); err != nil {
		log.Fatal("failed to start policy watcher", "error", err)
	}
	defer store.Stop()

	// Default-deny until the initial sync completes; bound it so a broken link
	// surfaces as an error instead of hanging forever.
	readyCtx, cancelReady := context.WithTimeout(ctx, 30*time.Second)
	err = store.WaitReady(readyCtx)
	cancelReady()
	if err != nil {
		log.Fatal("policy initial sync did not complete", "error", err)
	}
	log.Info("policy synced; controller ready",
		"policyBucket", cfg.Policy.Bucket,
		"points", cfg.Controller.Points)

	// Tap loop with mock drivers. Taps are simulated by publishing to
	// acc.tap.{site}.{point}; the lock just logs its pulse.
	points := cfg.Controller.Points
	if len(points) == 0 {
		log.Warn("no access points configured; tap loop will be idle", "hint", "set controller.points")
	}
	reader, err := controller.NewNATSReader(nc.NC, cfg.Controller.Site, points, log)
	if err != nil {
		log.Fatal("failed to start reader", "error", err)
	}
	defer reader.Stop()

	locks := make(map[string]drivers.LockDriver, len(points))
	for _, p := range points {
		locks[p] = drivers.NewMockLock(p, log)
	}

	rt := controller.NewRuntime(cfg.Controller.Site, store, reader, locks, controller.NewNATSEmitter(nc.NC), log, m)
	go func() { _ = rt.Run(ctx) }()
	log.Info("tap loop running",
		"site", cfg.Controller.Site,
		"tapSubject", fmt.Sprintf("acc.tap.%s.<point>", cfg.Controller.Site))

	// Command handler: posture/unlock commands + site fire-alarm-input signal.
	cmds := controller.NewCommandHandler(cfg.Controller.Site, rt, log)
	if err := cmds.Start(nc.NC); err != nil {
		log.Fatal("failed to start command handler", "error", err)
	}
	defer cmds.Stop()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

	log.Info("shutdown signal received")
	if metricsSrv != nil {
		shutdownCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		_ = metricsSrv.Shutdown(shutdownCtx)
		c()
	}
}
