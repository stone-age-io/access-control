// Package controller is the edge runtime: it watches the policy KV keyspace
// into in-memory maps (PolicyStore), decides credential presentations locally
// with the pure policy.Decide, drives reader/lock hardware, and emits events.
package controller

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/metrics"
	"github.com/stone-age-io/access-control/internal/policy"
	"github.com/stone-age-io/access-control/internal/policykv"
	"github.com/stone-age-io/access-control/internal/statuskv"
)

// KV watch re-establishment backoff bounds. Vars (not consts) so tests can
// shorten them.
var (
	kvWatchRetryBaseDelay = 500 * time.Millisecond
	kvWatchRetryMaxDelay  = 30 * time.Second
)

func nextKVWatchBackoff(d time.Duration) time.Duration {
	d *= 2
	if d > kvWatchRetryMaxDelay {
		return kvWatchRetryMaxDelay
	}
	return d
}

// locationEntry pairs a location record with its parsed timezone (resolved once
// on apply so the hot decision path never calls time.LoadLocation under the lock).
type locationEntry struct {
	location policykv.Location
	loc      *time.Location
}

// Binding is the controller-side hardware view of a portal: which edge box drives
// it and the *logical* relay/input indices on that box. Kept separate from
// policy.Portal so the pure decision graph stays free of hardware concerns. The
// model template (M3) maps these indices to physical lines.
type Binding struct {
	Controller      string
	LockRelay       int
	DpsInput        int
	RexInput        int
	HeldOpenSeconds int
	ReaderAddress   int // OSDP PD address of this portal's reader (reader=="osdp")
}

// PolicyStore holds the whole-org policy graph in plain maps behind an RWMutex
// and keeps it in sync with the ACC_POLICY KV bucket. Door-tap rates are low,
// so a read lock on the decision path is cheaper and simpler than lock-free
// snapshotting. Eventual consistency is fail-safe: an unknown credential, a
// user referencing a not-yet-synced role, or no policy at all all deny.
type PolicyStore struct {
	kv  jetstream.KeyValue
	log *logger.Logger
	m   *metrics.Metrics

	mu             sync.RWMutex
	locations      map[string]locationEntry
	graph          policy.Policy
	controllers    map[string]policykv.Controller // controller code -> controller (for model/location)
	bindings       map[string]Binding             // portal code -> hardware binding
	holidayRecords map[string]policykv.Holiday    // holiday id -> record (graph.Holidays is rebuilt from this)
	auxInputs      map[string]policykv.AuxInput   // aux input code -> record
	auxOutputs     map[string]policykv.AuxOutput  // aux output code -> record

	onChange func() // fired (off the lock) after each applied change and each sync

	ready     chan struct{}
	readyOnce sync.Once
	wg        sync.WaitGroup

	watcherMu  sync.Mutex
	watcher    jetstream.KeyWatcher
	cancel     context.CancelFunc
	newWatcher func(context.Context) (jetstream.KeyWatcher, error)
	watchOnce  sync.Once
	watchErr   error
}

// NewPolicyStore creates a store backed by the given KV bucket handle (the
// caller ensures the bucket exists).
func NewPolicyStore(kv jetstream.KeyValue, log *logger.Logger, m *metrics.Metrics) *PolicyStore {
	return &PolicyStore{
		kv:             kv,
		log:            log.With("component", "policystore"),
		m:              m,
		locations:      make(map[string]locationEntry),
		controllers:    make(map[string]policykv.Controller),
		bindings:       make(map[string]Binding),
		holidayRecords: make(map[string]policykv.Holiday),
		auxInputs:      make(map[string]policykv.AuxInput),
		auxOutputs:     make(map[string]policykv.AuxOutput),
		graph: policy.Policy{
			Schedules: make(map[string]policy.Schedule),
			Portals:   make(map[string]policy.Portal),
			Users:     make(map[string]policy.User),
			Roles:     make(map[string]policy.Role),
			Groups:    make(map[string]policy.AccessGroup),
			Creds:     make(map[string]policy.Credential),
			Holidays:  make(map[string]policy.HolidaySet),
		},
		ready: make(chan struct{}),
	}
}

