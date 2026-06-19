package controller

import (
	"context"
	"sync"

	"github.com/stone-age-io/access-control/internal/drivers"
	"github.com/stone-age-io/access-control/internal/logger"
)

// portalArmer is the reader surface the reconciler drives: arm/disarm one
// portal's tap subscription. *NATSReader implements it.
type portalArmer interface {
	Arm(p Portal) error
	Disarm(code string)
}

// PortalManager keeps the controller's *armed* portals — the readers and locks
// it has actually wired up — in step with policy. The set of portal codes a
// controller is responsible for is still local config (which doors this box is
// physically wired to), but each portal's type and existence come from the
// policy graph, which can change after boot: a portal may be created in accessd
// later, retyped, or removed. Rather than resolve once at startup and freeze,
// the manager reconciles on every policy change (via PolicyStore.SetOnChange):
//
//   - configured portal now resolvable (exists, has a type) and not armed -> arm
//   - armed portal's type changed                                         -> re-arm
//   - armed portal no longer resolvable                                   -> disarm
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
	codes    []string // configured portal codes this controller drives
	location string   // controller home location (for the mismatch warning)
	store    *PolicyStore
	armer    portalArmer
	rt       *Runtime
	mkLock   func(code string) drivers.LockDriver
	log      *logger.Logger

	dirty  chan struct{}
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Goroutine-confined to the reconcile loop; no lock needed.
	armed   map[string]string // code -> armed type
	missing map[string]bool   // code -> already warned it's absent from policy
}

// NewPortalManager builds a reconciler for the given configured portal codes.
// mkLock manufactures a portal's lock driver when it is armed (the mock lock in
// v1; real GPIO later).
func NewPortalManager(codes []string, location string, store *PolicyStore, armer portalArmer, rt *Runtime, mkLock func(code string) drivers.LockDriver, log *logger.Logger) *PortalManager {
	return &PortalManager{
		codes:    codes,
		location: location,
		store:    store,
		armer:    armer,
		rt:       rt,
		mkLock:   mkLock,
		log:      log.With("component", "portal-manager"),
		dirty:    make(chan struct{}, 1),
		armed:    make(map[string]string),
		missing:  make(map[string]bool),
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

// reconcile walks the configured portals and arms/disarms each to match policy.
// Runs only on the reconcile goroutine, so armed/missing need no locking.
func (pm *PortalManager) reconcile() {
	for _, code := range pm.codes {
		ap, ok := pm.store.Portal(code)
		resolvable := ok && ap.Type != ""
		curType, isArmed := pm.armed[code]

		if !resolvable {
			if isArmed {
				pm.log.Warn("portal no longer in policy; disarming", "portal", code)
				pm.disarm(code)
			} else if !pm.missing[code] {
				pm.missing[code] = true
				pm.log.Warn("configured portal not in policy; not armed (will arm when it appears)",
					"portal", code, "hint", "create the portal in accessd with a type")
			}
			continue
		}
		delete(pm.missing, code)

		if ap.Location != "" && ap.Location != pm.location {
			// Taps and commands are addressed on the controller's home location;
			// a portal placed elsewhere in policy won't line up. Surface it rather
			// than silently arm a reader on the wrong subject tree.
			pm.log.Warn("portal location differs from controller location",
				"portal", code, "portalLocation", ap.Location, "controllerLocation", pm.location)
		}

		switch {
		case !isArmed:
			pm.arm(code, ap.Type)
		case curType != ap.Type:
			pm.log.Info("portal type changed; re-arming", "portal", code, "from", curType, "to", ap.Type)
			pm.disarm(code)
			pm.arm(code, ap.Type)
		}
	}
}

func (pm *PortalManager) arm(code, ptype string) {
	if err := pm.armer.Arm(Portal{Code: code, Type: ptype}); err != nil {
		// Leave it unarmed and unrecorded; the next change re-attempts.
		pm.log.Error("failed to arm portal; will retry on next policy change", "portal", code, "error", err)
		return
	}
	pm.rt.SetLock(code, pm.mkLock(code))
	pm.armed[code] = ptype
}

func (pm *PortalManager) disarm(code string) {
	pm.armer.Disarm(code)
	pm.rt.DeleteLock(code)
	delete(pm.armed, code)
}
