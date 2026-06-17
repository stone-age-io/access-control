package controller

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/stone-age-io/access-control/internal/drivers"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/metrics"
)

// Runtime is the edge controller's event loop. It is purely event-driven: it
// reacts to taps (and, in step 7, commands and fire signals) with no polling
// ticker. For each tap it resolves the effective posture, decides locally,
// emits one tap event, and pulses the lock on allow.
type Runtime struct {
	site   string
	store  *PolicyStore
	reader drivers.ReaderDriver
	locks  map[string]drivers.LockDriver
	emit   Emitter
	log    *logger.Logger
	m      *metrics.Metrics

	mu        sync.RWMutex
	overrides map[string]string // point -> runtime posture override (command-driven)
	fire      map[string]bool   // site -> fire-alarm-input active (suppresses alarms)
}

// NewRuntime wires the tap loop. locks maps access-point code to its lock driver.
func NewRuntime(site string, store *PolicyStore, reader drivers.ReaderDriver, locks map[string]drivers.LockDriver, emit Emitter, log *logger.Logger, m *metrics.Metrics) *Runtime {
	return &Runtime{
		site:      site,
		store:     store,
		reader:    reader,
		locks:     locks,
		emit:      emit,
		log:       log.With("component", "runtime"),
		m:         m,
		overrides: make(map[string]string),
		fire:      make(map[string]bool),
	}
}

// Run consumes taps until the context is cancelled or the reader channel closes.
func (r *Runtime) Run(ctx context.Context) error {
	taps := r.reader.Taps()
	r.log.Info("tap loop started", "site", r.site)
	for {
		select {
		case <-ctx.Done():
			return nil
		case tap, ok := <-taps:
			if !ok {
				r.log.Info("reader closed; tap loop stopping")
				return nil
			}
			r.handleTap(tap)
		}
	}
}

func (r *Runtime) handleTap(tap drivers.Tap) {
	posture := r.postureFor(tap.Point)
	d := r.store.Decide(posture, tap.Credential, tap.Point, tap.At)
	r.m.IncDecision(d.Allow, d.Reason)

	// Attribute the event to the point's actual site when known.
	site := r.site
	if ap, ok := r.store.Point(tap.Point); ok && ap.Site != "" {
		site = ap.Site
	}

	if err := r.emit.Emit(tapSubject(site, tap.Point), TapEvent{
		Cred:   tap.Credential,
		User:   d.User,
		Allow:  d.Allow,
		Reason: d.Reason,
		TS:     tap.At.UTC().Format(time.RFC3339),
	}); err != nil {
		r.log.Error("failed to emit tap event", "point", tap.Point, "error", err)
	} else {
		r.m.IncEventPublished("tap")
	}

	r.log.Info("tap decided",
		"site", site, "point", tap.Point, "cred", tap.Credential,
		"allow", d.Allow, "reason", d.Reason, "user", d.User)

	if d.Allow {
		if lock, ok := r.locks[tap.Point]; ok {
			if err := lock.Pulse(d.Pulse); err != nil {
				r.log.Error("lock pulse failed", "point", tap.Point, "error", err)
			}
		} else {
			r.log.Warn("granted but no lock driver for point", "point", tap.Point)
		}
	}
}

// postureFor returns the effective posture for an access point: a runtime
// command override if present, otherwise the point's standing posture. Unknown
// points return "" — Decide checks point existence first and denies regardless.
func (r *Runtime) postureFor(point string) string {
	r.mu.RLock()
	override, has := r.overrides[point]
	r.mu.RUnlock()
	if has {
		return override
	}
	if ap, ok := r.store.Point(point); ok {
		return ap.Posture
	}
	return ""
}

// SetPosture installs a runtime posture override for an access point and emits a
// state event. Used by the command handler (step 7). The override is
// operational state and is never written back to PocketBase.
func (r *Runtime) SetPosture(point, posture, actor, reason string, at time.Time) {
	r.mu.Lock()
	r.overrides[point] = posture
	r.mu.Unlock()
	r.emitState(point, posture, actor, reason, at)
}

// ClearPosture removes a runtime override, reverting to the standing posture,
// and emits a state event reflecting the now-effective posture.
func (r *Runtime) ClearPosture(point, actor, reason string, at time.Time) {
	r.mu.Lock()
	delete(r.overrides, point)
	r.mu.Unlock()
	r.emitState(point, r.postureFor(point), actor, reason, at)
}

// Unlock momentarily energizes the strike for an access point (a command-driven
// pulse, distinct from a standing posture change). A non-positive seconds falls
// back to the point's configured pulse.
func (r *Runtime) Unlock(point string, seconds int, actor, reason string) {
	if seconds <= 0 {
		if ap, ok := r.store.Point(point); ok {
			seconds = ap.PulseSeconds
		}
	}
	lock, ok := r.locks[point]
	if !ok {
		r.log.Warn("unlock command but no lock driver for point", "point", point)
		return
	}
	if err := lock.Pulse(seconds); err != nil {
		r.log.Error("command unlock pulse failed", "point", point, "error", err)
		return
	}
	r.log.Info("command unlock", "point", point, "seconds", seconds, "actor", actor, "reason", reason)
}

// SetFire records a site's fire-alarm-input state. While active, the controller
// suppresses alarm emission for that site (forced/held-open events would be
// false alarms during an evacuation). It never changes posture and never
// unlocks — hardware owns egress.
func (r *Runtime) SetFire(site string, active bool, at time.Time) {
	r.mu.Lock()
	if active {
		r.fire[site] = true
	} else {
		delete(r.fire, site)
	}
	r.mu.Unlock()
	r.log.Info("fire state changed", "site", site, "active", active, "ts", at.UTC().Format(time.RFC3339))
}

// EmitAlarm emits an access-point alarm unless the site's fire input is active,
// in which case it is suppressed. (v1 has no alarm source yet; this is the gate
// real forced/held-open detection will flow through.)
func (r *Runtime) EmitAlarm(point, alarmType string, at time.Time) {
	site := r.site
	if ap, ok := r.store.Point(point); ok && ap.Site != "" {
		site = ap.Site
	}

	r.mu.RLock()
	suppressed := r.fire[site]
	r.mu.RUnlock()
	if suppressed {
		r.log.Info("alarm suppressed (fire active)", "site", site, "point", point, "type", alarmType)
		return
	}

	subject := fmt.Sprintf("acc.evt.%s.%s.alarm", site, point)
	if err := r.emit.Emit(subject, map[string]any{
		"type": alarmType,
		"ts":   at.UTC().Format(time.RFC3339),
	}); err != nil {
		r.log.Error("failed to emit alarm event", "point", point, "error", err)
		return
	}
	r.m.IncEventPublished("alarm")
}

func (r *Runtime) emitState(point, posture, actor, reason string, at time.Time) {
	site := r.site
	if ap, ok := r.store.Point(point); ok && ap.Site != "" {
		site = ap.Site
	}
	if err := r.emit.Emit(stateSubject(site, point), StateEvent{
		Posture: posture, Actor: actor, Reason: reason,
		TS: at.UTC().Format(time.RFC3339),
	}); err != nil {
		r.log.Error("failed to emit state event", "point", point, "error", err)
	} else {
		r.m.IncEventPublished("state")
	}
}
