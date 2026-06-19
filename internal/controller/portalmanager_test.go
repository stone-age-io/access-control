package controller

import (
	"sync"
	"testing"

	"github.com/stone-age-io/access-control/internal/drivers"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/subjects"
)

// fakeArmer records arm/disarm calls in place of real NATS subscriptions.
type fakeArmer struct {
	mu    sync.Mutex
	armed map[string]string // code -> type
}

func newFakeArmer() *fakeArmer { return &fakeArmer{armed: make(map[string]string)} }

func (f *fakeArmer) Arm(p Portal) error {
	f.mu.Lock()
	f.armed[p.Code] = p.Type
	f.mu.Unlock()
	return nil
}

func (f *fakeArmer) Disarm(code string) {
	f.mu.Lock()
	delete(f.armed, code)
	f.mu.Unlock()
}

func (f *fakeArmer) typeOf(code string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.armed[code]
	return t, ok
}

// managerFor builds a reconciler over the given configured codes, the seeded
// store, a fake armer, and a runtime with no locks armed.
func managerFor(t *testing.T, codes ...string) (*PortalManager, *fakeArmer, *Runtime, *PolicyStore) {
	t.Helper()
	store := seeded(t)
	armer := newFakeArmer()
	rt := NewRuntime("hq", store, drivers.NewMockReader(1), nil, &fakeEmitter{},
		subjects.Default(), logger.NewNopLogger(), nil)
	mkLock := func(code string) drivers.LockDriver { return drivers.NewMockLock(code, nil) }
	mgr := NewPortalManager(codes, "hq", store, armer, rt, mkLock, logger.NewNopLogger())
	return mgr, armer, rt, store
}

func TestReconcileArmsResolvablePortal(t *testing.T) {
	mgr, armer, rt, _ := managerFor(t, "lobby-main")
	mgr.reconcile()

	if got, ok := armer.typeOf("lobby-main"); !ok || got != "door" {
		t.Errorf("armed type = %q (ok=%v), want door", got, ok)
	}
	if !rt.drives("lobby-main") {
		t.Error("runtime does not drive lobby-main after reconcile")
	}
}

func TestReconcileSkipsPortalAbsentFromPolicy(t *testing.T) {
	mgr, armer, rt, _ := managerFor(t, "side-gate") // not in the fixture
	mgr.reconcile()

	if _, ok := armer.typeOf("side-gate"); ok {
		t.Error("armed a portal that is not in policy")
	}
	if rt.drives("side-gate") {
		t.Error("runtime drives a portal absent from policy")
	}
}

// A portal created in accessd after the controller booted is armed on the next
// reconcile, without a restart.
func TestReconcileArmsPortalThatAppearsLater(t *testing.T) {
	mgr, armer, rt, store := managerFor(t, "dock-1")
	mgr.reconcile()
	if _, ok := armer.typeOf("dock-1"); ok {
		t.Fatal("armed dock-1 before it existed")
	}

	store.apply("portal.dock-1", []byte(`{"code":"dock-1","type":"gate","location":"hq","posture":"secure","pulseSeconds":4}`))
	mgr.reconcile()

	if got, ok := armer.typeOf("dock-1"); !ok || got != "gate" {
		t.Errorf("armed type = %q (ok=%v), want gate", got, ok)
	}
	if !rt.drives("dock-1") {
		t.Error("runtime does not drive dock-1 after it appeared")
	}
}

// Re-typing a portal in policy re-arms it on the new type.
func TestReconcileReArmsOnTypeChange(t *testing.T) {
	mgr, armer, _, store := managerFor(t, "lobby-main")
	mgr.reconcile()
	if got, _ := armer.typeOf("lobby-main"); got != "door" {
		t.Fatalf("initial type = %q, want door", got)
	}

	store.apply("portal.lobby-main", []byte(`{"code":"lobby-main","type":"turnstile","location":"hq","posture":"secure","pulseSeconds":5}`))
	mgr.reconcile()

	if got, ok := armer.typeOf("lobby-main"); !ok || got != "turnstile" {
		t.Errorf("type after change = %q (ok=%v), want turnstile", got, ok)
	}
}

// Removing a portal from policy disarms its reader and lock.
func TestReconcileDisarmsOnRemoval(t *testing.T) {
	mgr, armer, rt, store := managerFor(t, "lobby-main")
	mgr.reconcile()
	if !rt.drives("lobby-main") {
		t.Fatal("not driving lobby-main after initial reconcile")
	}

	store.remove("portal.lobby-main")
	mgr.reconcile()

	if _, ok := armer.typeOf("lobby-main"); ok {
		t.Error("reader still armed after portal removed")
	}
	if rt.drives("lobby-main") {
		t.Error("runtime still drives lobby-main after portal removed")
	}
}

// Notify coalesces: a burst of signals collapses to a single pending reconcile.
func TestNotifyCoalesces(t *testing.T) {
	mgr, _, _, _ := managerFor(t, "lobby-main")
	for i := 0; i < 5; i++ {
		mgr.Notify()
	}
	if n := len(mgr.dirty); n != 1 {
		t.Errorf("pending reconciles = %d, want 1 (coalesced)", n)
	}
}
