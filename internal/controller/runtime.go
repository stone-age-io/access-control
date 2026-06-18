package controller

import (
	"context"
	"sync"
	"time"

	"github.com/stone-age-io/access-control/internal/drivers"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/metrics"
	"github.com/stone-age-io/access-control/internal/subjects"
)

// Runtime is the edge controller's event loop. It is purely event-driven: it
// reacts to taps, commands, and fire signals with no polling ticker. For each
// tap it resolves the effective posture, decides locally, emits one tap event,
// and pulses the lock on allow.
type Runtime struct {
	location string
	store    *PolicyStore
	reader   drivers.ReaderDriver
	locks    map[string]drivers.LockDriver
	emit     Emitter
	subs     subjects.Subjects
	log      *logger.Logger
	m        *metrics.Metrics

	mu        sync.RWMutex
	overrides map[string]string // portal -> runtime posture override (command-driven)
	fire      map[string]bool   // location -> fire-alarm-input active (suppresses alarms)
}

// NewRuntime wires the tap loop. locks maps portal code to its lock driver.
func NewRuntime(location string, store *PolicyStore, reader drivers.ReaderDriver, locks map[string]drivers.LockDriver, emit Emitter, subs subjects.Subjects, log *logger.Logger, m *metrics.Metrics) *Runtime {
	return &Runtime{
		location:  location,
		store:     store,
		reader:    reader,
		locks:     locks,
		emit:      emit,
		subs:      subs,
		log:       log.With("component", "runtime"),
		m:         m,
		overrides: make(map[string]string),
		fire:      make(map[string]bool),
	}
}

// Run consumes taps until the context is cancelled or the reader channel closes.
func (r *Runtime) Run(ctx context.Context) error {
	taps := r.reader.Taps()
	r.log.Info("tap loop started", "location", r.location)
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
	posture := r.postureFor(tap.Portal)
	d := r.store.Decide(posture, tap.Credential, tap.Portal, tap.At)
	r.m.IncDecision(d.Allow, d.Reason)

	location, ptype := r.portalAddr(tap.Portal)
	if ptype != "" {
		if err := r.emit.Emit(r.subs.EventTap(location, ptype, tap.Portal), TapEvent{
			Cred:   tap.Credential,
			User:   d.User,
			Allow:  d.Allow,
			Reason: d.Reason,
			TS:     tap.At.UTC().Format(time.RFC3339),
		}); err != nil {
			r.log.Error("failed to emit tap event", "portal", tap.Portal, "error", err)
		} else {
			r.m.IncEventPublished("tap")
		}
	} else {
		r.log.Warn("unknown portal type; tap event not emitted", "portal", tap.Portal)
	}

	r.log.Info("tap decided",
		"location", location, "portal", tap.Portal, "cred", tap.Credential,
		"allow", d.Allow, "reason", d.Reason, "user", d.User)

	if d.Allow {
		if lock, ok := r.locks[tap.Portal]; ok {
			if err := lock.Pulse(d.Pulse); err != nil {
				r.log.Error("lock pulse failed", "portal", tap.Portal, "error", err)
			}
		} else {
			r.log.Warn("granted but no lock driver for portal", "portal", tap.Portal)
		}
	}
}

// postureFor returns the effective posture for a portal: a runtime command
// override if present, otherwise the portal's standing posture. Unknown portals
// return "" — Decide checks portal existence first and denies regardless.
func (r *Runtime) postureFor(portal string) string {
	r.mu.RLock()
	override, has := r.overrides[portal]
	r.mu.RUnlock()
	if has {
		return override
	}
	if ap, ok := r.store.Portal(portal); ok {
		return ap.Posture
	}
	return ""
}

// portalAddr resolves a portal's location and type for subject construction. It
// falls back to the controller's configured location; an unknown portal yields
// an empty type, which callers treat as "can't build a subject" (skip + log).
func (r *Runtime) portalAddr(portal string) (location, ptype string) {
	location = r.location
	if ap, ok := r.store.Portal(portal); ok {
		if ap.Location != "" {
			location = ap.Location
		}
		ptype = ap.Type
	}
	return location, ptype
}

