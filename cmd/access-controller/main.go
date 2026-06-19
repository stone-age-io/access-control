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
	"github.com/stone-age-io/access-control/internal/drivers/gpio"
	"github.com/stone-age-io/access-control/internal/drivers/hardware"
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
	log = log.With("app", "access-controller", "controller", cfg.Controller.Code, "location", cfg.Controller.Location)

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

	if cfg.Controller.Code == "" {
		log.Warn("no controller code configured; no portals will be armed", "hint", "set controller.code to a controllers record")
	}

	// The reader starts with no subscriptions and the runtime with no locks; the
	// portal manager arms each as it appears in policy and disarms it when it
	// leaves. Portal types/existence live in the system of record and can change
	// after boot, so arming is watch-driven rather than resolved once at startup.
	reader := controller.NewNATSReader(nc.NC, cfg.Controller.Location, subj, log, m)
	defer reader.Stop()

	// Portal hardware backend. mock (default) drives no physical I/O — taps are
	// simulated over NATS and there are no door inputs. gpio drives real relays and
	// DPS/REX lines via the controller model's hardware profile, and is also the
	// runtime's door-input source. The backend resolves each portal's logical
	// relay/input indices itself, as portals are armed/disarmed by the reconciler.
	var (
		portalHW  controller.PortalHardware
		doorInput drivers.DoorInput // nil for mock (no door monitoring)
	)
	switch cfg.Controller.Driver {
	case "gpio":
		profile, ok := hardware.ProfileFor(cfg.Controller.Model)
		if !ok {
			log.Fatal("unknown controller model for gpio driver",
				"model", cfg.Controller.Model, "known", hardware.Models())
		}
		gpioHW, err := gpio.New(profile, log)
		if err != nil {
			log.Fatal("failed to initialize gpio driver", "error", err)
		}
		defer func() { _ = gpioHW.Close() }()
		portalHW = gpioHW
		doorInput = gpioHW
		log.Info("portal hardware driver: gpio", "model", cfg.Controller.Model)
	default: // "mock" (validated by config)
		portalHW = drivers.NewMockHardware(log)
		log.Info("portal hardware driver: mock (no physical I/O)")
	}

	emit := controller.NewNATSEmitter(nc.NC)
	rt := controller.NewRuntime(cfg.Controller.Location, store, reader, doorInput, nil, emit, subj, log, m)

	mgr := controller.NewPortalManager(cfg.Controller.Code, cfg.Controller.Location, store, reader, rt, portalHW, log)
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

	// Liveness heartbeat: accessd marks this box online/offline from it. Rides
	// core NATS on the ctrl-scoped subject (outside the audit stream). A no-op
	// when no controller code is configured.
	hb := controller.NewHeartbeat(emit, subj, cfg.Controller.Code, cfg.Controller.Location, cfg.Controller.HeartbeatInterval, log, m)
	hb.Start(ctx)
	defer hb.Stop()

	log.Info("controller started; default-deny until policy syncs",
		"policyBucket", cfg.Policy.Bucket,
		"subjectsApp", cfg.Subjects.App,
		"location", cfg.Controller.Location,
		"controllerCode", cfg.Controller.Code)

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