// Decide resolves the portal's location timezone and delegates to the pure
// policy.Decide under a read lock. Effective posture is supplied by the caller
// (standing value, possibly overridden by a runtime command).
func (s *PolicyStore) Decide(posture, cred, portal string, atUTC time.Time) policy.Decision {
	s.mu.RLock()
	defer s.mu.RUnlock()

	loc := time.UTC
	if ap, ok := s.graph.Portals[portal]; ok {
		if le, ok := s.locations[ap.Location]; ok && le.loc != nil {
			loc = le.loc
		}
	}
	return policy.Decide(&s.graph, loc, posture, cred, portal, atUTC)
}

// ResolvePosture computes a portal's effective posture under one read lock:
//
//  1. a runtime command override (passed in; "" means none) wins;
//  2. else, while the portal's auto_schedule window is open, its auto_posture;
//  3. else the standing posture.
//
// source reports which of the three produced the posture (a statuskv.PostureSource*
// constant), so the status shadow — and the UI — can mark a manual override or an
// active scheduled posture distinctly from the standing state.
//
// It also reports whether the result is determinate. resolved=false means the
// portal has an auto_schedule whose schedule record or timezone is not loaded yet
// (e.g. mid reconnect re-sync): the returned posture is the safe standing value
// (so the decision path stays fail-safe), but the physical-hold reconciler should
// keep the previous hold rather than flap. An unknown portal resolves to ("",
// standing, true) — Decide denies it regardless.
func (s *PolicyStore) ResolvePosture(portal, override string, atUTC time.Time) (posture, source string, resolved bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if override != "" {
		return override, statuskv.PostureSourceOverride, true
	}
	ap, ok := s.graph.Portals[portal]
	if !ok {
		return "", statuskv.PostureSourceStanding, true
	}
	if ap.AutoSchedule != "" {
		sched, schedOK := s.graph.Schedules[ap.AutoSchedule]
		le, locOK := s.locations[ap.Location]
		if !schedOK || !locOK || le.loc == nil {
			return ap.Posture, statuskv.PostureSourceStanding, false // configured but unresolved: keep previous hold
		}
		if policy.ScheduleOpen(sched, le.loc, atUTC, s.graph.Holidays[ap.Location]) {
			return ap.AutoPosture, statuskv.PostureSourceScheduled, true
		}
	}
	return ap.Posture, statuskv.PostureSourceStanding, true
}

// Portal returns a copy of the portal and whether it is known. The tap loop uses
// it to resolve the standing posture, the type, and the location (for subjects).
func (s *PolicyStore) Portal(code string) (policy.Portal, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ap, ok := s.graph.Portals[code]
	return ap, ok
}

// PortalsForController returns the portals bound to the given controller code.
// The portal reconciler uses it to decide which readers/locks this box arms;
// binding lives in policy (central), so reassigning a portal to another box takes
// effect without a controller restart. Returns a fresh slice each call.
func (s *PolicyStore) PortalsForController(controllerCode string) []policy.Portal {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []policy.Portal
	for code, b := range s.bindings {
		if b.Controller != controllerCode {
			continue
		}
		if ap, ok := s.graph.Portals[code]; ok {
			out = append(out, ap)
		}
	}
	return out
}

// Binding returns a portal's hardware binding (relay/input indices) and whether
// it is known. The runtime/drivers use it to resolve which physical lines to
// drive (via the controller's model template).
func (s *PolicyStore) Binding(code string) (Binding, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.bindings[code]
	return b, ok
}

// Controller returns a controller record (for its model/location) and whether it
// is known.
func (s *PolicyStore) Controller(code string) (policykv.Controller, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.controllers[code]
	return c, ok
}

// AuxInputsForController returns the aux inputs bound to the given controller
// code. The AuxManager uses it to decide which input lines this box arms. Returns
// a fresh slice each call.
func (s *PolicyStore) AuxInputsForController(controllerCode string) []policykv.AuxInput {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []policykv.AuxInput
	for _, ai := range s.auxInputs {
		if ai.Controller == controllerCode {
			out = append(out, ai)
		}
	}
	return out
}

