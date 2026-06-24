package controller

import (
	"context"
	"sync"
	"time"

	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/statuskv"
)

// AreaShadowWriter is the slice of StatusWriter the AreaManager needs: write or
// drop this controller's arm shadow for an area. Abstracted so the reconcile
// logic is testable without a KV bucket (the StatusWriter satisfies it).
type AreaShadowWriter interface {
	SetArea(areaCode, location, arm, source string, peers []string, at time.Time)
	DeleteArea(areaCode string)
}

// AreaManager keeps this controller's per-area arm shadows in step with policy —
// the sibling of AuxManager for areas. The desired set is every area with at least
// one member aux_input on this box (PolicyStore.AreasForController). It reconciles
// on every policy change (coalesced, off the watch goroutine) AND on the runtime's
// hold-eval tick (so a scheduled-arm window boundary refreshes the shadow with no
// policy event and no new timer — D5). Each shadow carries the full participant set
// (peers), so the console can aggregate trustworthy area arm-state across boxes.
//
// Arm-state boundaries fire NO alarm by themselves; they only change whether a
// future intrusion trip alarms. So this manager never emits — it only publishes
// status.
type AreaManager struct {
	code     string
	location string
	store    *PolicyStore
	sw       AreaShadowWriter
	log      *logger.Logger

	dirty  chan struct{}
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Goroutine-confined to the reconcile loop; no lock needed.
	shadowed map[string]struct{} // area codes this box currently shadows (for removal)
}

// NewAreaManager builds an area reconciler for the controller with the given code.
// sw may be nil (e.g. a controller publishing no status), in which case the
// manager is an inert no-op.
func NewAreaManager(code, location string, store *PolicyStore, sw AreaShadowWriter, log *logger.Logger) *AreaManager {
	return &AreaManager{
		code:     code,
		location: location,
		store:    store,
		sw:       sw,
		log:      log.With("component", "area-manager"),
		dirty:    make(chan struct{}, 1),
		shadowed: make(map[string]struct{}),
	}
}

// Notify signals that policy changed (or the tick fired). Non-blocking and
// coalescing; wire it to PolicyStore.SetOnChange and the runtime tick hook.
func (am *AreaManager) Notify() {
	select {
	case am.dirty <- struct{}{}:
	default:
	}
}

// Start launches the reconcile loop (reconciles once immediately, then on Notify).
// A nil shadow writer makes Start a no-op.
func (am *AreaManager) Start(ctx context.Context) {
	if am.sw == nil {
		return
	}
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
func (am *AreaManager) Stop() {
	if am.cancel != nil {
		am.cancel()
	}
	am.wg.Wait()
}

// reconcile writes the arm shadow for every area this box participates in, and
// drops the shadow for any it no longer does. Runs only on the reconcile
// goroutine, so shadowed needs no locking. The StatusWriter dedups on value, so a
// re-write of an unchanged shadow is a no-op (no churn on the tick).
func (am *AreaManager) reconcile() {
	now := time.Now().UTC()

	desired := make(map[string]string) // area code -> location
	if am.code != "" {
		for _, a := range am.store.AreasForController(am.code) {
			loc := a.Location
			if loc == "" {
				loc = am.location
			}
			desired[a.Code] = loc
		}
	}

	// Drop shadows for areas no longer participated in here (reassigned/removed).
	for code := range am.shadowed {
		if _, ok := desired[code]; !ok {
			am.log.Info("area no longer has a member input here; dropping shadow", "area", code)
			am.sw.DeleteArea(code)
			delete(am.shadowed, code)
		}
	}

	// Write/refresh each desired area's shadow. ResolveArmState returns the safe
	// standing value when a schedule isn't loaded yet, and the shadow has no
	// physical effect, so we always publish what it returns (no flap concern).
	for code, loc := range desired {
		am.warnLocation(code, loc)
		armed, source, _ := am.store.ResolveArmState(code, now)
		arm := statuskv.AreaDisarmed
		if armed {
			arm = statuskv.AreaArmed
		}
		peers := am.store.AreaControllers(code)
		am.sw.SetArea(code, loc, arm, source, peers, now)
		am.shadowed[code] = struct{}{}
	}
}

// warnLocation flags an area whose location differs from this controller's: an
// area is single-location, so a member input on a foreign-location box is a
// misconfig (which location's fire input suppresses it is then ambiguous).
func (am *AreaManager) warnLocation(code, loc string) {
	if loc != "" && am.location != "" && loc != am.location {
		am.log.Warn("area location differs from controller location",
			"area", code, "areaLocation", loc, "controllerLocation", am.location)
	}
}
