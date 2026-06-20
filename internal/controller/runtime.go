package controller

import (
	"context"
	"sync"
	"time"

	"github.com/stone-age-io/access-control/internal/drivers"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/metrics"
	"github.com/stone-age-io/access-control/internal/policy"
	"github.com/stone-age-io/access-control/internal/statuskv"
	"github.com/stone-age-io/access-control/internal/subjects"
)

// Alarm types carried in an alarm event's payload "type" field. Stable strings:
// they flow verbatim into events and the UI, like decision reason codes.
const (
	AlarmForced    = "forced"     // door opened without a grant or REX
	AlarmHeld      = "held"       // door held open past its DOTL threshold
	AlarmHeldClear = "held_clear" // a held-open door closed
)

// Door-monitoring timing. Vars (not consts) so tests can shorten them — same
// pattern as the KV watch backoff in policystore.go.
var (
	// accessGrace is how long after a grant or REX an open counts as authorized
	// rather than forced (time to actually push the door after the strike fires).
	accessGrace = 10 * time.Second
	// heldOpenUnit multiplies a portal's held_open_seconds into a duration.
	heldOpenUnit = time.Second
	// holdEvalInterval is how often the runtime re-reconciles each portal's strike
	// hold to its effective posture — the no-event fallback that flips scheduled
	// posture at window boundaries. It is the controller's third deliberate timer
	// exception (with the DOTL timer and the heartbeat); commands and arming
	// reconcile holds immediately, so this only has to be tight enough for time
	// boundaries, and schedules are minute-grained. A var so tests can shorten it.
	holdEvalInterval = 10 * time.Second
)

// doorMonitor is the per-portal door-state machine: current contact state, the
// windows during which an open is authorized, and the live door-open-too-long
// (DOTL) timer. Guarded by Runtime.mu — the tap/input loop and the DOTL timer
// goroutine both touch it.
type doorMonitor struct {
	open       bool
	dpsSeen    bool // a DPS edge has been observed (else door state is unknown)
	grantUntil time.Time
	rexUntil   time.Time
	timer      *time.Timer
	held       bool // a held-open alarm is currently active (for the clear)
}

// portalShadow is the last status the runtime published for a portal. The runtime
// only writes status when these semantic fields change, so the periodic hold-eval
// reconcile never churns the status bucket with timestamp-only updates.
type portalShadow struct {
	door    string
	posture string
	held    bool
}

// auxOutputState is the runtime's view of a driven aux output relay.
type auxOutputState struct {
	lock         drivers.LockDriver
	location     string
	pulseSeconds int
	energized    bool // standing held state (on/off; a pulse is momentary, not tracked)
}

// auxInputState is the runtime's view of a driven aux input (location + last
// published value for dedup).
type auxInputState struct {
	location string
	active   bool
}

// Runtime is the edge controller's event loop. It is purely event-driven: it
// reacts to taps, door inputs, commands, and fire signals. For each tap it
// resolves the effective posture, decides locally, emits one tap event, and
// pulses the lock on allow. Door inputs drive forced/held-open detection.
//
// It grows no polling ticker; the one exception is the per-door DOTL timer
// (held-open detection), which is hardware-local timing, not policy.
type Runtime struct {
	location string
	store    *PolicyStore
	reader   drivers.ReaderDriver
	input    drivers.DoorInput
	locks    map[string]drivers.LockDriver
	emit     Emitter
	subs     subjects.Subjects
	log      *logger.Logger
	m        *metrics.Metrics

	mu        sync.RWMutex
	overrides map[string]string       // portal -> runtime posture override (command-driven)
	fire      map[string]bool         // location -> fire-alarm-input active (suppresses alarms)
	monitors  map[string]*doorMonitor // portal -> door-state machine
	shadow    map[string]portalShadow // portal -> last status published (change detection)

	auxOutputs map[string]*auxOutputState // aux output code -> driven relay
	auxInputs  map[string]*auxInputState  // aux input code -> observed line

	statusWriter *StatusWriter // upward live-state channel; nil = not publishing status
}

