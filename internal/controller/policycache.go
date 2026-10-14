package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/policykv"
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
// never under the store lock. It is the single writer of its snapshot files.
//
// It writes two files from the same drain. The full snapshot (path) is the opt-in
// offline cache; an empty path disables it. The bindings snapshot (bindingsPath)
// is always on: only this controller's portal records, reduced to the fields that
// arm their hardware (see portalBinding). It lets a box arm its doors at boot
// before policy arrives, so a maglock is locked instead of unpowered, while
// carrying no posture and no credential: every tap still denies until policy
// syncs.
//
// The persisted set is only ever written from a live full sync (WatchAll
// re-delivers every key) — never from the partial view during boot re-delivery,
// and never while offline — so a delete that was compacted out of KV history while
// the box was offline is naturally dropped from the next persisted snapshot rather
// than lingering forever.
type PolicyCache struct {
	path         string // full snapshot; "" = the offline cache is disabled
	bindingsPath string // bindings snapshot; "" = not written
	controller   string // this controller's code: the bindings snapshot keeps only its portals
	log          *logger.Logger
	connected    func() bool // reports live NATS connectivity; nil = assume disconnected

	// lastBindings is the bindings entries last written, so the freshness touch and
	// unrelated policy churn (a credential edit) do not rewrite an unchanged file.
	// Confined to the drain goroutine.
	lastBindings []byte

	mu       sync.Mutex
	entries  map[string][]byte
	syncedAt time.Time
	synced   bool // a live full sync has completed this session (gate for writing)
	dirty    chan struct{}

	cancel context.CancelFunc
	done   chan struct{}
}

// NewPolicyCache creates a cache backed by the given snapshot path ("" = no full
// snapshot; SetBindings turns on the bindings one).
func NewPolicyCache(path string, log *logger.Logger) *PolicyCache {
	return &PolicyCache{
		path:    path,
		log:     log.With("component", "policycache"),
		entries: make(map[string][]byte),
		dirty:   make(chan struct{}, 1),
	}
}

// SetBindings turns on the bindings snapshot for the given controller code. Call
// before Start.
func (c *PolicyCache) SetBindings(path, controller string) {
	c.bindingsPath = path
	c.controller = controller
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
	c.log.Info("policy snapshots started", "cache", c.path, "bindings", c.bindingsPath)
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
// disconnected — an offline box must never refresh its own freshness. No-op
// without the full snapshot too: the bindings snapshot has no staleness bound.
func (c *PolicyCache) touch() {
	if c.path == "" || c.connected == nil || !c.connected() {
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

// load reads and decodes the full snapshot from disk.
func (c *PolicyCache) load() (snap cacheSnapshot, ok bool, err error) {
	return readSnapshot(c.path)
}

// readSnapshot reads and decodes a snapshot file. ok is false (with a nil error)
// when the file is simply absent — a fresh box has no snapshot yet.
func readSnapshot(path string) (snap cacheSnapshot, ok bool, err error) {
	b, err := os.ReadFile(path)
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

// drain writes the current snapshots to disk atomically. It is a no-op until the
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

	// On a write failure leave the on-disk file as-is; the next change or touch
	// retries. Failing to persist is not fatal — the live graph is unaffected.
	if c.path != "" {
		if err := writeSnapshot(c.path, snap); err != nil {
			c.log.Error("failed to write policy cache; will retry on next change", "error", err)
		}
	}
	if c.bindingsPath != "" {
		c.writeBindings(snap)
	}
}

// writeBindings writes the bindings snapshot: this controller's portal entries
// from the full snapshot, each reduced to a portalBinding. It skips the write
// when those entries are unchanged, so syncedAt is the live sync that last
// changed them (or the first one after boot).
func (c *PolicyCache) writeBindings(full cacheSnapshot) {
	snap := cacheSnapshot{SyncedAt: full.SyncedAt, Entries: make(map[string]json.RawMessage)}
	for key, val := range full.Entries {
		if !strings.HasPrefix(key, policykv.PrefixPortal) {
			continue
		}
		b, ok := reducePortal(val)
		if !ok || b.Controller != c.controller {
			continue
		}
		raw, err := json.Marshal(b)
		if err != nil {
			continue
		}
		snap.Entries[key] = raw
	}
	entries, err := json.Marshal(snap.Entries) // map keys marshal sorted: stable bytes
	if err != nil || bytes.Equal(entries, c.lastBindings) {
		return
	}
	if err := writeSnapshot(c.bindingsPath, snap); err != nil {
		c.log.Error("failed to write bindings snapshot; will retry on next change", "error", err)
		return
	}
	c.lastBindings = entries
}

// writeSnapshot serializes a snapshot and replaces the file atomically (temp +
// rename), creating the parent directory if needed. Perms are owner-only: the
// full snapshot holds credential values, and only the controller reads either.
func writeSnapshot(path string, snap cacheSnapshot) error {
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// portalBinding is a portal's wire record (policykv.Portal) cut down to what arms
// its hardware: the reader subject (type, location), the lock relay and its type,
// the door inputs and their contact sense, and the OSDP reader address. Its JSON
// tags are the wire's, so decoding a full record into it drops every other field,
// and PolicyStore.apply parses the result like any portal record.
//
// What it leaves out is the point. No posture, auto posture, or schedule: the
// portal resolves to posture "", which denies every tap and commanded grant and
// holds no strike. No held-open threshold, REX unlock, or area: no DOTL alarm, no
// REX pulse, no intrusion roll-up. Everything the portal manager compares before
// re-arming (type, reader address, hwSig) is kept, so the live record that
// replaces this one at sync does not re-arm the portal.
type portalBinding struct {
	Code          string `json:"code"`
	Type          string `json:"type"`
	Location      string `json:"location"`
	Controller    string `json:"controller,omitempty"`
	LockRelay     int    `json:"lockRelay,omitempty"`
	LockType      string `json:"lockType,omitempty"`
	DpsInput      int    `json:"dpsInput,omitempty"`
	RexInput      int    `json:"rexInput,omitempty"`
	DpsContact    string `json:"dpsContact,omitempty"`
	RexContact    string `json:"rexContact,omitempty"`
	ReaderAddress int    `json:"readerAddress,omitempty"`
}

// reducePortal decodes a portal wire record into its portalBinding.
func reducePortal(val []byte) (portalBinding, bool) {
	var b portalBinding
	if err := json.Unmarshal(val, &b); err != nil {
		return portalBinding{}, false
	}
	return b, true
}