// AuxOutputsForController returns the aux outputs bound to the given controller code.
func (s *PolicyStore) AuxOutputsForController(controllerCode string) []policykv.AuxOutput {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []policykv.AuxOutput
	for _, ao := range s.auxOutputs {
		if ao.Controller == controllerCode {
			out = append(out, ao)
		}
	}
	return out
}

// AuxOutput returns an aux output record (for its pulse default) and whether it
// is known.
func (s *PolicyStore) AuxOutput(code string) (policykv.AuxOutput, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ao, ok := s.auxOutputs[code]
	return ao, ok
}

// SetOnChange registers a callback fired (off the store lock) after each applied
// KV change and after each sync-complete sentinel — including the re-sync that
// follows a reconnect, since WatchAll re-delivers every key. The portal
// reconciler uses it to keep its armed set in step with policy. Must be called
// before Watch (the callback is read from the watcher goroutine).
func (s *PolicyStore) SetOnChange(fn func()) { s.onChange = fn }

// notifyChange invokes the onChange callback if one is registered. Callers must
// hold no store lock (the callback reads store state).
func (s *PolicyStore) notifyChange() {
	if s.onChange != nil {
		s.onChange()
	}
}

// Watch starts the KV watcher (once). It returns immediately; the watcher runs
// in a goroutine and self-heals across connection loss. Call WaitReady to block
// until the initial sync completes.
func (s *PolicyStore) Watch(parent context.Context) error {
	s.watchOnce.Do(func() {
		watchCtx, cancel := context.WithCancel(parent)
		s.newWatcher = func(c context.Context) (jetstream.KeyWatcher, error) {
			return s.kv.WatchAll(c)
		}

		watcher, err := s.newWatcher(watchCtx)
		if err != nil {
			cancel()
			s.watchErr = err
			return
		}

		s.watcherMu.Lock()
		s.watcher = watcher
		s.cancel = cancel
		s.watcherMu.Unlock()

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.runWatch(watchCtx, watcher)
		}()
		s.log.Info("policy KV watcher started")
	})
	return s.watchErr
}

// WaitReady blocks until the initial KV sync completes (nil sentinel) or the
// context is cancelled. The controller blocks on this before arming the reader
// so it default-denies until policy has loaded.
func (s *PolicyStore) WaitReady(ctx context.Context) error {
	select {
	case <-s.ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Ready reports, without blocking, whether the initial KV sync has completed —
// i.e. whether the controller is past its boot default-deny window. The
// diagnostics page uses it to show "synced" vs "default-deny (policy not yet
// loaded)".
func (s *PolicyStore) Ready() bool {
	select {
	case <-s.ready:
		return true
	default:
		return false
	}
}

// Counts returns the number of records loaded per kind, under one read lock. It
// feeds the diagnostics page ("what actually synced to this box") and is not on
// any hot path.
func (s *PolicyStore) Counts() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]int{
		"portals":     len(s.graph.Portals),
		"credentials": len(s.graph.Creds),
		"users":       len(s.graph.Users),
		"roles":       len(s.graph.Roles),
		"groups":      len(s.graph.Groups),
		"schedules":   len(s.graph.Schedules),
		"holidays":    len(s.holidayRecords),
		"bindings":    len(s.bindings),
		"controllers": len(s.controllers),
		"auxInputs":   len(s.auxInputs),
		"auxOutputs":  len(s.auxOutputs),
	}
}

// Resync forces the watcher to re-establish. A NATS reconnect can leave the
// watcher's server-side consumer stale without closing its Updates() channel;
// stopping the current watcher closes that channel, and runWatch then re-creates
// it (WatchAll re-delivers every key, refreshing the maps). Wire this to the
// NATS reconnect handler. Safe no-op before Watch has started.
func (s *PolicyStore) Resync() {
	s.watcherMu.Lock()
	w := s.watcher
	s.watcherMu.Unlock()
	if w == nil {
		return
	}
	s.m.SetKVWatchState(false)
	if err := w.Stop(); err != nil {
		s.log.Warn("resync: failed to stop watcher", "error", err)
	}
}