// NewRuntime wires the tap loop. locks maps portal code to its lock driver; it
// may be nil/empty when locks are armed later via SetLock (the portal reconciler
// does this as portals appear in policy). The map is copied, so the caller keeps
// ownership of theirs. input is the door-monitoring source (DPS/REX); it may be
// nil on a controller without door monitoring wired.
func NewRuntime(location string, store *PolicyStore, reader drivers.ReaderDriver, input drivers.DoorInput, locks map[string]drivers.LockDriver, emit Emitter, subs subjects.Subjects, log *logger.Logger, m *metrics.Metrics) *Runtime {
	owned := make(map[string]drivers.LockDriver, len(locks))
	for code, lock := range locks {
		owned[code] = lock
	}
	return &Runtime{
		location:   location,
		store:      store,
		reader:     reader,
		input:      input,
		locks:      owned,
		emit:       emit,
		subs:       subs,
		log:        log.With("component", "runtime"),
		m:          m,
		overrides:  make(map[string]string),
		fire:       make(map[string]bool),
		monitors:   make(map[string]*doorMonitor),
		shadow:     make(map[string]portalShadow),
		auxOutputs: make(map[string]*auxOutputState),
		auxInputs:  make(map[string]*auxInputState),
	}
}

// SetStatusWriter attaches the upward status channel. Optional: when nil (e.g. in
// tests) the runtime simply publishes no live state. Set once at wiring time.
func (r *Runtime) SetStatusWriter(w *StatusWriter) { r.statusWriter = w }

// SetLock arms (or replaces) the lock driver for a portal. Called by the portal
// reconciler when a portal appears in or changes type within policy.
func (r *Runtime) SetLock(portal string, lock drivers.LockDriver) {
	r.mu.Lock()
	r.locks[portal] = lock
	r.mu.Unlock()
}

// DeleteLock disarms the lock driver for a portal. Called by the reconciler when
// a portal is unassigned or removed from policy. It also tears down the portal's
// door monitor (and its DOTL timer), tying monitor lifecycle to arming.
func (r *Runtime) DeleteLock(portal string) {
	r.mu.Lock()
	delete(r.locks, portal)
	delete(r.shadow, portal)
	if m := r.monitors[portal]; m != nil {
		if m.timer != nil {
			m.timer.Stop()
		}
		delete(r.monitors, portal)
	}
	r.mu.Unlock()
	// The writer owns its keys' lifecycle: drop the shadow when the portal leaves.
	if r.statusWriter != nil {
		r.statusWriter.DeletePortal(portal)
	}
}

// lockFor returns the lock driver for a portal under the read lock. The tap loop
// and command handlers run concurrently with the reconciler mutating r.locks, so
// every read of the map goes through here.
func (r *Runtime) lockFor(portal string) (drivers.LockDriver, bool) {
	r.mu.RLock()
	lock, ok := r.locks[portal]
	r.mu.RUnlock()
	return lock, ok
}

// applyHold reconciles one portal's physical strike hold to its effective
// posture: the strike is held open only while effectively unlocked (B). Every
// other posture (secure/free_access/lockdown/disabled) is enforced lazily at the
// next tap, so it just releases the hold. An indeterminate posture (auto schedule
// not loaded yet) keeps the previous hold to avoid flapping during a re-sync.
// A no-op for portals this controller does not drive.
func (r *Runtime) applyHold(portal string, at time.Time) {
	lock, ok := r.lockFor(portal)
	if !ok {
		return
	}
	posture, resolved := r.effectivePosture(portal, at)
	if resolved {
		if err := lock.SetHeld(posture == policy.PostureUnlocked); err != nil {
			r.log.Error("failed to set lock hold", "portal", portal, "error", err)
		}
	}
	// Publish live status regardless of hold resolution: an unresolved posture
	// still reports the safe standing value, and door/held are independent of it.
	r.writeStatus(portal, at)
}

