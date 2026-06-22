// Package status projects the upward ACC_STATUS KV "device shadow" into the
// PocketBase point_status collection — the rebuildable read model the UI reads
// for live edge state. It is the accessd-side counterpart to the controller's
// StatusWriter, and the reverse of the policy mirror: where mirror writes config
// DOWN to KV, this watches state UP from KV.
//
// The watcher self-heals across reconnects exactly like the controller's
// PolicyStore: WatchAll re-delivers every key on each (re)subscribe, so a
// reconnect performs a full re-sync. Each sync sentinel prunes projection rows
// whose KV key is gone (a key removed while accessd was down).
package status

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/metrics"
	"github.com/stone-age-io/access-control/internal/statuskv"
)

const collection = "point_status"

// KV watch re-establishment backoff bounds. Vars so tests can shorten them.
var (
	kvWatchRetryBaseDelay = 500 * time.Millisecond
	kvWatchRetryMaxDelay  = 30 * time.Second
)

func nextBackoff(d time.Duration) time.Duration {
	d *= 2
	if d > kvWatchRetryMaxDelay {
		return kvWatchRetryMaxDelay
	}
	return d
}

// Projector watches ACC_STATUS and maintains the point_status collection.
type Projector struct {
	app core.App
	kv  jetstream.KeyValue
	log *logger.Logger
	m   *metrics.Metrics

	wg         sync.WaitGroup
	watcherMu  sync.Mutex
	watcher    jetstream.KeyWatcher
	cancel     context.CancelFunc
	newWatcher func(context.Context) (jetstream.KeyWatcher, error)
	watchOnce  sync.Once
	watchErr   error
}

// New creates a projector. The caller ensures the bucket exists (accessd owns
// creation via EnsureKVBucket).
func New(app core.App, kv jetstream.KeyValue, log *logger.Logger, m *metrics.Metrics) *Projector {
	return &Projector{app: app, kv: kv, log: log.With("component", "status"), m: m}
}

// Start launches the watcher (once). It returns immediately; the watcher runs in
// a goroutine and self-heals across connection loss.
func (p *Projector) Start(parent context.Context) error {
	p.watchOnce.Do(func() {
		ctx, cancel := context.WithCancel(parent)
		p.newWatcher = func(c context.Context) (jetstream.KeyWatcher, error) {
			return p.kv.WatchAll(c)
		}
		w, err := p.newWatcher(ctx)
		if err != nil {
			cancel()
			p.watchErr = err
			return
		}
		p.watcherMu.Lock()
		p.watcher = w
		p.cancel = cancel
		p.watcherMu.Unlock()

		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			p.runWatch(ctx, w)
		}()
		p.log.Info("status projector started")
	})
	return p.watchErr
}

// Resync forces the watcher to re-establish (wire to the NATS reconnect handler).
// Stopping the current watcher closes its Updates() channel; runWatch re-creates
// it, and WatchAll re-delivers every key. Safe no-op before Start.
func (p *Projector) Resync() {
	p.watcherMu.Lock()
	w := p.watcher
	p.watcherMu.Unlock()
	if w == nil {
		return
	}
	p.m.SetKVWatchState(false)
	if err := w.Stop(); err != nil {
		p.log.Warn("resync: failed to stop watcher", "error", err)
	}
}

// Stop stops the watcher and waits for the goroutine to exit.
func (p *Projector) Stop() {
	p.watcherMu.Lock()
	w, cancel := p.watcher, p.cancel
	p.watcherMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if w != nil {
		if err := w.Stop(); err != nil {
			p.log.Error("failed to stop status watcher", "error", err)
		}
	}
	p.wg.Wait()
}

func (p *Projector) runWatch(ctx context.Context, w jetstream.KeyWatcher) {
	backoff := kvWatchRetryBaseDelay
	for {
		if p.consumeUpdates(ctx, w) {
			return // clean shutdown
		}
		if ctx.Err() != nil {
			return
		}
		p.m.SetKVWatchState(false)
		p.log.Error("status KV watcher channel closed unexpectedly; re-establishing")
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			nw, err := p.newWatcher(ctx)
			if err != nil {
				p.log.Error("failed to re-establish status watcher, will retry", "retryIn", backoff, "error", err)
				backoff = nextBackoff(backoff)
				continue
			}
			if ctx.Err() != nil {
				_ = nw.Stop()
				return
			}
			w = nw
			p.watcherMu.Lock()
			p.watcher = w
			p.watcherMu.Unlock()
			backoff = kvWatchRetryBaseDelay
			p.log.Info("status KV watcher re-established")
			break
		}
	}
}

// consumeUpdates reads the watcher until its channel closes. The seen set
// accumulates every key delivered before the sync sentinel (WatchAll replays all
// current keys first), so the sentinel can prune projection rows with no live key.
// Returns true to terminate permanently (context cancelled).
func (p *Projector) consumeUpdates(ctx context.Context, w jetstream.KeyWatcher) bool {
	seen := make(map[string]struct{})
	for {
		select {
		case <-ctx.Done():
			return true
		case entry, ok := <-w.Updates():
			if !ok {
				if ctx.Err() != nil {
					return true
				}
				return false // re-establish
			}
			if entry == nil {
				p.m.SetKVWatchState(true)
				p.prune(seen)
				p.log.Info("status KV sync complete", "keys", len(seen))
				continue
			}
			switch entry.Operation() {
			case jetstream.KeyValuePut:
				seen[entry.Key()] = struct{}{}
				p.apply(entry.Key(), entry.Value())
			case jetstream.KeyValueDelete, jetstream.KeyValuePurge:
				delete(seen, entry.Key())
				p.removeKey(entry.Key())
			}
		}
	}
}