// Stop stops the watcher and waits for the goroutine to exit.
func (s *PolicyStore) Stop() {
	s.watcherMu.Lock()
	watcher, cancel := s.watcher, s.cancel
	s.watcherMu.Unlock()

	if cancel != nil {
		cancel()
	}
	if watcher != nil {
		if err := watcher.Stop(); err != nil {
			s.log.Error("failed to stop policy KV watcher", "error", err)
		}
	}
	s.wg.Wait()
}

// runWatch consumes updates and self-heals across unexpected watcher closures,
// re-creating the watcher with capped exponential backoff. WatchAll re-delivers
// the current value/delete marker for every key on each (re)subscribe, so a
// recreate performs a full re-sync. Returns only on context cancellation.
func (s *PolicyStore) runWatch(ctx context.Context, watcher jetstream.KeyWatcher) {
	backoff := kvWatchRetryBaseDelay
	for {
		if s.consumeUpdates(ctx, watcher) {
			return // clean shutdown
		}
		if ctx.Err() != nil {
			return
		}

		s.m.SetKVWatchState(false)
		s.log.Error("policy KV watcher channel closed unexpectedly; re-establishing")
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}

			newWatcher, err := s.newWatcher(ctx)
			if err != nil {
				s.log.Error("failed to re-establish policy KV watcher, will retry",
					"retryIn", backoff, "error", err)
				backoff = nextKVWatchBackoff(backoff)
				continue
			}
			if ctx.Err() != nil {
				_ = newWatcher.Stop()
				return
			}

			watcher = newWatcher
			s.watcherMu.Lock()
			s.watcher = watcher
			s.watcherMu.Unlock()
			backoff = kvWatchRetryBaseDelay
			s.log.Info("policy KV watcher re-established")
			break
		}
	}
}

// consumeUpdates reads from the watcher until its Updates() channel closes.
// Returns true to terminate permanently (context cancelled), false to signal
// the caller to re-establish.
func (s *PolicyStore) consumeUpdates(ctx context.Context, watcher jetstream.KeyWatcher) bool {
	for {
		select {
		case <-ctx.Done():
			return true
		case entry, ok := <-watcher.Updates():
			if !ok {
				// Closed channel yields (nil,false) forever — stop reading it.
				if ctx.Err() != nil {
					s.log.Debug("policy KV watcher stopped")
					return true
				}
				return false
			}
			if entry == nil {
				// Sync complete: all current keys (re)delivered. Fires on boot and
				// again after every reconnect re-sync, so the reconciler reconverges.
				s.readyOnce.Do(func() { close(s.ready) })
				s.m.SetKVWatchState(true)
				s.log.Info("policy KV initial sync complete")
				s.notifyChange()
				continue
			}

			switch entry.Operation() {
			case jetstream.KeyValuePut:
				s.apply(entry.Key(), entry.Value())
				s.notifyChange()
			case jetstream.KeyValueDelete, jetstream.KeyValuePurge:
				s.remove(entry.Key())
				s.notifyChange()
			}
		}
	}
}

