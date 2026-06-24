// file: internal/metrics/metrics.go

// Package metrics provides Prometheus instrumentation for the stone-access
// binaries. It is deliberately small: access decisions, KV mirror/watch health,
// event publishing, NATS connection state, and process gauges.
package metrics

import (
	"runtime"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds all collectors. A nil *Metrics is safe to pass around; every
// method guards on the receiver so callers don't have to.
type Metrics struct {
	registry *prometheus.Registry

	// Access decisions (controller).
	decisionsTotal *prometheus.CounterVec // labels: allow, reason

	// Taps dropped because the runtime could not keep up (controller).
	tapsDroppedTotal prometheus.Counter

	// Policy KV mirror (accessd publisher) and watch (controller).
	kvAppliesTotal *prometheus.CounterVec // labels: op (put/delete)
	kvWatchState   prometheus.Gauge       // 1 = synced, 0 = resyncing/disconnected

	// Event publishing (controller) and audit consumption (accessd).
	eventsPublishedTotal *prometheus.CounterVec // labels: kind
	auditWritesTotal     *prometheus.CounterVec // labels: status (ok/error)

	// Notification sink (accessd): the second ACC_EVENTS consumer that emails on
	// alarm/fire. status = ok/error/skip/dedup.
	notifySendsTotal *prometheus.CounterVec // labels: status

	// Disarm sink (accessd): the ACC_EVENTS tap consumer that durably disarms an
	// area on a valid grant at an entry portal. status = disarmed/skip/error.
	disarmsTotal *prometheus.CounterVec // labels: status

	// Controller heartbeats: sent by the controller, received by accessd.
	heartbeatsSentTotal     prometheus.Counter     // controller
	heartbeatsReceivedTotal *prometheus.CounterVec // labels: status (ok/unknown/error) (accessd)

	// NATS connection state (shared).
	natsConnectionStatus prometheus.Gauge
	natsReconnects       prometheus.Counter

	// Process gauges (shared).
	goroutines  prometheus.Gauge
	memoryBytes prometheus.Gauge
}

// NewMetrics creates and registers all collectors on the given registry.
func NewMetrics(registry *prometheus.Registry) (*Metrics, error) {
	m := &Metrics{
		registry: registry,

		decisionsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "access_decisions_total",
				Help: "Total access decisions by outcome and reason code",
			},
			[]string{"allow", "reason"},
		),
		tapsDroppedTotal: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "taps_dropped_total",
				Help: "Total taps dropped because the runtime tap queue was full",
			},
		),
		kvAppliesTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "policy_kv_applies_total",
				Help: "Total policy KV operations applied (mirror writes or watch deltas) by op",
			},
			[]string{"op"},
		),
		kvWatchState: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "policy_kv_watch_state",
				Help: "Policy KV watch state (1 = synced, 0 = resyncing/disconnected)",
			},
		),
		eventsPublishedTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "events_published_total",
				Help: "Total access events published by kind (tap/state/alarm/fire)",
			},
			[]string{"kind"},
		),
		auditWritesTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "audit_writes_total",
				Help: "Total audit event rows written to the events collection by status",
			},
			[]string{"status"},
		),
		notifySendsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "notify_sends_total",
				Help: "Total notification-sink outcomes by status (ok/error/skip/dedup)",
			},
			[]string{"status"},
		),
		disarmsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "disarm_sink_total",
				Help: "Total disarm-sink outcomes by status (disarmed/skip/error)",
			},
			[]string{"status"},
		),
		heartbeatsSentTotal: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "controller_heartbeats_sent_total",
				Help: "Total liveness heartbeats published by this controller",
			},
		),
		heartbeatsReceivedTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "controller_heartbeats_received_total",
				Help: "Total controller heartbeats accessd processed by status (ok/unknown/error)",
			},
			[]string{"status"},
		),
		natsConnectionStatus: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "nats_connection_status",
				Help: "NATS connection status (1 = connected, 0 = disconnected)",
			},
		),
		natsReconnects: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "nats_reconnects_total",
				Help: "Total number of NATS reconnections",
			},
		),
		goroutines: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "process_goroutines",
				Help: "Number of goroutines",
			},
		),
		memoryBytes: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "process_memory_bytes",
				Help: "Process memory usage in bytes",
			},
		),
	}

	collectors := []prometheus.Collector{
		m.decisionsTotal,
		m.tapsDroppedTotal,
		m.kvAppliesTotal,
		m.kvWatchState,
		m.eventsPublishedTotal,
		m.auditWritesTotal,
		m.notifySendsTotal,
		m.disarmsTotal,
		m.heartbeatsSentTotal,
		m.heartbeatsReceivedTotal,
		m.natsConnectionStatus,
		m.natsReconnects,
		m.goroutines,
		m.memoryBytes,
	}
	for _, c := range collectors {
		if err := registry.Register(c); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// GetRegistry returns the Prometheus registry (needed for the HTTP handler).
func (m *Metrics) GetRegistry() *prometheus.Registry {
	if m == nil {
		return nil
	}
	return m.registry
}

// IncDecision records one access decision outcome.
func (m *Metrics) IncDecision(allow bool, reason string) {
	if m == nil {
		return
	}
	allowStr := "false"
	if allow {
		allowStr = "true"
	}
	m.decisionsTotal.WithLabelValues(allowStr, reason).Inc()
}

// IncTapDropped records one tap dropped due to a full runtime tap queue.
func (m *Metrics) IncTapDropped() {
	if m == nil {
		return
	}
	m.tapsDroppedTotal.Inc()
}

// IncKVApply records one policy KV operation (op = "put" or "delete").
func (m *Metrics) IncKVApply(op string) {
	if m == nil {
		return
	}
	m.kvAppliesTotal.WithLabelValues(op).Inc()
}

// SetKVWatchState sets the policy KV watch health (true = synced).
func (m *Metrics) SetKVWatchState(synced bool) {
	if m == nil {
		return
	}
	if synced {
		m.kvWatchState.Set(1)
	} else {
		m.kvWatchState.Set(0)
	}
}

// IncEventPublished records one published access event of the given kind.
func (m *Metrics) IncEventPublished(kind string) {
	if m == nil {
		return
	}
	m.eventsPublishedTotal.WithLabelValues(kind).Inc()
}

// IncAuditWrite records one audit-row write outcome (status = "ok"/"error").
func (m *Metrics) IncAuditWrite(status string) {
	if m == nil {
		return
	}
	m.auditWritesTotal.WithLabelValues(status).Inc()
}

// IncNotify records one notification-sink outcome (status =
// "ok"/"error"/"skip"/"dedup").
func (m *Metrics) IncNotify(status string) {
	if m == nil {
		return
	}
	m.notifySendsTotal.WithLabelValues(status).Inc()
}

// IncDisarm records one disarm-sink outcome (status =
// "disarmed"/"skip"/"error").
func (m *Metrics) IncDisarm(status string) {
	if m == nil {
		return
	}
	m.disarmsTotal.WithLabelValues(status).Inc()
}

// IncHeartbeatSent records one liveness heartbeat published by the controller.
func (m *Metrics) IncHeartbeatSent() {
	if m == nil {
		return
	}
	m.heartbeatsSentTotal.Inc()
}

// IncHeartbeatReceived records one heartbeat accessd processed (status =
// "ok"/"unknown"/"error").
func (m *Metrics) IncHeartbeatReceived(status string) {
	if m == nil {
		return
	}
	m.heartbeatsReceivedTotal.WithLabelValues(status).Inc()
}

// SetNATSConnectionStatus sets the NATS connection gauge.
func (m *Metrics) SetNATSConnectionStatus(connected bool) {
	if m == nil {
		return
	}
	if connected {
		m.natsConnectionStatus.Set(1)
	} else {
		m.natsConnectionStatus.Set(0)
	}
}

// IncNATSReconnects increments the reconnect counter.
func (m *Metrics) IncNATSReconnects() {
	if m == nil {
		return
	}
	m.natsReconnects.Inc()
}

// UpdateSystemMetrics refreshes the process gauges.
func (m *Metrics) UpdateSystemMetrics() {
	if m == nil {
		return
	}
	m.goroutines.Set(float64(runtime.NumGoroutine()))

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	m.memoryBytes.Set(float64(memStats.Alloc))
}
