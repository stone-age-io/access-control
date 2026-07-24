package controller

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/stone-age-io/access-control/internal/logger"
)

// cacheTouchInterval is how often the cache re-stamps its freshness timestamp
// while NATS is connected, so an offline reboot measures staleness from the last
// moment the box actually had live contact — not merely the last policy change or
// reconnect. A box connected for days with no edits would otherwise look stale and
// wrongly refuse its own (perfectly current) cache on the next offline reboot. A
// var so tests can shorten it. Coarse on purpose: precision to a few minutes is
// ample against a multi-hour MaxAge, and it bounds flash-write wear on SD-backed
// boxes.
var cacheTouchInterval = 10 * time.Minute

// cacheSnapshot is the on-disk shape of the offline config cache: every policy KV
// entry (key -> raw wire value) as delivered over the wire, plus the time of the
// last live sync. Values are stored as raw JSON (the KV wire is JSON), so the file
// is human-readable and needs no decode step of its own. Replaying Entries through
// PolicyStore.apply reproduces the exact graph a live sync would build, so the
// cache carries no parsing logic and cannot drift from the wire contract.
type cacheSnapshot struct {
	SyncedAt time.Time                  `json:"syncedAt"`
	Entries  map[string]json.RawMessage `json:"entries"`
}

// PolicyCache is the controller's local, write-through snapshot of the policy KV
// keyspace, enabling last-known-config operation across a reboot with NATS
// unreachable (leaf node down, or no network). It mirrors StatusWriter's design:
// the watch goroutine only mutates an in-memory map and signals; a drain goroutine
// writes the snapshot to disk atomically (temp + rename), off the hot path and
// never under the store lock. It is the single writer of its snapshot file.
//
// The persisted set is only ever written from a live full sync (WatchAll
// re-delivers every key) — never from the partial view during boot re-delivery,
// and never while offline — so a delete that was compacted out of KV history while
// the box was offline is naturally dropped from the next persisted snapshot rather
// than lingering forever.
type PolicyCache struct {
	path      string
	log       *logger.Logger
	connected func() bool // reports live NATS connectivity; nil = assume disconnected

	mu       sync.Mutex
	entries  map[string][]byte
	syncedAt time.Time
	synced   bool // a live full sync has completed this session (gate for writing)
	dirty    chan struct{}

	cancel context.CancelFunc
	done   chan struct{}
}

// NewPolicyCache creates a cache backed by the given snapshot path.
func NewPolicyCache(path string, log *logger.Logger) *PolicyCache {
	return &PolicyCache{
		path:    path,
		log:     log.With("component", "policycache"),
		entries: make(map[string][]byte),
		dirty:   make(chan struct{}, 1),
	}
}

// SetConnected wires the live-connectivity probe used by the freshness touch.
// Call before Start.
func (c *PolicyCache) SetConnected(fn func() bool) { c.connected = fn }

// Start launches the drain + periodic-touch goroutine. Stop ends it.
func (c *PolicyCache) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	c.cancel = cancel
	c.done = make(chan struct{})
	go func() {
		defer close(c.done)
		ticker := time.NewTicker(cacheTouchInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-c.dirty:
				c.drain()
			case <-ticker.C:
				c.touch()
			}
		}
	}()
	c.log.Info("policy cache started", "path", c.path)
}

// Stop halts the goroutine and waits for it to exit.
func (c *PolicyCache) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
	if c.done != nil {
		<-c.done
	}
}

// put records a KV entry (copying val, since the caller owns the buffer) and
// signals the drain.
func (c *PolicyCache) put(key string, val []byte) {
	cp := make([]byte, len(val))
	copy(cp, val)
	c.mu.Lock()
	c.entries[key] = cp
	c.mu.Unlock()
	c.signal()
}

// delete drops a KV entry and signals the drain.
func (c *PolicyCache) delete(key string) {
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
	c.signal()
}

// markSynced records that a live full sync just completed: it stamps the freshness
// time and, from now on, permits the snapshot to be written. Called on the KV sync
// sentinel (boot connect and every reconnect).
func (c *PolicyCache) markSynced(at time.Time) {
	c.mu.Lock()
	c.syncedAt = at.UTC()
	c.synced = true
	c.mu.Unlock()
	c.signal()
}

// touch re-stamps the freshness time while connected, so staleness is measured
// from the last live contact. No-op until the first live sync, or when
// disconnected — an offline box must never refresh its own freshness.
func (c *PolicyCache) touch() {
	if c.connected == nil || !c.connected() {
		return
	}
	c.mu.Lock()
	if !c.synced {
		c.mu.Unlock()
		return
	}
	c.syncedAt = time.Now().UTC()
	c.mu.Unlock()
	c.signal()
}

func (c *PolicyCache) signal() {
	select {
	case c.dirty <- struct{}{}:
	default:
	}
}

// load reads and decodes the snapshot from disk. ok is false (with a nil error)
// when the file is simply absent — a fresh box has no cache yet.
func (c *PolicyCache) load() (snap cacheSnapshot, ok bool, err error) {
	b, err := os.ReadFile(c.path)
	if errors.Is(err, os.ErrNotExist) {
		return cacheSnapshot{}, false, nil
	}
	if err != nil {
		return cacheSnapshot{}, false, err
	}
	if err := json.Unmarshal(b, &snap); err != nil {
		return cacheSnapshot{}, false, err
	}
	return snap, true, nil
}

// drain writes the current snapshot to disk atomically. It is a no-op until the
// first live sync, so the partial view during boot re-delivery never clobbers a
// good on-disk snapshot.
func (c *PolicyCache) drain() {
	c.mu.Lock()
	if !c.synced {
		c.mu.Unlock()
		return
	}
	snap := cacheSnapshot{SyncedAt: c.syncedAt, Entries: make(map[string]json.RawMessage, len(c.entries))}
	for k, v := range c.entries {
		snap.Entries[k] = append(json.RawMessage(nil), v...)
	}
	c.mu.Unlock()

	if err := c.write(snap); err != nil {
		// Leave the on-disk file as-is; the next change or touch retries. Failing to
		// persist is not fatal — the live graph is unaffected.
		c.log.Error("failed to write policy cache; will retry on next change", "error", err)
	}
}

// write serializes the snapshot and replaces the file atomically (temp + rename),
// creating the parent directory if needed. Perms are owner-only: the snapshot
// holds credential values.
func (c *PolicyCache) write(snap cacheSnapshot) error {
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}
