package controller

import (
	"context"
	"sync"

	"github.com/stone-age-io/access-control/internal/drivers"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/policy"
)

// portalArmer is the reader surface the reconciler drives: arm/disarm one
// portal's tap subscription. *NATSReader implements it.
type portalArmer interface {
	Arm(p Portal) error
	Disarm(code string)
}

// PortalHardware arms and releases the physical I/O for one portal: its lock
// relay (returned as the LockDriver the tap loop pulses) and its DPS/REX input
// lines (which a hardware backend routes into the runtime's DoorInput). The mock
// backend (drivers.MockHardware) hands back a mock lock and no inputs; the GPIO
// backend (internal/drivers/gpio) resolves the logical relay/input indices
// through the controller model's hardware profile to physical lines.
type PortalHardware interface {
	Arm(code string, lockRelay, dpsInput, rexInput int) (drivers.LockDriver, error)
	Disarm(code string)
}

// PortalManager keeps the controller's *armed* portals — the readers and locks
// it has actually wired up — in step with policy. Which portals this box drives
// comes from policy too: the desired set is every portal whose `controller`
// relation points at this controller's code (PolicyStore.PortalsForController).
// That set, and each portal's type, can change after boot — a portal may be
// created, retyped, reassigned to another box, or removed — so rather than
// resolve once at startup and freeze, the manager reconciles on every policy
// change (via PolicyStore.SetOnChange):
//
//   - portal newly assigned here, resolvable (has a type), not armed -> arm
//   - armed portal's type changed                                    -> re-arm
//   - armed portal reassigned elsewhere / removed                    -> disarm
//
// This is also what lets the controller boot before policy is available: it
// comes up armed for nothing (default-deny, safe) and arms each portal as policy
// arrives, instead of crashing or freezing.
//
// Change notifications are coalesced through a depth-1 channel so a burst (e.g. a
// full re-sync after reconnect re-delivers every key) collapses to one
// reconcile, and reconciliation runs on its own goroutine — never on the watch
// goroutine, which must not block on NATS subscribe/unsubscribe calls.
type PortalManager struct {
	code     string // this controller's code; arms portals bound to it
	location string // controller home location (for the mismatch warning)
	store    *PolicyStore
	armer    portalArmer
	rt       *Runtime
	hw       PortalHardware
	log      *logger.Logger

	dirty  chan struct{}
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Goroutine-confined to the reconcile loop; no lock needed.
	armed map[string]string // code -> armed type
}

// NewPortalManager builds a reconciler for the controller with the given code; it
// arms the portals bound to that code in policy. hw arms a portal's physical I/O
// (lock relay + DPS/REX inputs) when the portal is armed — the mock backend in
// dev, the GPIO backend on real hardware.
func NewPortalManager(code, location string, store *PolicyStore, armer portalArmer, rt *Runtime, hw PortalHardware, log *logger.Logger) *PortalManager {
	return &PortalManager{
		code:     code,
		location: location,
		store:    store,
		armer:    armer,
		rt:       rt,
		hw:       hw,
		log:      log.With("component", "portal-manager"),
		dirty:    make(chan struct{}, 1),
		armed:    make(map[string]string),
	}
}

// Notify signals that policy changed. Non-blocking and coalescing: wire it to
// PolicyStore.SetOnChange. Safe to call before Start (the pending tick is read
// once the loop runs).
func (pm *PortalManager) Notify() {
	select {
	case pm.dirty <- struct{}{}:
	default:
	}
}

// Start launches the reconcile loop. It reconciles once immediately (converging
// against whatever policy is already loaded) and then on every Notify until the
// context is cancelled or Stop is called.
func (pm *PortalManager) Start(ctx context.Context) {
	rctx, cancel := context.WithCancel(ctx)
	pm.cancel = cancel
	pm.wg.Add(1)
	go func() {
		defer pm.wg.Done()
		pm.reconcile()
		for {
			select {
			case <-rctx.Done():
				return
			case <-pm.dirty:
				pm.reconcile()
			}
		}
	}()
}

// Stop ends the reconcile loop and waits for it to exit.
func (pm *PortalManager) Stop() {
	if pm.cancel != nil {
		pm.cancel()
	}
	pm.wg.Wait()
}

// reconcile arms/disarms portals to match the set bound to this controller in
// policy. Runs only on the reconcile goroutine, so armed needs no locking.
func (pm *PortalManager) reconcile() {
	// Desired set: portals assigned to this controller that have resolved a type.
	// With no controller code configured, drive nothing — never match the
	// unassigned portals (whose controller is also "").
	desired := make(map[string]policy.Portal)
	if pm.code != "" {
		for _, ap := range pm.store.PortalsForController(pm.code) {
			if ap.Type == "" {
				continue // not yet resolvable; arm once it gains a type
			}
			desired[ap.Code] = ap
		}
	}

	// Disarm anything armed that is no longer assigned here (reassigned/removed).
	// Deleting from pm.armed mid-range is safe; disarm does the delete.
	for code := range pm.armed {
		if _, ok := desired[code]; !ok {
			pm.log.Info("portal no longer assigned to this controller; disarming", "portal", code)
			pm.disarm(code)
		}
	}

	// Arm new portals; re-arm any whose type changed.
	for code, ap := range desired {
		curType, isArmed := pm.armed[code]

		if ap.Location != "" && ap.Location != pm.location {
			// Commands and the fire signal are subscribed on the controller's home
			// location; a portal placed elsewhere won't receive them. Surface it.
			pm.log.Warn("portal location differs from controller location",
				"portal", code, "portalLocation", ap.Location, "controllerLocation", pm.location)
		}

		switch {
		case !isArmed:
			pm.arm(ap)
		case curType != ap.Type:
			pm.log.Info("portal type changed; re-arming", "portal", code, "from", curType, "to", ap.Type)
			pm.disarm(code)
			pm.arm(ap)
		}
	}
}

func (pm *PortalManager) arm(ap policy.Portal) {
	// Arm physical I/O first (it can fail — a GPIO line may be missing/busy). The
	// hardware backend resolves the portal's logical relay/input indices itself.
	b, _ := pm.store.Binding(ap.Code)
	lock, err := pm.hw.Arm(ap.Code, b.LockRelay, b.DpsInput, b.RexInput)
	if err != nil {
		// Leave it unarmed and unrecorded; the next change re-attempts.
		pm.log.Error("failed to arm portal hardware; will retry on next policy change", "portal", ap.Code, "error", err)
		return
	}
	if err := pm.armer.Arm(Portal{Code: ap.Code, Type: ap.Type, Location: ap.Location, Address: b.ReaderAddress}); err != nil {
		pm.hw.Disarm(ap.Code) // roll back the hardware we just armed
		pm.log.Error("failed to arm portal reader; will retry on next policy change", "portal", ap.Code, "error", err)
		return
	}
	pm.rt.SetLock(ap.Code, lock)
	pm.armed[ap.Code] = ap.Type
	// Set the strike's standing hold to match effective posture right away, so a
	// scheduled-/standing-unlocked portal opens on arm without waiting for the
	// next hold-eval tick.
	pm.rt.ApplyHold(ap.Code)
}

func (pm *PortalManager) disarm(code string) {
	pm.armer.Disarm(code)
	pm.rt.DeleteLock(code)
	pm.hw.Disarm(code)
	delete(pm.armed, code)
}