// writeStatus publishes a portal's current shadow to the upward status channel,
// but only when its door/posture/held actually change — so the periodic hold-eval
// reconcile never churns the bucket with timestamp-only updates. A no-op without
// a status writer (e.g. in tests).
func (r *Runtime) writeStatus(portal string, at time.Time) {
	if r.statusWriter == nil {
		return
	}
	posture, _ := r.effectivePosture(portal, at) // standing value if unresolved — a fine display value
	location, _ := r.portalAddr(portal)

	r.mu.Lock()
	door := statuskv.DoorUnknown
	held := false
	if m := r.monitors[portal]; m != nil {
		held = m.held
		if r.input != nil && m.dpsSeen {
			if m.open {
				door = statuskv.DoorOpen
			} else {
				door = statuskv.DoorClosed
			}
		}
	}
	cur := portalShadow{door: door, posture: posture, held: held}
	if prev, ok := r.shadow[portal]; ok && prev == cur {
		r.mu.Unlock()
		return // unchanged — skip the write (no churn on the hold-eval tick)
	}
	r.shadow[portal] = cur
	r.mu.Unlock()

	r.statusWriter.SetPortal(portal, location, door, posture, held, at)
}

// reconcileHolds reconciles every driven portal's strike hold to effective
// posture. It snapshots the portal set under the read lock, then applies holds
// without holding it (SetHeld may touch hardware). Driven off the hold-eval
// ticker and exercised directly by tests.
func (r *Runtime) reconcileHolds(at time.Time) {
	r.mu.RLock()
	portals := make([]string, 0, len(r.locks))
	for code := range r.locks {
		portals = append(portals, code)
	}
	r.mu.RUnlock()
	for _, portal := range portals {
		r.applyHold(portal, at)
	}
}

// ApplyHold reconciles a single portal's strike hold to its current effective
// posture immediately. The portal reconciler calls it right after arming so a
// portal's physical state is correct at once, not only on the next hold-eval tick.
func (r *Runtime) ApplyHold(portal string) {
	r.applyHold(portal, time.Now().UTC())
}

// Run consumes taps and door inputs until the context is cancelled or the reader
// channel closes. A nil DoorInput yields a nil channel, which is never selected.
func (r *Runtime) Run(ctx context.Context) error {
	taps := r.reader.Taps()
	var inputs <-chan drivers.InputEvent
	if r.input != nil {
		inputs = r.input.Inputs()
	}
	holdTicker := time.NewTicker(holdEvalInterval)
	defer holdTicker.Stop()
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
		case ev, ok := <-inputs:
			if !ok {
				inputs = nil // input source closed; stop selecting it
				continue
			}
			r.handleInput(ev)
		case <-holdTicker.C:
			// No-event fallback: flip scheduled-posture holds at window boundaries.
			r.reconcileHolds(time.Now().UTC())
		}
	}
}

func (r *Runtime) handleTap(tap drivers.Tap) {
	posture, _ := r.effectivePosture(tap.Portal, tap.At) // unresolved falls back to standing (fail-safe)
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
		// Open the grant window so the imminent door-open reads as authorized,
		// not forced.
		r.noteGrant(tap.Portal, tap.At)
		if lock, ok := r.lockFor(tap.Portal); ok {
			if err := lock.Pulse(d.Pulse); err != nil {
				r.log.Error("lock pulse failed", "portal", tap.Portal, "error", err)
			}
		} else {
			r.log.Warn("granted but no lock driver for portal", "portal", tap.Portal)
		}
	}
}

// handleInput processes one door-monitoring transition (DPS/REX). Runs on the
// run loop goroutine.
func (r *Runtime) handleInput(ev drivers.InputEvent) {
	switch ev.Kind {
	case drivers.InputDPS:
		r.handleDPS(ev.Portal, ev.Closed, ev.At)
	case drivers.InputREX:
		if ev.Active {
			r.noteREX(ev.Portal, ev.At)
		}
	case drivers.InputAux:
		r.setAuxInput(ev.Portal, ev.Active, ev.At)
	default:
		r.log.Warn("unknown door input kind; ignoring", "portal", ev.Portal, "kind", ev.Kind)
	}
}

