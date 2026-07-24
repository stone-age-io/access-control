package controller

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/policy"

	// Hermetic timezones on any OS (Windows has no system zoneinfo).
	_ "time/tzdata"
)

// cacheRecords is a minimal but valid policy graph in wire form — enough to prove
// a cached tap decides (location hq → portal lobby-main under business-hours;
// role staff → alice → CARD-001). Reused to write and reload the offline cache.
var cacheRecords = []struct{ key, val string }{
	{"location.hq", `{"code":"hq","timezone":"America/New_York"}`},
	{"sched.business-hours", `{"code":"business-hours","windows":[{"days":[1,2,3,4,5],"start":"08:00","end":"17:00"}]}`},
	{"portal.lobby-main", `{"code":"lobby-main","type":"door","location":"hq","posture":"secure","pulseSeconds":5,"controller":"ctrl-hq-1","lockRelay":1}`},
	{"group.lobby-group", `{"code":"lobby-group","portals":["lobby-main"],"schedule":"business-hours"}`},
	{"role.staff", `{"code":"staff","groups":["lobby-group"]}`},
	{"user.alice", `{"id":"alice","status":"active","roles":["staff"]}`},
	{"cred.CARD-001", `{"value":"CARD-001","user":"alice","status":"active"}`},
}

// writeCacheFile builds a snapshot on disk via a PolicyCache (put + mark-synced +
// drain), stamped at syncedAt, and returns its path.
func writeCacheFile(t *testing.T, syncedAt time.Time) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "policy-cache.json")
	c := NewPolicyCache(path, logger.NewNopLogger())
	for _, r := range cacheRecords {
		c.put(r.key, []byte(r.val))
	}
	c.markSynced(syncedAt)
	c.drain()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected cache file written: %v", err)
	}
	return path
}

func TestPolicyCacheRoundtrip(t *testing.T) {
	at := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)
	c := NewPolicyCache(writeCacheFile(t, at), logger.NewNopLogger())
	snap, ok, err := c.load()
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	if !snap.SyncedAt.Equal(at) {
		t.Errorf("syncedAt = %v, want %v", snap.SyncedAt, at)
	}
	if len(snap.Entries) != len(cacheRecords) {
		t.Errorf("entries = %d, want %d", len(snap.Entries), len(cacheRecords))
	}
}

// TestPolicyCacheDrainSkipsBeforeSync verifies the partial view during boot
// re-delivery never clobbers a good on-disk snapshot: nothing is written until the
// first live sync (markSynced) fires.
func TestPolicyCacheDrainSkipsBeforeSync(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy-cache.json")
	c := NewPolicyCache(path, logger.NewNopLogger())
	c.put("cred.CARD-001", []byte(`{"value":"CARD-001","status":"active"}`))
	c.drain() // no markSynced yet
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected no cache file before first sync, stat err = %v", err)
	}
}

// TestPolicyCacheReflectsDelete verifies write-through: a deleted key is dropped
// from the next persisted snapshot.
func TestPolicyCacheReflectsDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy-cache.json")
	c := NewPolicyCache(path, logger.NewNopLogger())
	c.put("cred.A", []byte(`{"value":"A","status":"active"}`))
	c.put("cred.B", []byte(`{"value":"B","status":"active"}`))
	c.markSynced(time.Now())
	c.drain()
	c.delete("cred.A")
	c.drain()

	snap, ok, err := c.load()
	if err != nil || !ok {
		t.Fatalf("load: ok=%v err=%v", ok, err)
	}
	if _, has := snap.Entries["cred.A"]; has {
		t.Error("deleted key should not persist")
	}
	if _, has := snap.Entries["cred.B"]; !has {
		t.Error("kept key should persist")
	}
}

