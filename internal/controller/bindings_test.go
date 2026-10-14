package controller

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stone-age-io/access-control/internal/drivers"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/policy"
	"github.com/stone-age-io/access-control/internal/subjects"
)

// bindingsRecords is a live policy graph for ctrl-hq-1: a maglock portal whose
// standing posture is unlocked (so a posture leaking through the snapshot would
// show as a held lock), with every hardware field set to a non-default value, plus
// a portal driven by another box and the credential chain a tap would need.
var bindingsRecords = []struct{ key, val string }{
	{"location.hq", `{"code":"hq","timezone":"America/New_York"}`},
	{"sched.business-hours", `{"code":"business-hours","windows":[{"days":[1,2,3,4,5],"start":"08:00","end":"17:00"}]}`},
	{"portal.lobby-main", `{"code":"lobby-main","type":"door","location":"hq","posture":"unlocked","pulseSeconds":5,` +
		`"autoPosture":"secure","autoSchedule":"business-hours","controller":"ctrl-hq-1","lockRelay":2,"dpsInput":3,` +
		`"rexInput":4,"heldOpenSeconds":30,"readerAddress":7,"dpsContact":"no","rexContact":"nc","lockType":"maglock",` +
		`"rexUnlock":true,"area":"vault","disarmOnGrant":true}`},
	{"portal.dock-1", `{"code":"dock-1","type":"gate","location":"hq","posture":"secure","controller":"ctrl-hq-2","lockRelay":1}`},
	{"group.lobby-group", `{"code":"lobby-group","portals":["lobby-main"],"schedule":"business-hours"}`},
	{"role.staff", `{"code":"staff","groups":["lobby-group"]}`},
	{"user.alice", `{"id":"alice","status":"active","roles":["staff"]}`},
	{"cred.CARD-001", `{"value":"CARD-001","user":"alice","status":"active"}`},
}

// writeBindingsFile writes the snapshots a live sync of bindingsRecords leaves on
// disk with the offline cache DISABLED, and returns the bindings path.
func writeBindingsFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bindings.json")
	c := NewPolicyCache("", logger.NewNopLogger())
	c.SetBindings(path, "ctrl-hq-1")
	for _, r := range bindingsRecords {
		c.put(r.key, []byte(r.val))
	}
	c.markSynced(time.Now())
	c.drain()
	return path
}

// The snapshot is written with the cache disabled, holds only this controller's
// portal entries, and none of them carries a posture, a schedule, or any other
// operational field.
func TestBindingsSnapshotContents(t *testing.T) {
	path := writeBindingsFile(t)

	files, _ := os.ReadDir(filepath.Dir(path))
	if len(files) != 1 || files[0].Name() != "bindings.json" {
		t.Fatalf("files written = %v, want only bindings.json (cache disabled)", files)
	}
	snap, ok, err := readSnapshot(path)
	if err != nil || !ok {
		t.Fatalf("read: ok=%v err=%v", ok, err)
	}
	if len(snap.Entries) != 1 {
		t.Fatalf("entries = %v, want only portal.lobby-main", snap.Entries)
	}
	raw, ok := snap.Entries["portal.lobby-main"]
	if !ok {
		t.Fatal("portal.lobby-main missing")
	}
	for _, field := range []string{"posture", "autoPosture", "autoSchedule", "pulseSeconds", "heldOpenSeconds",
		"rexUnlock", "area", "disarmOnGrant"} {
		if strings.Contains(string(raw), `"`+field+`"`) {
			t.Errorf("entry carries %q: %s", field, raw)
		}
	}
	for _, field := range []string{"lockType", "dpsContact", "rexContact", "readerAddress", "lockRelay"} {
		if !strings.Contains(string(raw), `"`+field+`"`) {
			t.Errorf("entry lost hardware field %q: %s", field, raw)
		}
	}
}

// countingHW is the mock backend, counting Arm calls per portal so a test can
// prove a portal was armed once and not re-armed.
type countingHW struct {
	*drivers.MockHardware
	arms map[string]int
}

