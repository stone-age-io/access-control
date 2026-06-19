package controller

import (
	"context"
	"time"

	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/metrics"
	"github.com/stone-age-io/access-control/internal/subjects"
)

// DefaultHeartbeatInterval is the controller's liveness publish cadence when the
// configured interval is unset/non-positive.
const DefaultHeartbeatInterval = 15 * time.Second

// HeartbeatPayload is the heartbeat body. The code/location are also in the
// subject; carrying them keeps the message self-describing for any direct
// subscriber, and ts dates the liveness sample.
type HeartbeatPayload struct {
	Code     string `json:"code"`
	Location string `json:"location"`
	TS       string `json:"ts"`
}

// Heartbeat periodically publishes a controller-liveness signal so accessd can
// surface an online/offline controller. It reuses the runtime's Emitter (core
// NATS, fire-and-forget) but publishes to the ctrl-scoped heartbeat subject,
// which sits outside the .evt subtree and so never lands in the audit stream.
//
// The publish ticker is the second deliberate exception to the controller's
// "no ticker" rule (the first is the DOTL timer): it is a health/operational
// concern, not policy.
type Heartbeat struct {
	emit     Emitter
	subj     subjects.Subjects
	code     string
	location string
	interval time.Duration
	log      *logger.Logger
	m        *metrics.Metrics

	cancel context.CancelFunc
	done   chan struct{}
}

// NewHeartbeat creates a heartbeat publisher for one controller identity. A
// non-positive interval falls back to DefaultHeartbeatInterval.
func NewHeartbeat(emit Emitter, subj subjects.Subjects, code, location string, interval time.Duration, log *logger.Logger, m *metrics.Metrics) *Heartbeat {
	if interval <= 0 {
		interval = DefaultHeartbeatInterval
	}
	return &Heartbeat{
		emit:     emit,
		subj:     subj,
		code:     code,
		location: location,
		interval: interval,
		log:      log.With("component", "heartbeat"),
		m:        m,
	}
}

// Start publishes one heartbeat immediately, then every interval until Stop or
// the parent context is cancelled. It is a no-op (with a warning) when the
// controller has no code, since accessd keys liveness on the code.
func (h *Heartbeat) Start(parent context.Context) {
	if h.code == "" {
		h.log.Warn("no controller code; not publishing heartbeats")
		return
	}
	ctx, cancel := context.WithCancel(parent)
	h.cancel = cancel
	h.done = make(chan struct{})
	go func() {
		defer close(h.done)
		t := time.NewTicker(h.interval)
		defer t.Stop()
		h.publish() // announce liveness immediately on start
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				h.publish()
			}
		}
	}()
	h.log.Info("heartbeat started", "interval", h.interval, "subject", h.subj.Heartbeat(h.location, h.code))
}

func (h *Heartbeat) publish() {
	subject := h.subj.Heartbeat(h.location, h.code)
	if err := h.emit.Emit(subject, HeartbeatPayload{
		Code:     h.code,
		Location: h.location,
		TS:       time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		h.log.Error("failed to publish heartbeat", "subject", subject, "error", err)
		return
	}
	h.m.IncHeartbeatSent()
}

// Stop halts the heartbeat loop and waits for the goroutine to exit. Safe to call
// when Start was a no-op (no code).
func (h *Heartbeat) Stop() {
	if h.cancel != nil {
		h.cancel()
	}
	if h.done != nil {
		<-h.done
	}
}
