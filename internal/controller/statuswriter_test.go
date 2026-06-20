package controller

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/statuskv"
)

// fakeStatusKV records puts/deletes against the status bucket. It implements only
// the two methods StatusWriter uses; the embedded interface panics on anything
// else (which the writer never calls).
type fakeStatusKV struct {
	jetstream.KeyValue
	mu      sync.Mutex
	store   map[string][]byte
	puts    int
	deletes int
	failOn  map[string]bool
}

func newFakeStatusKV() *fakeStatusKV {
	return &fakeStatusKV{store: map[string][]byte{}, failOn: map[string]bool{}}
}

func (f *fakeStatusKV) Put(_ context.Context, key string, value []byte) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failOn[key] {
		return 0, errors.New("boom")
	}
	f.store[key] = append([]byte(nil), value...)
	f.puts++
	return 1, nil
}

func (f *fakeStatusKV) Delete(_ context.Context, key string, _ ...jetstream.KVDeleteOpt) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failOn[key] {
		return errors.New("boom")
	}
	delete(f.store, key)
	f.deletes++
	return nil
}

func (f *fakeStatusKV) snapshot() (store map[string][]byte, puts, deletes int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string][]byte, len(f.store))
	for k, v := range f.store {
		out[k] = v
	}
	return out, f.puts, f.deletes
}

func newWriter(t *testing.T) (*StatusWriter, *fakeStatusKV) {
	t.Helper()
	kv := newFakeStatusKV()
	return NewStatusWriter(kv, "ctrl-hq-1", logger.NewNopLogger()), kv
}

// Drains are exercised directly (no goroutine) for determinism.
func TestStatusWriterCoalescesUnchanged(t *testing.T) {
	w, kv := newWriter(t)
	at := time.Unix(1_700_000_000, 0).UTC()

	w.SetPortal("lobby-main", "hq", statuskv.DoorClosed, "secure", false, at)
	w.drain(context.Background())
	if _, puts, _ := kv.snapshot(); puts != 1 {
		t.Fatalf("puts after first set = %d, want 1", puts)
	}

	// Same value again → reconcile finds no divergence → no new put.
	w.SetPortal("lobby-main", "hq", statuskv.DoorClosed, "secure", false, at)
	w.drain(context.Background())
	if _, puts, _ := kv.snapshot(); puts != 1 {
		t.Errorf("puts after redundant set = %d, want 1 (coalesced)", puts)
	}

	// A real change → one more put.
	w.SetPortal("lobby-main", "hq", statuskv.DoorOpen, "secure", false, at)
	w.drain(context.Background())
	if store, puts, _ := kv.snapshot(); puts != 2 {
		t.Errorf("puts after change = %d, want 2 (store=%v)", puts, store)
	}
}

func TestStatusWriterDelete(t *testing.T) {
	w, kv := newWriter(t)
	at := time.Unix(1_700_000_000, 0).UTC()
	w.SetPortal("lobby-main", "hq", statuskv.DoorClosed, "secure", false, at)
	w.drain(context.Background())

	w.DeletePortal("lobby-main")
	w.drain(context.Background())
	store, _, deletes := kv.snapshot()
	if deletes != 1 {
		t.Errorf("deletes = %d, want 1", deletes)
	}
	if _, ok := store[statuskv.PrefixPortal+"lobby-main"]; ok {
		t.Errorf("key still present after delete: %v", store)
	}
}

// Resync re-publishes the whole shadow even when nothing changed (the self-heal
// after a reconnect drops in-flight writes).
func TestStatusWriterResyncRepublishes(t *testing.T) {
	w, kv := newWriter(t)
	at := time.Unix(1_700_000_000, 0).UTC()
	w.SetPortal("lobby-main", "hq", statuskv.DoorClosed, "secure", false, at)
	w.drain(context.Background())
	if _, puts, _ := kv.snapshot(); puts != 1 {
		t.Fatalf("setup puts = %d, want 1", puts)
	}

	w.Resync()
	w.drain(context.Background())
	if _, puts, _ := kv.snapshot(); puts != 2 {
		t.Errorf("puts after resync = %d, want 2 (republished)", puts)
	}
}

// A failed put leaves the key divergent so the next drain retries it.
func TestStatusWriterRetriesAfterFailure(t *testing.T) {
	w, kv := newWriter(t)
	at := time.Unix(1_700_000_000, 0).UTC()
	key := statuskv.PrefixPortal + "lobby-main"
	kv.failOn[key] = true

	w.SetPortal("lobby-main", "hq", statuskv.DoorClosed, "secure", false, at)
	w.drain(context.Background())
	if store, _, _ := kv.snapshot(); len(store) != 0 {
		t.Fatalf("store should be empty after failed put, got %v", store)
	}

	kv.failOn[key] = false
	w.drain(context.Background()) // retry without any new Set
	if store, _, _ := kv.snapshot(); len(store) != 1 {
		t.Errorf("store should have the key after retry, got %v", store)
	}
}