// row holds the projection columns derived from one status key/value. Pure
// (no DB), so it is unit-testable.
type row struct {
	key, code, kind, state, posture string
	postureSource                   string // provenance of posture: standing|scheduled|override
	held                            bool
	controller, location, changed   string
	payload                         map[string]any
}

// rowFor parses one status key/value into projection columns. ok is false for an
// unrecognized prefix or a malformed value (skip + log, fail-safe).
func rowFor(key string, value []byte) (row, bool) {
	kind, code, ok := statuskv.Parse(key)
	if !ok {
		return row{}, false
	}
	r := row{key: key, code: code, kind: kind}

	var payload map[string]any
	if err := json.Unmarshal(value, &payload); err != nil {
		return row{}, false
	}
	r.payload = payload

	switch kind {
	case statuskv.KindPortal:
		var ps statuskv.PortalStatus
		if err := json.Unmarshal(value, &ps); err != nil {
			return row{}, false
		}
		r.state = ps.Door
		r.posture = ps.Posture
		r.postureSource = ps.Source
		r.held = ps.Held
		r.controller = ps.Controller
		r.location = ps.Location
		r.changed = ps.UpdatedAt
	case statuskv.KindAuxInput:
		var ai statuskv.AuxInputStatus
		if err := json.Unmarshal(value, &ai); err != nil {
			return row{}, false
		}
		r.state = auxInputState(ai.Active)
		r.controller = ai.Controller
		r.location = ai.Location
		r.changed = ai.UpdatedAt
	case statuskv.KindAuxOutput:
		var ao statuskv.AuxOutputStatus
		if err := json.Unmarshal(value, &ao); err != nil {
			return row{}, false
		}
		r.state = auxOutputState(ao.Energized)
		r.controller = ao.Controller
		r.location = ao.Location
		r.changed = ao.UpdatedAt
	default:
		return row{}, false
	}
	return r, true
}

func auxInputState(active bool) string {
	if active {
		return "active"
	}
	return "inactive"
}

func auxOutputState(energized bool) string {
	if energized {
		return "energized"
	}
	return "off"
}

// apply upserts the projection row for a status key. A find-or-create keyed by
// the KV key (globally unique across kinds).
func (p *Projector) apply(key string, value []byte) {
	r, ok := rowFor(key, value)
	if !ok {
		p.log.Warn("status: unrecognized or malformed key, skipping", "key", key)
		return
	}

	rec, err := p.app.FindFirstRecordByData(collection, "key", key)
	if errors.Is(err, sql.ErrNoRows) {
		col, cerr := p.app.FindCollectionByNameOrId(collection)
		if cerr != nil {
			p.log.Error("status: point_status collection missing", "error", cerr)
			return
		}
		rec = core.NewRecord(col)
	} else if err != nil {
		p.log.Error("status: lookup failed", "key", key, "error", err)
		return
	}

	rec.Set("key", r.key)
	rec.Set("code", r.code)
	rec.Set("kind", r.kind)
	rec.Set("state", r.state)
	rec.Set("posture", r.posture)
	rec.Set("posture_source", r.postureSource)
	rec.Set("held", r.held)
	rec.Set("controller", r.controller)
	rec.Set("location", r.location)
	if r.changed != "" {
		rec.Set("changed", r.changed)
	}
	rec.Set("payload", r.payload)
	if err := p.app.Save(rec); err != nil {
		p.log.Error("status: save failed", "key", key, "error", err)
		return
	}
	p.m.IncKVApply("put")
}

// removeKey deletes the projection row for a status key, if present.
func (p *Projector) removeKey(key string) {
	rec, err := p.app.FindFirstRecordByData(collection, "key", key)
	if errors.Is(err, sql.ErrNoRows) {
		return
	}
	if err != nil {
		p.log.Error("status: delete lookup failed", "key", key, "error", err)
		return
	}
	if err := p.app.Delete(rec); err != nil {
		p.log.Error("status: delete failed", "key", key, "error", err)
		return
	}
	p.m.IncKVApply("delete")
}

// prune deletes projection rows whose KV key is not in seen — rows orphaned by a
// key removed while accessd was down (no live delete was delivered). Runs at each
// sync sentinel (boot + every reconnect re-sync).
func (p *Projector) prune(seen map[string]struct{}) {
	recs, err := p.app.FindAllRecords(collection)
	if err != nil {
		p.log.Error("status: prune list failed", "error", err)
		return
	}
	for _, rec := range recs {
		if _, ok := seen[rec.GetString("key")]; ok {
			continue
		}
		if err := p.app.Delete(rec); err != nil {
			p.log.Error("status: prune delete failed", "key", rec.GetString("key"), "error", err)
			continue
		}
		p.m.IncKVApply("delete")
	}
}
