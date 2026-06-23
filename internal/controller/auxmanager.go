package controller

import (
	"context"
	"sync"

	"github.com/stone-age-io/access-control/internal/drivers"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/policykv"
)

// AuxHardware arms and releases the physical I/O for auxiliary points: an output
// relay (returned as a LockDriver the runtime drives) and an input line (whose
// edges a backend routes into the runtime's DoorInput with kind InputAux). The
// mock backend hands back a mock lock and no input edges; the GPIO backend
// resolves logical indices through the model profile. Both drivers.MockHardware
// and the GPIO backend satisfy it.
type AuxHardware interface {
	ArmOutput(code string, relayIndex int) (drivers.LockDriver, error)
	DisarmOutput(code string)
	ArmInput(code string, inputIndex int, invert bool) error
	DisarmInput(code string)
}

// AuxManager keeps the controller's armed auxiliary points in step with policy —
// the sibling of PortalManager for aux inputs/outputs. The desired set is every
// aux record whose controller relation points at this controller's code
// (PolicyStore.AuxInputsForController / AuxOutputsForController). It reconciles on
// every policy change (coalesced, off the watch goroutine): a new/retyped index
// arms or re-arms, a removed/reassigned point disarms. Like PortalManager it boots
// armed for nothing and converges as policy arrives.
type AuxManager struct {
	code     string
	location string
	store    *PolicyStore
	rt       *Runtime
	hw       AuxHardware
	log      *logger.Logger

	dirty  chan struct{}
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Goroutine-confined to the reconcile loop; no lock needed.
	armedInputs  map[string]auxInSig // code -> armed input (index + contact sense)
	armedOutputs map[string]int      // code -> armed relay index
}

// NewAuxManager builds an aux reconciler for the controller with the given code.
func NewAuxManager(code, location string, store *PolicyStore, rt *Runtime, hw AuxHardware, log *logger.Logger) *AuxManager {
	return &AuxManager{
		code:         code,
		location:     location,
		store:        store,
		rt:           rt,
		hw:           hw,
		log:          log.With("component", "aux-manager"),
		dirty:        make(chan struct{}, 1),
		armedInputs:  make(map[string]auxInSig),
		armedOutputs: make(map[string]int),
	}
}

// Notify signals that policy changed. Non-blocking and coalescing; wire it to
// PolicyStore.SetOnChange (alongside the portal manager).
func (am *AuxManager) Notify() {
	select {
	case am.dirty <- struct{}{}:
	default:
	}
}

// Start launches the reconcile loop (reconciles once immediately, then on Notify).
func (am *AuxManager) Start(ctx context.Context) {
	rctx, cancel := context.WithCancel(ctx)
	am.cancel = cancel
	am.wg.Add(1)
	go func() {
		defer am.wg.Done()
		am.reconcile()
		for {
			select {
			case <-rctx.Done():
				return
			case <-am.dirty:
				am.reconcile()
			}
		}
	}()
}

// Stop ends the reconcile loop and waits for it to exit.
func (am *AuxManager) Stop() {
	if am.cancel != nil {
		am.cancel()
	}
	am.wg.Wait()
}

// reconcile arms/disarms aux points to match the set bound to this controller.
// Runs only on the reconcile goroutine, so the armed maps need no locking.
func (am *AuxManager) reconcile() {
	desiredOut := make(map[string]policykv.AuxOutput)
	desiredIn := make(map[string]policykv.AuxInput)
	if am.code != "" {
		for _, ao := range am.store.AuxOutputsForController(am.code) {
			desiredOut[ao.Code] = ao
		}
		for _, ai := range am.store.AuxInputsForController(am.code) {
			desiredIn[ai.Code] = ai
		}
	}

	// Disarm anything armed that is no longer desired.
	for code := range am.armedOutputs {
		if _, ok := desiredOut[code]; !ok {
			am.log.Info("aux output no longer assigned here; disarming", "code", code)
			am.disarmOutput(code)
		}
	}
	for code := range am.armedInputs {
		if _, ok := desiredIn[code]; !ok {
			am.log.Info("aux input no longer assigned here; disarming", "code", code)
			am.disarmInput(code)
		}
	}

	// Arm new; re-arm any whose index changed.
	for code, ao := range desiredOut {
		am.warnLocation(code, ao.Location)
		cur, armed := am.armedOutputs[code]
		switch {
		case !armed:
			am.armOutput(ao)
		case cur != ao.RelayIndex:
			am.log.Info("aux output relay changed; re-arming", "code", code, "from", cur, "to", ao.RelayIndex)
			am.disarmOutput(code)
			am.armOutput(ao)
		}
	}
	for code, ai := range desiredIn {
		am.warnLocation(code, ai.Location)
		cur, armed := am.armedInputs[code]
		sig := auxInSig{index: ai.InputIndex, invert: ai.Invert()}
		switch {
		case !armed:
			am.armInput(ai)
		case cur != sig:
			am.log.Info("aux input wiring changed; re-arming", "code", code, "fromIndex", cur.index, "toIndex", sig.index)
			am.disarmInput(code)
			am.armInput(ai)
		}
	}
}

func (am *AuxManager) warnLocation(code, loc string) {
	if loc != "" && loc != am.location {
		am.log.Warn("aux point location differs from controller location",
			"code", code, "auxLocation", loc, "controllerLocation", am.location)
	}
}

func (am *AuxManager) armOutput(ao policykv.AuxOutput) {
	lock, err := am.hw.ArmOutput(ao.Code, ao.RelayIndex)
	if err != nil {
		am.log.Error("failed to arm aux output; will retry on next policy change", "code", ao.Code, "error", err)
		return
	}
	am.rt.SetAuxOutput(ao.Code, ao.Location, ao.PulseSeconds, lock)
	am.armedOutputs[ao.Code] = ao.RelayIndex
}

func (am *AuxManager) disarmOutput(code string) {
	am.hw.DisarmOutput(code)
	am.rt.DeleteAuxOutput(code)
	delete(am.armedOutputs, code)
}

func (am *AuxManager) armInput(ai policykv.AuxInput) {
	if err := am.hw.ArmInput(ai.Code, ai.InputIndex, ai.Invert()); err != nil {
		am.log.Error("failed to arm aux input; will retry on next policy change", "code", ai.Code, "error", err)
		return
	}
	am.rt.ArmAuxInput(ai.Code, ai.Location)
	am.armedInputs[ai.Code] = auxInSig{index: ai.InputIndex, invert: ai.Invert()}
}

// auxInSig is the part of an aux input's binding that determines how its line is
// requested (index + contact sense); a change re-arms it. Comparable for ==.
type auxInSig struct {
	index  int
	invert bool
}

func (am *AuxManager) disarmInput(code string) {
	am.hw.DisarmInput(code)
	am.rt.DeleteAuxInput(code)
	delete(am.armedInputs, code)
}
