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

	// Read-only bind: the controller only watches policy, so its NATS identity
	// needs no stream-management rights. accessd owns/creates the bucket.
	kv, err := nc.KVBucket(ctx, cfg.Policy.Bucket)
	if err != nil {
		log.Fatal("failed to open policy KV bucket", "error", err)
	}

	// PolicyStore watches ACC_POLICY into in-memory maps and decides locally.
	store := controller.NewPolicyStore(kv, log, m)
	resync = store.Resync

	if len(cfg.Controller.Portals) == 0 {
		log.Warn("no portals configured; tap loop will be idle", "hint", "set controller.portals")
	}

	// The reader starts with no subscriptions and the runtime with no locks; the
	// portal manager arms each as it appears in policy and disarms it when it
	// leaves. Portal types/existence live in the system of record and can change
	// after boot, so arming is watch-driven rather than resolved once at startup.
	reader := controller.NewNATSReader(nc.NC, cfg.Controller.Location, subj, log, m)
	defer reader.Stop()

	rt := controller.NewRuntime(cfg.Controller.Location, store, reader, nil, controller.NewNATSEmitter(nc.NC), subj, log, m)

	mkLock := func(code string) drivers.LockDriver { return drivers.NewMockLock(code, log) }
	mgr := controller.NewPortalManager(cfg.Controller.Portals, cfg.Controller.Location, store, reader, rt, mkLock, log)
	store.SetOnChange(mgr.Notify) // must be set before Watch

	if err := store.Watch(ctx); err != nil {
		log.Fatal("failed to start policy watcher", "error", err)
	}
	defer store.Stop()

	mgr.Start(ctx)
	defer mgr.Stop()

	go func() { _ = rt.Run(ctx) }()

	// Command handler: posture/unlock commands + location fire-alarm-input signal.
	cmds := controller.NewCommandHandler(cfg.Controller.Location, rt, subj, log)
	if err := cmds.Start(nc.NC); err != nil {
		log.Fatal("failed to start command handler", "error", err)
	}
	defer cmds.Stop()

	log.Info("controller started; default-deny until policy syncs",
		"policyBucket", cfg.Policy.Bucket,
		"subjectsApp", cfg.Subjects.App,
		"location", cfg.Controller.Location,
		"portals", cfg.Controller.Portals)

	// Note when the initial sync lands, without blocking startup or crashing if
	// the hub is slow/unreachable: the watcher and reconciler keep retrying, and
	// the controller safely denies until policy arrives, then converges.
	go func() {
		if err := store.WaitReady(ctx); err != nil {
			return // context cancelled during shutdown
		}
		log.Info("policy synced; controller live")
	}()

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