// TestLoadCacheFreshPopulates proves a fresh-enough cache seeds the graph and a
// tap decides from it — the whole point of offline operation.
func TestLoadCacheFreshPopulates(t *testing.T) {
	path := writeCacheFile(t, time.Now().Add(-time.Hour))
	s := NewPolicyStore(nil, logger.NewNopLogger(), nil)
	s.SetCache(NewPolicyCache(path, logger.NewNopLogger()))

	loaded, _, age := s.LoadCache(72 * time.Hour)
	if !loaded {
		t.Fatal("expected fresh cache to load")
	}
	if age < 30*time.Minute || age > 2*time.Hour {
		t.Errorf("age = %v, want ~1h", age)
	}
	if _, ok := s.Portal("lobby-main"); !ok {
		t.Error("lobby-main not loaded from cache")
	}
	if state, _ := s.SyncStatus(); state != "cached" {
		t.Errorf("state = %q, want cached", state)
	}
	// A tap decides entirely from cached policy — Mon 2026-01-05 09:00 NY, in hours.
	d := s.Decide(policy.PostureSecure, "CARD-001", "lobby-main", ny(t, 2026, 1, 5, 9, 0))
	if !d.Allow || d.User != "alice" {
		t.Errorf("decision from cached policy = %+v, want allow alice", d)
	}
}

// TestLoadCacheStaleRefused proves the staleness bound is fail-secure: a cache
// older than maxAge loads nothing and the box stays default-deny.
func TestLoadCacheStaleRefused(t *testing.T) {
	path := writeCacheFile(t, time.Now().Add(-100*time.Hour))
	s := NewPolicyStore(nil, logger.NewNopLogger(), nil)
	s.SetCache(NewPolicyCache(path, logger.NewNopLogger()))

	if loaded, _, _ := s.LoadCache(72 * time.Hour); loaded {
		t.Fatal("expected stale cache to be refused")
	}
	if _, ok := s.Portal("lobby-main"); ok {
		t.Error("stale cache must not populate the graph")
	}
	if state, _ := s.SyncStatus(); state != "loading" {
		t.Errorf("state = %q, want loading", state)
	}
}

func TestLoadCacheMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")
	s := NewPolicyStore(nil, logger.NewNopLogger(), nil)
	s.SetCache(NewPolicyCache(path, logger.NewNopLogger()))
	if loaded, _, _ := s.LoadCache(72 * time.Hour); loaded {
		t.Fatal("expected no load when the file is absent")
	}
}

func TestLoadCacheDisabledNoCache(t *testing.T) {
	s := NewPolicyStore(nil, logger.NewNopLogger(), nil)
	if loaded, _, _ := s.LoadCache(72 * time.Hour); loaded {
		t.Fatal("expected no load when no cache is attached")
	}
}

// TestWatchOfflineBootDoesNotBlockOrCrash proves the boot-resilience contract: a
// controller whose KV bind keeps failing (NATS unreachable) must not make Watch
// fatal, must keep retrying, must stay not-ready (default-deny), and must still
// shut down promptly.
func TestWatchOfflineBootDoesNotBlockOrCrash(t *testing.T) {
	old := kvWatchRetryBaseDelay
	kvWatchRetryBaseDelay = 5 * time.Millisecond
	defer func() { kvWatchRetryBaseDelay = old }()

	var attempts int32
	binder := func(context.Context) (jetstream.KeyValue, error) {
		atomic.AddInt32(&attempts, 1)
		return nil, errors.New("no NATS")
	}
	s := NewPolicyStore(binder, logger.NewNopLogger(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Watch(ctx); err != nil {
		t.Fatalf("Watch returned an error on offline boot: %v", err)
	}

	time.Sleep(60 * time.Millisecond) // let it retry a few times
	if s.Ready() {
		t.Error("store must not be Ready while the KV bind keeps failing")
	}
	if n := atomic.LoadInt32(&attempts); n < 2 {
		t.Errorf("expected repeated bind attempts, got %d", n)
	}

	done := make(chan struct{})
	go func() { s.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return promptly after an offline boot")
	}
}
