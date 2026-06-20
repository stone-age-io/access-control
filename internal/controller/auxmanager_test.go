package controller

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stone-age-io/access-control/internal/drivers"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/statuskv"
	"github.com/stone-age-io/access-control/internal/subjects"
)

func auxManagerFor(t *testing.T, code string) (*AuxManager, *drivers.MockHardware, *Runtime, *PolicyStore, *fakeStatusKV) {
	t.Helper()
	store := seeded(t)
	kv := newFakeStatusKV()
	w := NewStatusWriter(kv, "ctrl-hq-1", logger.NewNopLogger())
	rt := NewRuntime("hq", store, drivers.NewMockReader(1), nil, nil, &fakeEmitter{},
		subjects.Default(), logger.NewNopLogger(), nil)
	rt.SetStatusWriter(w)
	hw := drivers.NewMockHardware(nil)
	am := NewAuxManager(code, "hq", store, rt, hw, logger.NewNopLogger())
	return am, hw, rt, store, kv
}

// Arming an aux output reaches the hardware, and driving it on energizes the relay
// and publishes its shadow.
func TestAuxReconcileArmsAndDrivesOutput(t *testing.T) {
	am, hw, rt, store, kv := auxManagerFor(t, "ctrl-hq-1")
	store.apply("auxout.gate-1", []byte(`{"code":"gate-1","location":"hq","controller":"ctrl-hq-1","relayIndex":3,"pulseSeconds":4}`))
	am.reconcile()

	lock, ok := hw.AuxOutputLock("gate-1")
	if !ok {
		t.Fatal("aux output not armed in hardware")
	}

	rt.DriveOutput("gate-1", "on", 0, "guard", "open gate")
	if !lock.Held() {
		t.Error("aux output relay not energized after on")
	}

	rt.statusWriter.drain(context.Background())
	if v, ok := readAuxOut(kv, "gate-1"); !ok || !v.Energized {
		t.Errorf("aux output status not energized: %+v (ok=%v)", v, ok)
	}

	rt.DriveOutput("gate-1", "off", 0, "guard", "")
	if lock.Held() {
		t.Error("aux output relay still energized after off")
	}
}

// Pulse uses the configured default when seconds<=0.
func TestAuxDriveOutputPulseDefault(t *testing.T) {
	am, hw, rt, store, _ := auxManagerFor(t, "ctrl-hq-1")
	store.apply("auxout.gate-1", []byte(`{"code":"gate-1","location":"hq","controller":"ctrl-hq-1","relayIndex":3,"pulseSeconds":4}`))
	am.reconcile()
	lock, _ := hw.AuxOutputLock("gate-1")

	rt.DriveOutput("gate-1", "pulse", 0, "guard", "")
	if got := lock.Pulses(); len(got) != 1 || got[0] != 4 {
		t.Errorf("pulses = %v, want [4] (configured default)", got)
	}
}

// Removing an aux output from policy disarms it.
func TestAuxReconcileDisarmsOutput(t *testing.T) {
	am, hw, _, store, _ := auxManagerFor(t, "ctrl-hq-1")
	store.apply("auxout.gate-1", []byte(`{"code":"gate-1","location":"hq","controller":"ctrl-hq-1","relayIndex":3}`))
	am.reconcile()
	if _, ok := hw.AuxOutputLock("gate-1"); !ok {
		t.Fatal("not armed after reconcile")
	}

	store.remove("auxout.gate-1")
	am.reconcile()
	if _, ok := hw.AuxOutputLock("gate-1"); ok {
		t.Error("aux output still armed after removal")
	}
}

// An aux input publishes inactive on arm, then its transition.
func TestAuxInputArmAndTransition(t *testing.T) {
	am, _, rt, store, kv := auxManagerFor(t, "ctrl-hq-1")
	store.apply("auxin.dock-sensor", []byte(`{"code":"dock-sensor","location":"hq","controller":"ctrl-hq-1","inputIndex":5}`))
	am.reconcile()

	rt.statusWriter.drain(context.Background())
	if v, ok := readAuxIn(kv, "dock-sensor"); !ok || v.Active {
		t.Fatalf("initial aux input status should be inactive: %+v (ok=%v)", v, ok)
	}

	rt.handleInput(drivers.InputEvent{Portal: "dock-sensor", Kind: drivers.InputAux, Active: true})
	rt.statusWriter.drain(context.Background())
	if v, ok := readAuxIn(kv, "dock-sensor"); !ok || !v.Active {
		t.Errorf("aux input not active after transition: %+v (ok=%v)", v, ok)
	}
}

func readAuxOut(kv *fakeStatusKV, code string) (statuskv.AuxOutputStatus, bool) {
	store, _, _ := kv.snapshot()
	b, ok := store[statuskv.PrefixAuxOut+code]
	if !ok {
		return statuskv.AuxOutputStatus{}, false
	}
	var v statuskv.AuxOutputStatus
	_ = json.Unmarshal(b, &v)
	return v, true
}

func readAuxIn(kv *fakeStatusKV, code string) (statuskv.AuxInputStatus, bool) {
	store, _, _ := kv.snapshot()
	b, ok := store[statuskv.PrefixAuxIn+code]
	if !ok {
		return statuskv.AuxInputStatus{}, false
	}
	var v statuskv.AuxInputStatus
	_ = json.Unmarshal(b, &v)
	return v, true
}