// handleDPS folds a door-position transition into the monitor. A closed→open
// with no active grant/REX is forced; an authorized open arms the DOTL timer;
// open→close stops it and clears any held-open alarm. The decision is taken under
// the lock, but events are emitted after releasing it (EmitAlarm takes the read
// lock, and RWMutex is not reentrant).
func (r *Runtime) handleDPS(portal string, closed bool, at time.Time) {
	newOpen := !closed
	var emitForced, emitHeldClear bool

	r.mu.Lock()
	m := r.monitorFor(portal)
	m.dpsSeen = true // any edge resolves the door from "unknown"
	if newOpen != m.open {
		m.open = newOpen
		if newOpen {
			if at.Before(m.grantUntil) || at.Before(m.rexUntil) {
				r.scheduleDOTL(portal, m) // authorized: watch for held-open
			} else {
				emitForced = true
			}
		} else {
			if m.timer != nil {
				m.timer.Stop()
				m.timer = nil
			}
			emitHeldClear = m.held
			m.held = false
		}
	}
	r.mu.Unlock()

	if emitForced {
		r.EmitAlarm(portal, AlarmForced, at)
	}
	if emitHeldClear {
		r.EmitAlarm(portal, AlarmHeldClear, at)
	}
	// Publish the current door state (deduped): also surfaces the first edge of a
	// non-transition (e.g. a "closed" report that confirms the unknown state).
	r.writeStatus(portal, at)
}

// scheduleDOTL arms the door-open-too-long timer from the portal's binding. Must
// be called holding r.mu. A non-positive held_open_seconds disables held-open
// detection for the portal.
func (r *Runtime) scheduleDOTL(portal string, m *doorMonitor) {
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	b, ok := r.store.Binding(portal)
	if !ok || b.HeldOpenSeconds <= 0 {
		return
	}
	d := time.Duration(b.HeldOpenSeconds) * heldOpenUnit
	m.timer = time.AfterFunc(d, func() { r.onDOTL(portal) })
}

// onDOTL fires when a door has been open past its threshold. Emits a held-open
// alarm once per open episode, only if the door is still open. Runs on the timer
// goroutine.
func (r *Runtime) onDOTL(portal string) {
	r.mu.Lock()
	m := r.monitors[portal]
	if m == nil || !m.open || m.held {
		r.mu.Unlock()
		return
	}
	m.held = true
	r.mu.Unlock()
	now := time.Now().UTC()
	r.EmitAlarm(portal, AlarmHeld, now)
	r.writeStatus(portal, now)
}

// noteGrant opens the authorized-open window after a grant or commanded unlock.
func (r *Runtime) noteGrant(portal string, at time.Time) {
	r.mu.Lock()
	r.monitorFor(portal).grantUntil = at.Add(accessGrace)
	r.mu.Unlock()
}

// noteREX opens the authorized-open window after a request-to-exit press, so an
// egress doesn't read as forced.
func (r *Runtime) noteREX(portal string, at time.Time) {
	r.mu.Lock()
	r.monitorFor(portal).rexUntil = at.Add(accessGrace)
	r.mu.Unlock()
}

// monitorFor returns the portal's door monitor, creating it on first use. Must be
// called holding r.mu.
func (r *Runtime) monitorFor(portal string) *doorMonitor {
	m := r.monitors[portal]
	if m == nil {
		m = &doorMonitor{}
		r.monitors[portal] = m
	}
	return m
}

