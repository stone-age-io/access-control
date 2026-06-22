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
	addr  map[string]int    // code -> reader address last armed with
}

func newFakeArmer() *fakeArmer {
	return &fakeArmer{armed: make(map[string]string), addr: make(map[string]int)}
}

func (f *fakeArmer) Arm(p Portal) error {
	f.mu.Lock()
	f.armed[p.Code] = p.Type
	f.addr[p.Code] = p.Address
	f.mu.Unlock()
	return nil
}

func (f *fakeArmer) Disarm(code string) {
	f.mu.Lock()
	delete(f.armed, code)
	delete(f.addr, code)
	f.mu.Unlock()
}

func (f *fakeArmer) typeOf(code string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.armed[code]
	return t, ok
}

func (f *fakeArmer) addrOf(code string) (int, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.addr[code]
	return a, ok
}

// managerFor builds a reconciler for the given controller code over the seeded
// store, a fake armer, and a runtime with no locks armed. The seeded fixture
// binds portal lobby-main to controller ctrl-hq-1.
func managerFor(t *testing.T, code string) (*PortalManager, *fakeArmer, *Runtime, *PolicyStore) {
	t.Helper()
	store := seeded(t)
	armer := newFakeArmer()
	rt := NewRuntime("hq", store, drivers.NewMockReader(1), nil, nil, &fakeEmitter{},
		subjects.Default(), logger.NewNopLogger(), nil)
	mgr := NewPortalManager(code, "hq", store, armer, rt, drivers.NewMockHardware(nil), logger.NewNopLogger())
	return mgr, armer, rt, store
}

// recordingHW records the logical indices each portal is armed with, so a test
// can prove the central binding (policy) reaches the hardware backend.
type recordingHW struct {
	mu    sync.Mutex
	armed map[string][3]int // code -> {lockRelay, dpsInput, rexInput}
}

func newRecordingHW() *recordingHW { return &recordingHW{armed: make(map[string][3]int)} }

func (h *recordingHW) Arm(code string, lockRelay, dpsInput, rexInput int) (drivers.LockDriver, error) {
	h.mu.Lock()
	h.armed[code] = [3]int{lockRelay, dpsInput, rexInput}
	h.mu.Unlock()
	return drivers.NewMockLock(code, nil), nil
}

func (h *recordingHW) Disarm(code string) {
	h.mu.Lock()
	delete(h.armed, code)
	h.mu.Unlock()
}

func (h *recordingHW) indices(code string) ([3]int, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	v, ok := h.armed[code]
	return v, ok
}

// The portal's logical relay/input indices from policy flow through to the
// hardware backend's Arm — the path that, on real hardware, picks the GPIO lines.
func TestReconcilePassesBindingToHardware(t *testing.T) {
	store := seeded(t)
	rt := NewRuntime("hq", store, drivers.NewMockReader(1), nil, nil, &fakeEmitter{},
		subjects.Default(), logger.NewNopLogger(), nil)
	hw := newRecordingHW()
	mgr := NewPortalManager("ctrl-hq-1", "hq", store, newFakeArmer(), rt, hw, logger.NewNopLogger())
	mgr.reconcile()

	got, ok := hw.indices("lobby-main")
	if !ok {
		t.Fatal("lobby-main hardware not armed")
	}
	// Seeded fixture: lock_relay=1, dps_input=1, rex_input=2.
	if got != [3]int{1, 1, 2} {
		t.Errorf("armed indices = %v, want [1 1 2] (lock_relay, dps_input, rex_input)", got)
	}
}

func TestReconcileArmsBoundPortal(t *testing.T) {
	mgr, armer, rt, _ := managerFor(t, "ctrl-hq-1")
	mgr.reconcile()

	if got, ok := armer.typeOf("lobby-main"); !ok || got != "door" {
		t.Errorf("armed type = %q (ok=%v), want door", got, ok)
	}
	if !rt.drives("lobby-main") {
		t.Error("runtime does not drive lobby-main after reconcile")
	}
}

// A controller with no portals bound to it arms nothing.
func TestReconcileArmsNothingForUnboundController(t *testing.T) {
	mgr, armer, rt, _ := managerFor(t, "ctrl-empty")
	mgr.reconcile()

	if _, ok := armer.typeOf("lobby-main"); ok {
		t.Error("armed a portal not bound to this controller")
	}
	if rt.drives("lobby-main") {
		t.Error("runtime drives a portal not bound to this controller")
	}
}

// A controller with no code configured arms nothing — it must not match the
// unassigned portals (whose controller relation is also empty).
func TestReconcileArmsNothingForEmptyCode(t *testing.T) {
	mgr, armer, _, store := managerFor(t, "")
	// Add an unassigned portal (no controller) to prove it is not armed.
	store.apply("portal.orphan", []byte(`{"code":"orphan","type":"door","location":"hq"}`))
	mgr.reconcile()

	if _, ok := armer.typeOf("orphan"); ok {
		t.Error("armed an unassigned portal under an empty controller code")
	}
	if _, ok := armer.typeOf("lobby-main"); ok {
		t.Error("armed a bound portal under an empty controller code")
	}
}

