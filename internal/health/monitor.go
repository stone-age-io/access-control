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
//
// A liveness TRANSITION is a different matter and IS audited. A heartbeat is a
// flood (one every few seconds per box); an online↔offline flip is one event per
// outage, and it is the most operationally urgent thing the system notices — a box
// going offline means that site is now deciding on cached policy, or default-deny.
// So each transition publishes {app}.{location}.ctrl.{code}.evt.state, which the
// existing 6-token stream wildcard captures with no stream change, putting it in
// the events timeline and within reach of the notification sinks.
package health

import (
	"context"
	"encoding/json"
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

	// publish emits one liveness event. It defaults to the NATS connection's
	// Publish and is nil when no connection was supplied (then nothing is emitted).
	// Injected rather than called through mon.nc directly so tests can observe the
	// transitions — the same shape as notify.SendFunc and disarm.DisarmFunc.
	publish func(subject string, data []byte) error
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
	var pub func(string, []byte) error
	if nc != nil {
		pub = nc.Publish
	}
	return &Monitor{
		publish:      pub,
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
//
// Every heartbeat writes last_seen, but only a real status change emits an event:
// the status is set unconditionally (cheap, and it self-heals a drifted value), so
// the transition has to be read off the record BEFORE the Set.
func (mon *Monitor) markOnline(code string) error {
	rec, err := mon.app.FindFirstRecordByData("controllers", "code", code)
	if err != nil {
		mon.m.IncHeartbeatReceived("unknown")
		mon.log.Warn("heartbeat from unknown controller; ignoring", "code", code)
		return nil
	}
	transitioned := rec.GetString("status") != statusOnline
	rec.Set("last_seen", types.NowDateTime())
	rec.Set("status", statusOnline)
	if err := mon.app.Save(rec); err != nil {
		mon.m.IncHeartbeatReceived("error")
		return err
	}
	mon.m.IncHeartbeatReceived("ok")
	if transitioned {
		mon.emitLiveness(rec, statusOnline)
	}
	return nil
}

// emitLiveness publishes a controller's liveness transition to
// {app}.{location}.ctrl.{code}.evt.state. Fail-safe: the record write has already
// committed, so a publish failure is logged and swallowed — liveness in the
// controllers record must never depend on the event landing.
//
// The heartbeat subject carries the location code, but the offline direction has no
// inbound message to read it from, so both directions resolve it the same way: from
// the controller's location relation. A controller with no resolvable location can't
// be addressed on the wire and is skipped (the record write still stands).
func (mon *Monitor) emitLiveness(rec *core.Record, status string) {
	if mon.publish == nil {
		return // no NATS wired
	}
	location := mon.locationCode(rec)
	if location == "" {
		mon.log.Warn("controller has no resolvable location; liveness event not emitted",
			"code", rec.GetString("code"))
		return
	}
	body, err := json.Marshal(map[string]any{
		"status":   status,
		"lastSeen": rec.GetDateTime("last_seen").String(),
		"ts":       time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		mon.log.Error("failed to encode liveness event", "code", rec.GetString("code"), "error", err)
		return
	}
	subject := mon.subj.EventState(location, subjects.CtrlType, rec.GetString("code"))
	if err := mon.publish(subject, body); err != nil {
		mon.log.Error("failed to publish liveness event", "subject", subject, "error", err)
		return
	}
	mon.m.IncEventPublished("state")
}

// locationCode resolves a controller's location relation to the stable location
// code the subject scheme uses. Empty when the relation is unset or dangling.
func (mon *Monitor) locationCode(rec *core.Record) string {
	id := rec.GetString("location")
	if id == "" {
		return ""
	}
	loc, err := mon.app.FindRecordById("locations", id)
	if err != nil {
		return ""
	}
	return loc.GetString("code")
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
		// Only records currently "online" are queried above, so reaching here IS the
		// transition — a settled offline box never re-emits.
		mon.emitLiveness(rec, statusOffline)
	}
}