// apply parses one KV record and writes it into the matching map under the
// write lock. A parse error keeps the previous value for that key (fail-safe).
func (s *PolicyStore) apply(key string, value []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch {
	case strings.HasPrefix(key, policykv.PrefixLocation):
		var w policykv.Location
		if !s.unmarshal(key, value, &w) {
			return
		}
		loc, err := time.LoadLocation(w.Timezone)
		if err != nil {
			s.log.Error("policystore: bad timezone, using UTC", "location", w.Code, "tz", w.Timezone, "error", err)
			loc = time.UTC
		}
		s.locations[w.Code] = locationEntry{location: w, loc: loc}

	case strings.HasPrefix(key, policykv.PrefixSched):
		var w policykv.Schedule
		if !s.unmarshal(key, value, &w) {
			return
		}
		s.graph.Schedules[w.Code] = toSchedule(w)

	case strings.HasPrefix(key, policykv.PrefixPortal):
		var w policykv.Portal
		if !s.unmarshal(key, value, &w) {
			return
		}
		s.graph.Portals[w.Code] = policy.Portal{
			Code: w.Code, Type: w.Type, Location: w.Location,
			Posture: w.Posture, PulseSeconds: w.PulseSeconds,
			AutoPosture: w.AutoPosture, AutoSchedule: w.AutoSchedule,
		}
		s.bindings[w.Code] = Binding{
			Controller: w.Controller, LockRelay: w.LockRelay,
			DpsInput: w.DpsInput, RexInput: w.RexInput,
			HeldOpenSeconds: w.HeldOpenSeconds, ReaderAddress: w.ReaderAddress,
		}

	case strings.HasPrefix(key, policykv.PrefixController):
		var w policykv.Controller
		if !s.unmarshal(key, value, &w) {
			return
		}
		s.controllers[w.Code] = w

	case strings.HasPrefix(key, policykv.PrefixHoliday):
		var w policykv.Holiday
		if !s.unmarshal(key, value, &w) {
			return
		}
		s.holidayRecords[strings.TrimPrefix(key, policykv.PrefixHoliday)] = w
		s.rebuildHolidays()

	case strings.HasPrefix(key, policykv.PrefixGroup):
		var w policykv.AccessGroup
		if !s.unmarshal(key, value, &w) {
			return
		}
		s.graph.Groups[w.Code] = policy.AccessGroup{
			Code: w.Code, Portals: toSet(w.Portals), Schedule: w.Schedule,
		}

	case strings.HasPrefix(key, policykv.PrefixRole):
		var w policykv.Role
		if !s.unmarshal(key, value, &w) {
			return
		}
		s.graph.Roles[w.Code] = policy.Role{Code: w.Code, Groups: w.Groups}

	case strings.HasPrefix(key, policykv.PrefixUser):
		var w policykv.User
		if !s.unmarshal(key, value, &w) {
			return
		}
		s.graph.Users[w.ID] = policy.User{ID: w.ID, Status: w.Status, Roles: w.Roles}

	case strings.HasPrefix(key, policykv.PrefixCred):
		var w policykv.Credential
		if !s.unmarshal(key, value, &w) {
			return
		}
		validFrom, ok1 := parseOptionalTime(w.ValidFrom)
		validUntil, ok2 := parseOptionalTime(w.ValidUntil)
		if !ok1 || !ok2 {
			// The mirror always emits RFC 3339, so an unparseable bound means a
			// corrupt value. Fail closed: drop the credential entirely (a tap then
			// reads as deny_unknown_credential) rather than honor a half-parsed one.
			s.log.Error("policystore: credential has unparseable validity date, dropping (fail closed)",
				"key", key, "validFrom", w.ValidFrom, "validUntil", w.ValidUntil)
			delete(s.graph.Creds, w.Value)
			return
		}
		s.graph.Creds[w.Value] = policy.Credential{
			Value: w.Value, User: w.User, Status: w.Status,
			ValidFrom: validFrom, ValidUntil: validUntil,
		}

	case strings.HasPrefix(key, policykv.PrefixAuxInput):
		var w policykv.AuxInput
		if !s.unmarshal(key, value, &w) {
			return
		}
		s.auxInputs[w.Code] = w

	case strings.HasPrefix(key, policykv.PrefixAuxOutput):
		var w policykv.AuxOutput
		if !s.unmarshal(key, value, &w) {
			return
		}
		s.auxOutputs[w.Code] = w

	default:
		s.log.Warn("policystore: unknown key prefix, ignoring", "key", key)
		return
	}
	s.m.IncKVApply("put")
}