// SetPosture installs a runtime posture override for a portal and emits a state
// event. Used by the command handler. The override is operational state and is
// never written back to PocketBase.
func (r *Runtime) SetPosture(portal, posture, actor, reason string, at time.Time) {
	r.mu.Lock()
	r.overrides[portal] = posture
	r.mu.Unlock()
	r.emitState(portal, posture, actor, reason, at)
}

// ClearPosture removes a runtime override, reverting to the standing posture,
// and emits a state event reflecting the now-effective posture.
func (r *Runtime) ClearPosture(portal, actor, reason string, at time.Time) {
	r.mu.Lock()
	delete(r.overrides, portal)
	r.mu.Unlock()
	r.emitState(portal, r.postureFor(portal), actor, reason, at)
}

// Unlock momentarily energizes the strike for a portal (a command-driven pulse,
// distinct from a standing posture change). A non-positive seconds falls back to
// the portal's configured pulse.
func (r *Runtime) Unlock(portal string, seconds int, actor, reason string) {
	if seconds <= 0 {
		if ap, ok := r.store.Portal(portal); ok {
			seconds = ap.PulseSeconds
		}
	}
	lock, ok := r.locks[portal]
	if !ok {
		r.log.Warn("unlock command but no lock driver for portal", "portal", portal)
		return
	}
	if err := lock.Pulse(seconds); err != nil {
		r.log.Error("command unlock pulse failed", "portal", portal, "error", err)
		return
	}
	r.log.Info("command unlock", "portal", portal, "seconds", seconds, "actor", actor, "reason", reason)
}

// SetFire records a location's fire-alarm-input state. While active, the
// controller suppresses alarm emission for that location (forced/held-open events
// would be false alarms during an evacuation). It never changes posture and never
// unlocks — hardware owns egress.
func (r *Runtime) SetFire(location string, active bool, at time.Time) {
	r.mu.Lock()
	if active {
		r.fire[location] = true
	} else {
		delete(r.fire, location)
	}
	r.mu.Unlock()
	r.log.Info("fire state changed", "location", location, "active", active, "ts", at.UTC().Format(time.RFC3339))
}

// EmitAlarm emits a portal alarm unless the location's fire input is active, in
// which case it is suppressed. (v1 has no alarm source yet; this is the gate real
// forced/held-open detection will flow through.)
func (r *Runtime) EmitAlarm(portal, alarmType string, at time.Time) {
	location, ptype := r.portalAddr(portal)
	if ptype == "" {
		r.log.Warn("unknown portal type; alarm not emitted", "portal", portal)
		return
	}

	r.mu.RLock()
	suppressed := r.fire[location]
	r.mu.RUnlock()
	if suppressed {
		r.log.Info("alarm suppressed (fire active)", "location", location, "portal", portal, "type", alarmType)
		return
	}

	if err := r.emit.Emit(r.subs.EventAlarm(location, ptype, portal), map[string]any{
		"type": alarmType,
		"ts":   at.UTC().Format(time.RFC3339),
	}); err != nil {
		r.log.Error("failed to emit alarm event", "portal", portal, "error", err)
		return
	}
	r.m.IncEventPublished("alarm")
}

func (r *Runtime) emitState(portal, posture, actor, reason string, at time.Time) {
	location, ptype := r.portalAddr(portal)
	if ptype == "" {
		r.log.Warn("unknown portal type; state event not emitted", "portal", portal)
		return
	}
	if err := r.emit.Emit(r.subs.EventState(location, ptype, portal), StateEvent{
		Posture: posture, Actor: actor, Reason: reason,
		TS: at.UTC().Format(time.RFC3339),
	}); err != nil {
		r.log.Error("failed to emit state event", "portal", portal, "error", err)
	} else {
		r.m.IncEventPublished("state")
	}
}
