// Package armrelease is accessd's one-shot disarm release: a periodic sweep that
// clears a stale disarm override once the area's scheduled arm-state has moved on.
//
// A disarm — whether an operator's manual "Disarm" or an entry-disarm badge at a
// disarm_on_grant portal — writes a durable areas.arm_override=disarmed. On a
// SCHEDULED area that override is meant to be *one-shot*: it should last only until the
// next scheduled arm, so "arm overnight on a schedule, first badge in the morning
// disarms it" loops on its own without an operator clearing the override every day.
//
// The rule is deliberately simple: drop the override the moment the area's BASE
// arm-state (schedule+standing, override excluded) is disarmed. We do NOT track the
// "next arm" edge across ticks or reboots. While the base is armed — the window the
// operator disarmed *into* — the override is kept, so the disarm holds. Once the base
// goes disarmed the override is holding nothing (the area is disarmed either way), so
// clearing it is invisible; the area then re-arms cleanly on schedule at the next
// window with no stale override in the way. An area with no auto_schedule never has a
// disarmed base on a schedule, so its override is never auto-cleared — it stays until
// an operator clears it, as intended. See policysnapshot.ShouldReleaseDisarm.
//
// It lives centrally in accessd (which owns durable arm-state) and writes the same
// areas.arm_override the manual/entry-disarm paths write; the mirror propagates the
// clear to KV where every controller re-resolves. Reusing internal/policysnapshot to
// resolve the schedule keeps arm-state resolution in one home, identical to the
// controller's, without importing the edge runtime.
package armrelease

import (
	"context"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/policysnapshot"
)

// releaseInterval is how often the sweep runs. One minute is ample: re-arming is
// driven by the controller's own hold-eval tick once the override is gone, so this
// sweep only has to clear the override sometime during the (typically hours-long)
// disarmed gap — it is never on the critical path for re-arm timing. A var so tests
// can shorten it.
var releaseInterval = time.Minute

// kvReadTimeout bounds the one-shot KV drain per sweep.
const kvReadTimeout = 10 * time.Second

// armResolver is the slice of policysnapshot.Snapshot the release logic needs: decide
// whether a scheduled area's disarm override should be dropped now. Abstracted so
// releaseStale is testable with a fake — no NATS, no KV.
type armResolver interface {
	ShouldReleaseDisarm(areaCode string, atUTC time.Time) bool
}

// Releaser periodically releases stale one-shot disarm overrides. It owns its own
// lifetime (like health.Monitor): Start launches the sweep loop, Stop ends it.
type Releaser struct {
	app core.App
	kv  jetstream.KeyValue
	log *logger.Logger

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates a Releaser. kv is the read side of the policy bucket (accessd owns it).
func New(app core.App, kv jetstream.KeyValue, log *logger.Logger) *Releaser {
	return &Releaser{app: app, kv: kv, log: log.With("component", "arm-release")}
}

// Start launches the sweep loop on its own context (cancelled by Stop), so it lives
// for the whole serve lifetime rather than the caller's boot context.
func (r *Releaser) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.wg.Add(1)
	go r.loop(ctx)
	r.log.Info("one-shot disarm release started", "every", releaseInterval)
}

// Stop ends the sweep loop and waits for it to exit.
func (r *Releaser) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
	r.wg.Wait()
}

func (r *Releaser) loop(ctx context.Context) {
	defer r.wg.Done()
	t := time.NewTicker(releaseInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.reconcile(time.Now().UTC())
		}
	}
}

// reconcile drains the policy KV into a snapshot, then releases stale disarm overrides.
// The KV read is the only NATS-bound step; releaseStale is pure over (app, resolver),
// so the release logic is testable without NATS. A KV read failure skips this sweep
// (the next one retries) — fail-safe, since skipping only defers a release.
func (r *Releaser) reconcile(now time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), kvReadTimeout)
	defer cancel()
	entries, err := drainKV(ctx, r.kv)
	if err != nil {
		r.log.Error("policy KV read failed; skipping sweep", "error", err)
		return
	}
	releaseStale(r.app, policysnapshot.Build(entries), now, r.log)
}

// releaseStale clears the disarm override on every area whose override should be
// released now (see policysnapshot.ShouldReleaseDisarm). It selects the disarm-
// overridden areas from PocketBase (the authoritative, writable record) and lets the
// snapshot make the schedule decision, so the two never disagree.
func releaseStale(app core.App, r armResolver, now time.Time, log *logger.Logger) {
	// The filter value is a constant, not user input.
	areas, err := app.FindRecordsByFilter("areas", "arm_override = 'disarmed'", "", 0, 0)
	if err != nil {
		log.Error("area query failed", "error", err)
		return
	}
	for _, a := range areas {
		code := a.GetString("code")
		if !r.ShouldReleaseDisarm(code, now) {
			continue
		}
		a.Set("arm_override", "")
		if err := app.Save(a); err != nil {
			log.Error("failed to clear disarm override", "area", code, "error", err)
			continue
		}
		log.Info("released one-shot disarm; area resumes its schedule", "area", code)
	}
}

// drainKV reads the current value of every key in the bucket via a one-shot WatchAll
// drain — the twin of simulateapi.snapshotKV (kept local: natsx deliberately excludes
// watch/subscription concerns, and policysnapshot must stay NATS-free). WatchAll
// re-delivers each key's latest value, then a nil sentinel marks "all delivered".
func drainKV(ctx context.Context, kv jetstream.KeyValue) (map[string][]byte, error) {
	w, err := kv.WatchAll(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = w.Stop() }()

	out := make(map[string][]byte)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case entry, ok := <-w.Updates():
			if !ok || entry == nil {
				return out, nil // channel closed or initial sync complete
			}
			switch entry.Operation() {
			case jetstream.KeyValuePut:
				out[entry.Key()] = entry.Value()
			case jetstream.KeyValueDelete, jetstream.KeyValuePurge:
				delete(out, entry.Key())
			}
		}
	}
}
