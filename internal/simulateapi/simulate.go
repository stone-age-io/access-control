// Package simulateapi exposes accessd's access *simulator*: an authenticated
// POST /api/simulate that answers "would this credential open this portal at this
// time?" by running the real, shared policy.Decide over a live snapshot of the
// ACC_POLICY KV — the same data and the same decision function the edge controller
// uses. It is a what-if / commissioning tool: read-only, it publishes nothing and
// changes no state.
//
// Any authenticated operator may call it (like /api/models): a simulation only
// reveals what the policy already grants, and reads are the universal floor. The
// graph assembly + decision live in internal/policysnapshot (pure, tested without
// NATS); this package only reads the KV and shapes the HTTP request/response.
package simulateapi

import (
	"context"
	"net/http"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/policy"
	"github.com/stone-age-io/access-control/internal/policysnapshot"
)

// kvSnapshotTimeout bounds the one-shot KV read so a stalled NATS connection can't
// hang the request indefinitely.
const kvSnapshotTimeout = 10 * time.Second

type handler struct {
	kv  jetstream.KeyValue
	log *logger.Logger
}

// Register wires POST /api/simulate onto the serve event's router.
func Register(se *core.ServeEvent, kv jetstream.KeyValue, log *logger.Logger) {
	h := &handler{kv: kv, log: log.With("component", "simulateapi")}
	se.Router.POST("/api/simulate", h.simulate).Bind(apis.RequireAuth())
}

type request struct {
	Credential string `json:"credential"` // credential value (the thing presented at a reader)
	Portal     string `json:"portal"`     // portal code
	At         string `json:"at"`         // RFC 3339 instant; empty = now
	Posture    string `json:"posture"`    // optional posture override (what-if); empty = resolve normally
}

func (h *handler) simulate(e *core.RequestEvent) error {
	var req request
	if err := e.BindBody(&req); err != nil {
		return e.BadRequestError("invalid request body", err)
	}

	ctx, cancel := context.WithTimeout(e.Request.Context(), kvSnapshotTimeout)
	defer cancel()
	entries, err := snapshotKV(ctx, h.kv)
	if err != nil {
		return e.InternalServerError("failed to read policy", err)
	}

	res, err := evaluate(entries, req, time.Now().UTC())
	if err != nil {
		if bi, ok := err.(badInput); ok {
			return e.BadRequestError(bi.msg, nil)
		}
		return e.InternalServerError("simulation failed", err)
	}
	return e.JSON(http.StatusOK, res)
}

// evaluate is the pure core (no NATS, no HTTP): validate the request, build the
// snapshot, and simulate. `now` is the fallback instant when At is omitted (passed
// in so the function is deterministic in tests).
func evaluate(entries map[string][]byte, req request, now time.Time) (policysnapshot.Result, error) {
	if req.Portal == "" {
		return policysnapshot.Result{}, badInput{"portal is required"}
	}
	if req.Posture != "" && !policy.IsSettablePosture(req.Posture) {
		return policysnapshot.Result{}, badInput{"invalid posture override (want secure/free_access/unlocked/lockdown/disabled)"}
	}
	atUTC := now
	if req.At != "" {
		t, perr := time.Parse(time.RFC3339, req.At)
		if perr != nil {
			return policysnapshot.Result{}, badInput{"invalid 'at' time (want RFC 3339)"}
		}
		atUTC = t.UTC()
	}
	return policysnapshot.Build(entries).Simulate(req.Credential, req.Portal, atUTC, req.Posture), nil
}

// snapshotKV reads the current value of every key in the bucket via a one-shot
// WatchAll drain: WatchAll re-delivers each key's latest value, then a nil sentinel
// marks "all current keys delivered" — exactly the snapshot the controller's
// PolicyStore syncs on boot, without an ongoing watch.
func snapshotKV(ctx context.Context, kv jetstream.KeyValue) (map[string][]byte, error) {
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
			if !ok {
				return out, nil
			}
			if entry == nil {
				return out, nil // initial sync complete
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

// badInput is a sentinel for a 400 (vs. a 500): a client error carrying the message.
type badInput struct{ msg string }

func (b badInput) Error() string { return b.msg }