func (h *countingHW) Arm(code string, io drivers.PortalIO) (drivers.LockDriver, error) {
	h.arms[code]++
	return h.MockHardware.Arm(code, io)
}

// bindingsBoot boots a controller the way main does with no usable cache: load the
// bindings snapshot, then reconcile once.
func bindingsBoot(t *testing.T) (*PolicyStore, *Runtime, *PortalManager, *countingHW, *drivers.MockReader, *fakeEmitter) {
	t.Helper()
	s := NewPolicyStore(nil, logger.NewNopLogger(), nil)
	c := NewPolicyCache("", logger.NewNopLogger())
	c.SetBindings(writeBindingsFile(t), "ctrl-hq-1")
	s.SetCache(c)
	if got := s.LoadLocal(72 * time.Hour); got != "bindings" {
		t.Fatalf("LoadLocal = %q, want bindings", got)
	}
	reader := drivers.NewMockReader(8)
	emit := &fakeEmitter{}
	rt := NewRuntime("hq", s, reader, nil, nil, emit, subjects.Default(), logger.NewNopLogger(), nil)
	hw := &countingHW{MockHardware: drivers.NewMockHardware(nil), arms: map[string]int{}}
	mgr := NewPortalManager("ctrl-hq-1", "hq", s, newFakeArmer(), rt, hw, logger.NewNopLogger())
	mgr.reconcile()
	return s, rt, mgr, hw, reader, emit
}

// A bindings-only boot arms the portal with its hardware fields, the maglock is
// not held (so it is locked), a tap denies at the posture gate, and lock commands
// are refused.
func TestBindingsBootArmsLockedAndDenies(t *testing.T) {
	s, rt, _, hw, reader, emit := bindingsBoot(t)

	if hw.arms["lobby-main"] != 1 {
		t.Fatalf("lobby-main armed %d times, want 1", hw.arms["lobby-main"])
	}
	if _, ok := hw.arms["dock-1"]; ok {
		t.Error("armed another controller's portal")
	}
	b, _ := s.Binding("lobby-main")
	if !b.Maglock || !b.DpsInvert || !b.RexInvert || b.LockRelay != 2 || b.ReaderAddress != 7 {
		t.Errorf("binding = %+v, want the snapshot's hardware fields", b)
	}
	if b.RexUnlock || b.HeldOpenSeconds != 0 || b.Area != "" {
		t.Errorf("binding = %+v, want no operational fields", b)
	}
	if state, _ := s.SyncStatus(); state != "loading" {
		t.Errorf("state = %q, want loading (a bindings load is not policy)", state)
	}
	lock, _ := hw.Lock("lobby-main")
	if lock.Held() {
		t.Error("maglock held open after a bindings-only boot, want locked")
	}

	rt.SetPosture("lobby-main", policy.PostureUnlocked, "guard", "", time.Now().UTC())
	if lock.Held() {
		t.Error("cmd.posture unlocked was not refused before policy synced")
	}
	if p, _, _ := rt.effectivePosture("lobby-main", time.Now().UTC()); p != "" {
		t.Errorf("posture = %q after a refused override, want none", p)
	}
	rt.Grant("lobby-main", 3, "guard", "", "")
	if p := lock.Pulses(); len(p) != 0 {
		t.Errorf("cmd.grant pulsed %v before policy synced", p)
	}

	drain(rt, reader, drivers.Tap{Portal: "lobby-main", Credential: "CARD-001", At: ny(t, 2026, 1, 5, 9, 0)})
	taps := emit.taps()
	if len(taps) != 1 || taps[0].Allow || taps[0].Reason != policy.ReasonDenyPointDisabled {
		t.Errorf("tap events = %+v, want one deny_point_disabled", taps)
	}
	if p := lock.Pulses(); len(p) != 0 {
		t.Errorf("tap pulsed %v before policy synced", p)
	}
}