// A portal assigned to this controller after boot is armed on the next reconcile,
// without a restart.
func TestReconcileArmsPortalAssignedLater(t *testing.T) {
	mgr, armer, rt, store := managerFor(t, "ctrl-hq-2")
	mgr.reconcile()
	if _, ok := armer.typeOf("dock-1"); ok {
		t.Fatal("armed dock-1 before it was assigned")
	}

	store.apply("portal.dock-1", []byte(`{"code":"dock-1","type":"gate","location":"hq","posture":"secure","pulseSeconds":4,"controller":"ctrl-hq-2"}`))
	mgr.reconcile()

	if got, ok := armer.typeOf("dock-1"); !ok || got != "gate" {
		t.Errorf("armed type = %q (ok=%v), want gate", got, ok)
	}
	if !rt.drives("dock-1") {
		t.Error("runtime does not drive dock-1 after it was assigned")
	}
}

// Re-typing a portal in policy re-arms it on the new type.
func TestReconcileReArmsOnTypeChange(t *testing.T) {
	mgr, armer, _, store := managerFor(t, "ctrl-hq-1")
	mgr.reconcile()
	if got, _ := armer.typeOf("lobby-main"); got != "door" {
		t.Fatalf("initial type = %q, want door", got)
	}

	store.apply("portal.lobby-main", []byte(`{"code":"lobby-main","type":"turnstile","location":"hq","posture":"secure","pulseSeconds":5,"controller":"ctrl-hq-1"}`))
	mgr.reconcile()

	if got, ok := armer.typeOf("lobby-main"); !ok || got != "turnstile" {
		t.Errorf("type after change = %q (ok=%v), want turnstile", got, ok)
	}
}

// Toggling a portal's OSDP reader (its reader_address) re-arms it live, so the
// reader binding changes without a controller restart. -1 = NATS-only; >= 0 =
// an OSDP reader at that PD address.
func TestReconcileReArmsOnReaderAddressChange(t *testing.T) {
	mgr, armer, _, store := managerFor(t, "ctrl-hq-1")
	// Start from a known NATS-only baseline.
	store.apply("portal.lobby-main", []byte(`{"code":"lobby-main","type":"door","location":"hq","posture":"secure","pulseSeconds":5,"controller":"ctrl-hq-1","readerAddress":-1}`))
	mgr.reconcile()
	if a, ok := armer.addrOf("lobby-main"); !ok || a != -1 {
		t.Fatalf("baseline address = %d (ok=%v), want -1", a, ok)
	}

	// Operator enables OSDP at PD 5.
	store.apply("portal.lobby-main", []byte(`{"code":"lobby-main","type":"door","location":"hq","posture":"secure","pulseSeconds":5,"controller":"ctrl-hq-1","readerAddress":5}`))
	mgr.reconcile()
	if a, ok := armer.addrOf("lobby-main"); !ok || a != 5 {
		t.Errorf("address after enabling OSDP = %d (ok=%v), want 5", a, ok)
	}

	// Operator disables it again.
	store.apply("portal.lobby-main", []byte(`{"code":"lobby-main","type":"door","location":"hq","posture":"secure","pulseSeconds":5,"controller":"ctrl-hq-1","readerAddress":-1}`))
	mgr.reconcile()
	if a, ok := armer.addrOf("lobby-main"); !ok || a != -1 {
		t.Errorf("address after disabling OSDP = %d (ok=%v), want -1", a, ok)
	}
}

// Removing a portal from policy disarms its reader and lock.
func TestReconcileDisarmsOnRemoval(t *testing.T) {
	mgr, armer, rt, store := managerFor(t, "ctrl-hq-1")
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

// Reassigning a portal to another controller disarms it here.
func TestReconcileDisarmsOnReassignment(t *testing.T) {
	mgr, armer, rt, store := managerFor(t, "ctrl-hq-1")
	mgr.reconcile()
	if !rt.drives("lobby-main") {
		t.Fatal("not driving lobby-main after initial reconcile")
	}

	store.apply("portal.lobby-main", []byte(`{"code":"lobby-main","type":"door","location":"hq","posture":"secure","pulseSeconds":5,"controller":"ctrl-hq-2"}`))
	mgr.reconcile()

	if _, ok := armer.typeOf("lobby-main"); ok {
		t.Error("reader still armed after portal reassigned to another controller")
	}
	if rt.drives("lobby-main") {
		t.Error("runtime still drives lobby-main after reassignment")
	}
}

// Notify coalesces: a burst of signals collapses to a single pending reconcile.
func TestNotifyCoalesces(t *testing.T) {
	mgr, _, _, _ := managerFor(t, "ctrl-hq-1")
	for i := 0; i < 5; i++ {
		mgr.Notify()
	}
	if n := len(mgr.dirty); n != 1 {
		t.Errorf("pending reconciles = %d, want 1 (coalesced)", n)
	}
}
