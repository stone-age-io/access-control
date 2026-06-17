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

// siteEntry pairs a site record with its parsed timezone (resolved once on
// apply so the hot decision path never calls time.LoadLocation under the lock).
type siteEntry struct {
	site policykv.Site
	loc  *time.Location
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

	mu    sync.RWMutex
	sites map[string]siteEntry
	graph policy.Policy

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
		kv:    kv,
		log:   log.With("component", "policystore"),
		m:     m,
		sites: make(map[string]siteEntry),
		graph: policy.Policy{
			Schedules: make(map[string]policy.Schedule),
			Points:    make(map[string]policy.AccessPoint),
			Users:     make(map[string]policy.User),
			Roles:     make(map[string]policy.Role),
			Groups:    make(map[string]policy.AccessGroup),
			Creds:     make(map[string]policy.Credential),
		},
		ready: make(chan struct{}),
	}
}

// Decide resolves the access point's site timezone and delegates to the pure
// policy.Decide under a read lock. Effective posture is supplied by the caller
// (standing value, possibly overridden by a runtime command).
func (s *PolicyStore) Decide(posture, cred, point string, atUTC time.Time) policy.Decision {
	s.mu.RLock()
	defer s.mu.RUnlock()

	loc := time.UTC
	if ap, ok := s.graph.Points[point]; ok {
		if se, ok := s.sites[ap.Site]; ok && se.loc != nil {
			loc = se.loc
		}
	}
	return policy.Decide(&s.graph, loc, posture, cred, point, atUTC)
}

// Point returns a copy of the access point and whether it is known. The tap
// loop uses it to resolve the standing posture and the site (for event subjects).
func (s *PolicyStore) Point(code string) (policy.AccessPoint, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ap, ok := s.graph.Points[code]
	return ap, ok
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
				// Initial sync complete: all existing keys delivered.
				s.readyOnce.Do(func() { close(s.ready) })
				s.m.SetKVWatchState(true)
				s.log.Info("policy KV initial sync complete")
				continue
			}

			switch entry.Operation() {
			case jetstream.KeyValuePut:
				s.apply(entry.Key(), entry.Value())
			case jetstream.KeyValueDelete, jetstream.KeyValuePurge:
				s.remove(entry.Key())
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
	case strings.HasPrefix(key, policykv.PrefixSite):
		var w policykv.Site
		if !s.unmarshal(key, value, &w) {
			return
		}
		loc, err := time.LoadLocation(w.Timezone)
		if err != nil {
			s.log.Error("policystore: bad timezone, using UTC", "site", w.Code, "tz", w.Timezone, "error", err)
			loc = time.UTC
		}
		s.sites[w.Code] = siteEntry{site: w, loc: loc}

	case strings.HasPrefix(key, policykv.PrefixSched):
		var w policykv.Schedule
		if !s.unmarshal(key, value, &w) {
			return
		}
		s.graph.Schedules[w.Code] = toSchedule(w)

	case strings.HasPrefix(key, policykv.PrefixPoint):
		var w policykv.AccessPoint
		if !s.unmarshal(key, value, &w) {
			return
		}
		s.graph.Points[w.Code] = policy.AccessPoint{
			Code: w.Code, Site: w.Site, Posture: w.Posture, PulseSeconds: w.PulseSeconds,
		}

	case strings.HasPrefix(key, policykv.PrefixGroup):
		var w policykv.AccessGroup
		if !s.unmarshal(key, value, &w) {
			return
		}
		s.graph.Groups[w.Code] = policy.AccessGroup{
			Code: w.Code, Points: toSet(w.Points), Schedule: w.Schedule,
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
		s.graph.Creds[w.Value] = policy.Credential{Value: w.Value, User: w.User, Status: w.Status}

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
	case strings.HasPrefix(key, policykv.PrefixSite):
		delete(s.sites, strings.TrimPrefix(key, policykv.PrefixSite))
	case strings.HasPrefix(key, policykv.PrefixSched):
		delete(s.graph.Schedules, strings.TrimPrefix(key, policykv.PrefixSched))
	case strings.HasPrefix(key, policykv.PrefixPoint):
		delete(s.graph.Points, strings.TrimPrefix(key, policykv.PrefixPoint))
	case strings.HasPrefix(key, policykv.PrefixGroup):
		delete(s.graph.Groups, strings.TrimPrefix(key, policykv.PrefixGroup))
	case strings.HasPrefix(key, policykv.PrefixRole):
		delete(s.graph.Roles, strings.TrimPrefix(key, policykv.PrefixRole))
	case strings.HasPrefix(key, policykv.PrefixUser):
		delete(s.graph.Users, strings.TrimPrefix(key, policykv.PrefixUser))
	case strings.HasPrefix(key, policykv.PrefixCred):
		delete(s.graph.Creds, strings.TrimPrefix(key, policykv.PrefixCred))
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
	return policy.Schedule{Windows: windows}
}

func toSet(codes []string) map[string]struct{} {
	set := make(map[string]struct{}, len(codes))
	for _, c := range codes {
		set[c] = struct{}{}
	}
	return set
}