// When live policy lands after a bindings-only boot, the portal is not re-armed
// (a re-arm would release the maglock for a moment), its posture applies on the
// normal hold path, and lock commands are accepted.
func TestBindingsBootLiveSyncNoReArm(t *testing.T) {
	s, rt, mgr, hw, _, _ := bindingsBoot(t)

	for _, r := range bindingsRecords {
		s.apply(r.key, []byte(r.val))
	}
	s.readyOnce.Do(func() { close(s.ready) }) // the sync sentinel
	mgr.reconcile()

	if hw.arms["lobby-main"] != 1 {
		t.Fatalf("lobby-main armed %d times after live sync, want 1 (no re-arm)", hw.arms["lobby-main"])
	}
	lock, _ := hw.Lock("lobby-main")
	// Sat 2026-01-10: the auto schedule is closed, so the standing unlocked applies.
	rt.reconcileHolds(ny(t, 2026, 1, 10, 9, 0))
	if !lock.Held() {
		t.Error("standing unlocked posture not applied after live sync")
	}

	rt.SetPosture("lobby-main", policy.PostureLockdown, "guard", "", time.Now().UTC())
	if p, _, _ := rt.effectivePosture("lobby-main", time.Now().UTC()); p != policy.PostureLockdown {
		t.Errorf("posture = %q, want lockdown (commands accepted once synced)", p)
	}
	if lock.Held() {
		t.Error("lockdown override did not release the hold")
	}
}

// A fresh full cache wins over the bindings snapshot: its postures and
// credentials load, the store reports cached, and commands are accepted.
func TestFullCacheWinsOverBindings(t *testing.T) {
	dir := t.TempDir()
	c := NewPolicyCache(filepath.Join(dir, "policy-cache.json"), logger.NewNopLogger())
	c.SetBindings(filepath.Join(dir, "bindings.json"), "ctrl-hq-1")
	for _, r := range bindingsRecords {
		c.put(r.key, []byte(r.val))
	}
	c.markSynced(time.Now())
	c.drain()

	s := NewPolicyStore(nil, logger.NewNopLogger(), nil)
	s.SetCache(c)
	if got := s.LoadLocal(72 * time.Hour); got != "cache" {
		t.Fatalf("LoadLocal = %q, want cache", got)
	}
	if ap, _ := s.Portal("lobby-main"); ap.Posture != policy.PostureUnlocked {
		t.Errorf("posture = %q, want the cached unlocked (bindings must not overwrite it)", ap.Posture)
	}
	if b, _ := s.Binding("lobby-main"); b.HeldOpenSeconds != 30 || !b.RexUnlock {
		t.Errorf("binding = %+v, want the cached operational fields", b)
	}
	rt := NewRuntime("hq", s, drivers.NewMockReader(1), nil,
		map[string]drivers.LockDriver{"lobby-main": drivers.NewMockLock("lobby-main", nil)}, &fakeEmitter{},
		subjects.Default(), logger.NewNopLogger(), nil)
	rt.SetPosture("lobby-main", policy.PostureLockdown, "guard", "", time.Now().UTC())
	if p, _, _ := rt.effectivePosture("lobby-main", time.Now().UTC()); p != policy.PostureLockdown {
		t.Errorf("posture = %q, want lockdown (commands accepted on a cache boot)", p)
	}
}

// A missing or corrupt bindings snapshot loads nothing: the old boot, nothing armed.
func TestBindingsMissingOrCorrupt(t *testing.T) {
	dir := t.TempDir()
	corrupt := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(corrupt, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(dir, "missing.json"), corrupt} {
		s := NewPolicyStore(nil, logger.NewNopLogger(), nil)
		c := NewPolicyCache("", logger.NewNopLogger())
		c.SetBindings(path, "ctrl-hq-1")
		s.SetCache(c)
		if got := s.LoadLocal(72 * time.Hour); got != "" {
			t.Errorf("%s: LoadLocal = %q, want nothing loaded", filepath.Base(path), got)
		}
		if n := s.Counts()["portals"]; n != 0 {
			t.Errorf("%s: %d portals loaded, want 0", filepath.Base(path), n)
		}
	}
}
