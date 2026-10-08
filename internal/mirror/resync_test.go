package mirror

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stone-age-io/access-control/internal/logger"
)

// memKV is an in-memory ACC_POLICY stand-in. down makes every write fail, the
// way a put does while accessd's NATS link is gone; puts counts the writes that
// landed, so a test can tell a repair from a redundant re-put.
type memKV struct {
	jetstream.KeyValue // unimplemented methods panic; the mirror uses only these
	mu                 sync.Mutex
	data               map[string][]byte
	down               bool
	puts               int
}

type memEntry struct {
	jetstream.KeyValueEntry
	val []byte
}

func (e memEntry) Value() []byte { return e.val }

func (k *memKV) Get(_ context.Context, key string) (jetstream.KeyValueEntry, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.data[key]
	if !ok {
		return nil, jetstream.ErrKeyNotFound
	}
	return memEntry{val: v}, nil
}

func (k *memKV) Put(_ context.Context, key string, val []byte) (uint64, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.down {
		return 0, errors.New("nats: connection closed")
	}
	k.data[key] = append([]byte(nil), val...)
	k.puts++
	return uint64(k.puts), nil
}

func (k *memKV) Delete(_ context.Context, key string, _ ...jetstream.KVDeleteOpt) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.down {
		return errors.New("nats: connection closed")
	}
	delete(k.data, key)
	return nil
}

func (k *memKV) Keys(_ context.Context, _ ...jetstream.WatchOpt) ([]string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if len(k.data) == 0 {
		return nil, jetstream.ErrNoKeysFound
	}
	keys := make([]string, 0, len(k.data))
	for key := range k.data {
		keys = append(keys, key)
	}
	return keys, nil
}

func (k *memKV) setDown(down bool) { k.mu.Lock(); k.down = down; k.mu.Unlock() }

func (k *memKV) state() (string, int) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return string(k.data["cred.CARD-001"]), k.puts
}

// A credential revoked while NATS is down fails its KV write after the commit.
// Resync, which accessd runs on reconnect, must carry the revocation to KV, and
// must not re-put keys that already hold their payload.
func TestResyncRepairsWritesMissedDuringOutage(t *testing.T) {
	app := newApp(t)
	kv := &memKV{data: map[string][]byte{}}
	p := Register(app, kv, logger.NewNopLogger(), nil)
	if err := p.SyncAll(context.Background(), app); err != nil {
		t.Fatalf("SyncAll: %v", err)
	}
	before, putsAfterBoot := kv.state()

	kv.setDown(true)
	cred := find(t, app, "credentials", "value", "CARD-001")
	cred.Set("status", "revoked")
	if err := app.Save(cred); err != nil {
		t.Fatalf("save: %v", err)
	}
	if after, _ := kv.state(); after != before {
		t.Fatalf("KV changed while down: %s", after)
	}

	kv.setDown(false)
	p.Resync()
	p.Resync() // coalesces with the running one
	p.syncWG.Wait()

	got, puts := kv.state()
	if got == before {
		t.Fatalf("revocation never reached KV: %s", got)
	}
	if puts-putsAfterBoot != 1 {
		t.Errorf("resync wrote %d keys, want only the 1 that was missed", puts-putsAfterBoot)
	}
}
