// Command access-controller is the edge stone-access app: it watches the policy
// KV keyspace into in-memory maps, decides credential presentations locally
// with the pure policy.Decide function, drives the reader/lock hardware (mocked
// in v1), and emits access events to JetStream.
//
// v1 drivers are mocks: taps are simulated by publishing to
// {app}.{location}.{type}.{thing}.tap and the lock just logs its pulse.
package main

import (
	"context"
	"flag"
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
	"github.com/stone-age-io/access-control/internal/subjects"
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
	log = log.With("app", "access-controller", "location", cfg.Controller.Location)

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

	// One subjects app token shared by the reader, runtime emitter, and command
	// handler — and matching accessd's, so policy/events flow between them.
	subj := subjects.New(cfg.Subjects.App)

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
		"subjectsApp", cfg.Subjects.App,
		"portals", cfg.Controller.Portals)

	// Resolve each configured portal's type from the synced policy so the reader
	// and emitter can build exact {location}.{type}.{thing} subjects. A portal
	// unknown to policy (or with no type) is skipped + logged — it default-denies
	// by omission rather than arming a reader we can't address.
	if len(cfg.Controller.Portals) == 0 {
		log.Warn("no portals configured; tap loop will be idle", "hint", "set controller.portals")
	}
	portals := make([]controller.Portal, 0, len(cfg.Controller.Portals))
	for _, code := range cfg.Controller.Portals {
		ap, ok := store.Portal(code)
		if !ok || ap.Type == "" {
			log.Error("configured portal is unknown or has no type; skipping",
				"portal", code, "hint", "create the portal in accessd with a type")
			continue
		}
		portals = append(portals, controller.Portal{Code: code, Type: ap.Type})
	}

	// Tap loop with mock drivers. Taps are simulated by publishing to
	// {app}.{location}.{type}.{thing}.tap; the lock just logs its pulse.
	reader, err := controller.NewNATSReader(nc.NC, cfg.Controller.Location, portals, subj, log)
	if err != nil {
		log.Fatal("failed to start reader", "error", err)
	}
	defer reader.Stop()

	locks := make(map[string]drivers.LockDriver, len(portals))
	for _, p := range portals {
		locks[p.Code] = drivers.NewMockLock(p.Code, log)
	}

	rt := controller.NewRuntime(cfg.Controller.Location, store, reader, locks, controller.NewNATSEmitter(nc.NC), subj, log, m)
	go func() { _ = rt.Run(ctx) }()
	log.Info("tap loop running", "location", cfg.Controller.Location, "portals", len(portals))

	// Command handler: posture/unlock commands + location fire-alarm-input signal.
	cmds := controller.NewCommandHandler(cfg.Controller.Location, rt, subj, log)
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