// effectivePosture returns the effective posture for a portal at the given
// instant — a runtime command override, else the scheduled posture while its
// window is open, else the standing posture (resolved together under the store
// lock). The bool is false only when an auto_schedule is configured but not yet
// loaded: the posture is still the safe standing value, but the hold reconciler
// treats it as "keep previous" rather than flap. Unknown portals return ("", true).
func (r *Runtime) effectivePosture(portal string, at time.Time) (string, bool) {
	r.mu.RLock()
	override := r.overrides[portal] // "" when absent
	r.mu.RUnlock()
	return r.store.ResolvePosture(portal, override, at)
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

// drives reports whether this controller physically drives the given portal (it
// has a lock for it). Command subscriptions are location-wildcarded, so a
// controller hears commands for every portal at its location, including ones
// other controllers drive; acting on those would double-emit state events and
// stray overrides. Commands for portals we don't drive are silently ignored.
func (r *Runtime) drives(portal string) bool {
	_, ok := r.lockFor(portal)
	return ok
}

// SetPosture installs a runtime posture override for a portal and emits a state
// event. Used by the command handler. The override is operational state and is
// never written back to PocketBase. Ignored for portals this controller does
// not drive (another controller at the location owns them).
func (r *Runtime) SetPosture(portal, posture, actor, reason string, at time.Time) {
	if !r.drives(portal) {
		r.log.Debug("ignoring posture command for portal not driven here", "portal", portal)
		return
	}
	r.mu.Lock()
	r.overrides[portal] = posture
	r.mu.Unlock()
	r.applyHold(portal, at) // reflect the override on the strike now (lockdown is instant)
	r.emitState(portal, posture, actor, reason, at)
}

// ClearPosture removes a runtime override, reverting to the standing posture,
// and emits a state event reflecting the now-effective posture. Ignored for
// portals this controller does not drive.
func (r *Runtime) ClearPosture(portal, actor, reason string, at time.Time) {
	if !r.drives(portal) {
		r.log.Debug("ignoring posture-clear command for portal not driven here", "portal", portal)
		return
	}
	r.mu.Lock()
	delete(r.overrides, portal)
	r.mu.Unlock()
	r.applyHold(portal, at) // strike follows the now-effective (scheduled/standing) posture
	effective, _ := r.effectivePosture(portal, at)
	r.emitState(portal, effective, actor, reason, at)
}

// Grant momentarily energizes the strike for a portal (a command-driven pulse,
// distinct from a standing posture change) and emits a tap-style audit event so
// the operator-initiated open is attributable in the timeline. A non-positive
// seconds falls back to the portal's configured pulse.
func (r *Runtime) Grant(portal string, seconds int, actor, reason string) {
	if !r.drives(portal) {
		r.log.Debug("ignoring grant command for portal not driven here", "portal", portal)
		return
	}
	if seconds <= 0 {
		if ap, ok := r.store.Portal(portal); ok {
			seconds = ap.PulseSeconds
		}
	}
	lock, _ := r.lockFor(portal) // drives() guaranteed presence above
	if err := lock.Pulse(seconds); err != nil {
		r.log.Error("command grant pulse failed", "portal", portal, "error", err)
		return
	}
	now := time.Now().UTC()
	// A commanded grant is an authorized open, like a credential grant.
	r.noteGrant(portal, now)
	r.log.Info("command grant", "portal", portal, "seconds", seconds, "actor", actor, "reason", reason)

	// Emit an audit event: operator-initiated grants belong in the access trail.
	if location, ptype := r.portalAddr(portal); ptype != "" {
		if err := r.emit.Emit(r.subs.EventTap(location, ptype, portal), TapEvent{
			User:   actor,
			Allow:  true,
			Reason: policy.ReasonAllowCommandGrant,
			TS:     now.Format(time.RFC3339),
		}); err != nil {
			r.log.Error("failed to emit command-grant event", "portal", portal, "error", err)
		} else {
			r.m.IncEventPublished("tap")
		}
	}
}

// SetAuxOutput arms (or replaces) a driven aux output relay and publishes its
// initial (de-energized) status. Called by the AuxManager on arm.
func (r *Runtime) SetAuxOutput(code, location string, pulseSeconds int, lock drivers.LockDriver) {
	r.mu.Lock()
	r.auxOutputs[code] = &auxOutputState{lock: lock, location: location, pulseSeconds: pulseSeconds}
	r.mu.Unlock()
	if r.statusWriter != nil {
		r.statusWriter.SetAuxOutput(code, location, false, time.Now().UTC())
	}
}

// DeleteAuxOutput disarms a driven aux output and drops its shadow key.
func (r *Runtime) DeleteAuxOutput(code string) {
	r.mu.Lock()
	delete(r.auxOutputs, code)
	r.mu.Unlock()
	if r.statusWriter != nil {
		r.statusWriter.DeleteAuxOutput(code)
	}
}

// ArmAuxInput records a driven aux input and publishes its initial (inactive)
// status. Called by the AuxManager on arm; transitions arrive via handleInput.
func (r *Runtime) ArmAuxInput(code, location string) {
	r.mu.Lock()
	r.auxInputs[code] = &auxInputState{location: location}
	r.mu.Unlock()
	if r.statusWriter != nil {
		r.statusWriter.SetAuxInput(code, location, false, time.Now().UTC())
	}
}

// DeleteAuxInput disarms a driven aux input and drops its shadow key.
func (r *Runtime) DeleteAuxInput(code string) {
	r.mu.Lock()
	delete(r.auxInputs, code)
	r.mu.Unlock()
	if r.statusWriter != nil {
		r.statusWriter.DeleteAuxInput(code)
	}
}

// DriveOutput drives a named aux output relay. "on"/"off" set the standing held
// state; "pulse" energizes momentarily (seconds<=0 falls back to the configured
// pulse). Ignored for aux outputs this controller does not drive (commands are
// location-wildcarded, like posture/grant).
func (r *Runtime) DriveOutput(code, action string, seconds int, actor, reason string) {
	r.mu.RLock()
	st, ok := r.auxOutputs[code]
	r.mu.RUnlock()
	if !ok {
		r.log.Debug("ignoring output command for aux output not driven here", "code", code)
		return
	}
	now := time.Now().UTC()
	switch action {
	case "on":
		if err := st.lock.SetHeld(true); err != nil {
			r.log.Error("aux output on failed", "code", code, "error", err)
			return
		}
		r.setAuxEnergized(code, st, true, now)
	case "off":
		if err := st.lock.SetHeld(false); err != nil {
			r.log.Error("aux output off failed", "code", code, "error", err)
			return
		}
		r.setAuxEnergized(code, st, false, now)
	case "pulse":
		sec := seconds
		if sec <= 0 {
			sec = st.pulseSeconds
		}
		if err := st.lock.Pulse(sec); err != nil {
			r.log.Error("aux output pulse failed", "code", code, "error", err)
			return
		}
		// Momentary: the standing energized state is unchanged.
	default:
		r.log.Warn("unknown aux output action; ignoring", "code", code, "action", action)
		return
	}
	r.log.Info("aux output driven", "code", code, "action", action, "actor", actor, "reason", reason)
}

// setAuxEnergized updates an aux output's standing state and publishes it.
func (r *Runtime) setAuxEnergized(code string, st *auxOutputState, energized bool, at time.Time) {
	r.mu.Lock()
	st.energized = energized
	loc := st.location
	r.mu.Unlock()
	if r.statusWriter != nil {
		r.statusWriter.SetAuxOutput(code, loc, energized, at)
	}
}

// setAuxInput records an aux input transition (deduped) and publishes it.
func (r *Runtime) setAuxInput(code string, active bool, at time.Time) {
	r.mu.Lock()
	st, ok := r.auxInputs[code]
	if !ok {
		r.mu.Unlock()
		r.log.Debug("aux input event for aux not driven here; ignoring", "code", code)
		return
	}
	if st.active == active {
		r.mu.Unlock()
		return // no change
	}
	st.active = active
	loc := st.location
	r.mu.Unlock()
	if r.statusWriter != nil {
		r.statusWriter.SetAuxInput(code, loc, active, at)
	}
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