// remove deletes one KV record from the matching map under the write lock.
func (s *PolicyStore) remove(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	switch {
	case strings.HasPrefix(key, policykv.PrefixLocation):
		delete(s.locations, strings.TrimPrefix(key, policykv.PrefixLocation))
	case strings.HasPrefix(key, policykv.PrefixSched):
		delete(s.graph.Schedules, strings.TrimPrefix(key, policykv.PrefixSched))
	case strings.HasPrefix(key, policykv.PrefixPortal):
		code := strings.TrimPrefix(key, policykv.PrefixPortal)
		delete(s.graph.Portals, code)
		delete(s.bindings, code)
	case strings.HasPrefix(key, policykv.PrefixController):
		delete(s.controllers, strings.TrimPrefix(key, policykv.PrefixController))
	case strings.HasPrefix(key, policykv.PrefixHoliday):
		delete(s.holidayRecords, strings.TrimPrefix(key, policykv.PrefixHoliday))
		s.rebuildHolidays()
	case strings.HasPrefix(key, policykv.PrefixGroup):
		delete(s.graph.Groups, strings.TrimPrefix(key, policykv.PrefixGroup))
	case strings.HasPrefix(key, policykv.PrefixRole):
		delete(s.graph.Roles, strings.TrimPrefix(key, policykv.PrefixRole))
	case strings.HasPrefix(key, policykv.PrefixUser):
		delete(s.graph.Users, strings.TrimPrefix(key, policykv.PrefixUser))
	case strings.HasPrefix(key, policykv.PrefixCred):
		delete(s.graph.Creds, strings.TrimPrefix(key, policykv.PrefixCred))
	case strings.HasPrefix(key, policykv.PrefixAuxInput):
		delete(s.auxInputs, strings.TrimPrefix(key, policykv.PrefixAuxInput))
	case strings.HasPrefix(key, policykv.PrefixAuxOutput):
		delete(s.auxOutputs, strings.TrimPrefix(key, policykv.PrefixAuxOutput))
	default:
		s.log.Warn("policystore: unknown key prefix on delete, ignoring", "key", key)
		return
	}
	s.m.IncKVApply("delete")
}

// unmarshal decodes a KV value, logging and skipping on error. Must be called
// under the write lock. Returns false if the value was malformed.
func (s *PolicyStore) unmarshal(key string, value []byte, dst any) bool {
	if err := json.Unmarshal(value, dst); err != nil {
		s.log.Error("policystore: malformed KV value, keeping previous", "key", key, "error", err)
		return false
	}
	return true
}

func toSchedule(w policykv.Schedule) policy.Schedule {
	windows := make([]policy.Window, len(w.Windows))
	for i, win := range w.Windows {
		windows[i] = policy.Window{Days: win.Days, Start: win.Start, End: win.End}
	}
	return policy.Schedule{Windows: windows, ObserveHolidays: w.ObserveHolidays}
}

// rebuildHolidays regenerates the per-location HolidaySets in the graph from the
// raw holiday records. Holidays are few (dozens per org), so a full rebuild on
// each holiday change is cheaper than incremental set surgery. Must be called
// holding the write lock.
func (s *PolicyStore) rebuildHolidays() {
	out := make(map[string]policy.HolidaySet)
	for _, h := range s.holidayRecords {
		if h.Location == "" || len(h.Date) != 10 {
			continue // dangling/malformed: fail-safe skip (a missing holiday just doesn't close)
		}
		set := out[h.Location]
		if h.Recurring {
			if set.Recurring == nil {
				set.Recurring = make(map[string]struct{})
			}
			set.Recurring[h.Date[5:]] = struct{}{} // "YYYY-MM-DD" -> "MM-DD"
		} else {
			if set.Dates == nil {
				set.Dates = make(map[string]struct{})
			}
			set.Dates[h.Date] = struct{}{}
		}
		out[h.Location] = set
	}
	s.graph.Holidays = out
}

// parseOptionalTime parses an optional RFC 3339 timestamp. An empty string is a
// valid "unbounded" value (zero time, ok). A non-empty value that fails to parse
// returns ok=false so the caller can fail closed.
func parseOptionalTime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, true
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

func toSet(codes []string) map[string]struct{} {
	set := make(map[string]struct{}, len(codes))
	for _, c := range codes {
		set[c] = struct{}{}
	}
	return set
}
