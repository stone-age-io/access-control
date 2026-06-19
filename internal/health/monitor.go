// Package health keeps the controllers collection's liveness columns
// (last_seen/status) in step with controller heartbeats. It is the accessd-side
// counterpart to the controller's heartbeat publisher.
//
// Heartbeats are ephemeral control-plane signals: they ride core NATS (not
// JetStream) and sit outside the .evt subtree, so they are deliberately NOT in
// the audit stream. This monitor subscribes to them directly and writes the
// controllers record — never an events row, which would flood the audit log. A
// periodic sweep flips a controller to "offline" once its last heartbeat is older
// than offlineAfter, so a stopped box becomes visible without any signal arriving.
package health

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/metrics"
	"github.com/stone-age-io/access-control/internal/subjects"
)

// DefaultOfflineAfter is how long since the last heartbeat before a controller is
// considered offline, when not configured — three default heartbeat intervals.
const DefaultOfflineAfter = 45 * time.Second

const (
	statusOnline  = "online"
	statusOffline = "offline"
)

// Monitor subscribes to controller heartbeats and maintains controllers.last_seen
// and controllers.status.
type Monitor struct {
	app          core.App
	nc           *nats.Conn
	subj         subjects.Subjects
	offlineAfter time.Duration
	sweepEvery   time.Duration
	log          *logger.Logger
	m            *metrics.Metrics

	sub    *nats.Subscription
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates a health monitor. A non-positive offlineAfter falls back to
// DefaultOfflineAfter; the staleness sweep runs at a third of that cadence.
func New(app core.App, nc *nats.Conn, subj subjects.Subjects, offlineAfter time.Duration, log *logger.Logger, m *metrics.Metrics) *Monitor {
	if offlineAfter <= 0 {
		offlineAfter = DefaultOfflineAfter
	}
	sweep := offlineAfter / 3
	if sweep < time.Second {
		sweep = time.Second
	}
	return &Monitor{
		app:          app,
		nc:           nc,
		subj:         subj,
		offlineAfter: offlineAfter,
		sweepEvery:   sweep,
		log:          log.With("component", "health"),
		m:            m,
	}
}

// Start subscribes to the heartbeat subject and launches the staleness sweep. It
// owns its own context (cancelled by Stop), so it lives for the whole serve
// lifetime rather than the caller's boot context.
func (mon *Monitor) Start() error {
	subject := mon.subj.HeartbeatWildcard()
	sub, err := mon.nc.Subscribe(subject, mon.onHeartbeat)
	if err != nil {
		return fmt.Errorf("subscribe %q: %w", subject, err)
	}
	mon.sub = sub

	ctx, cancel := context.WithCancel(context.Background())
	mon.cancel = cancel
	mon.wg.Add(1)
	go mon.sweepLoop(ctx)

	mon.log.Info("controller health monitor started",
		"subject", subject, "offlineAfter", mon.offlineAfter, "sweepEvery", mon.sweepEvery)
	return nil
}

// Stop unsubscribes and halts the sweep loop.
func (mon *Monitor) Stop() {
	if mon.sub != nil {
		_ = mon.sub.Unsubscribe()
	}
	if mon.cancel != nil {
		mon.cancel()
	}
	mon.wg.Wait()
}

func (mon *Monitor) onHeartbeat(msg *nats.Msg) {
	_, code, ok := mon.subj.ParseHeartbeat(msg.Subject)
	if !ok || code == "" {
		mon.log.Warn("unrecognized heartbeat subject", "subject", msg.Subject)
		return
	}
	if err := mon.markOnline(code); err != nil {
		mon.log.Error("failed to record heartbeat", "code", code, "error", err)
	}
}

// markOnline stamps a controller's last_seen and flips it online. An unknown code
// (a box reporting in that is not registered, or codes drifted) is counted and
// skipped — never auto-created, fail-safe.
func (mon *Monitor) markOnline(code string) error {
	rec, err := mon.app.FindFirstRecordByData("controllers", "code", code)
	if err != nil {
		mon.m.IncHeartbeatReceived("unknown")
		mon.log.Warn("heartbeat from unknown controller; ignoring", "code", code)
		return nil
	}
	rec.Set("last_seen", types.NowDateTime())
	rec.Set("status", statusOnline)
	if err := mon.app.Save(rec); err != nil {
		mon.m.IncHeartbeatReceived("error")
		return err
	}
	mon.m.IncHeartbeatReceived("ok")
	return nil
}

func (mon *Monitor) sweepLoop(ctx context.Context) {
	defer mon.wg.Done()
	t := time.NewTicker(mon.sweepEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			mon.sweep()
		}
	}
}

// sweep marks online controllers offline once their last heartbeat is stale. Only
// records currently "online" are queried, and a record is written only when it
// actually transitions, so a settled offline box generates no churn.
func (mon *Monitor) sweep() {
	cutoff := time.Now().UTC().Add(-mon.offlineAfter)
	// statusOnline is a constant, not user input — safe to inline in the filter.
	recs, err := mon.app.FindRecordsByFilter("controllers", "status = 'online'", "", 0, 0)
	if err != nil {
		mon.log.Error("health sweep query failed", "error", err)
		return
	}
	for _, rec := range recs {
		ls := rec.GetDateTime("last_seen").Time()
		if !ls.IsZero() && !ls.Before(cutoff) {
			continue // still fresh
		}
		rec.Set("status", statusOffline)
		if err := mon.app.Save(rec); err != nil {
			mon.log.Error("failed to mark controller offline", "code", rec.GetString("code"), "error", err)
			continue
		}
		mon.log.Info("controller offline (heartbeat stale)", "code", rec.GetString("code"), "lastSeen", rec.GetDateTime("last_seen").String())
	}
}
