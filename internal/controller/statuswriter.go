package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/statuskv"
)

// statusOpTimeout bounds a single KV put/delete against the status bucket.
const statusOpTimeout = 5 * time.Second

// StatusWriter publishes the controller's live "device shadow" into the
// ACC_STATUS KV bucket — the upward counterpart to PolicyStore's downward watch.
// It is the single writer of this controller's status keys.
//
// Writes are coalesced off the hot path: the tap/input loop only updates an
// in-memory desired map and signals; a drain goroutine reconciles it to KV
// (latest-wins per key — exactly KV's model). A failed put leaves the key
// divergent, so it is retried on the next signal; a NATS reconnect calls Resync
// to re-publish the whole shadow, the same self-heal PolicyStore performs. KV
// puts are never done inline on the run loop (they would block on JetStream).
type StatusWriter struct {
	kv     jetstream.KeyValue // bound handle; may start nil and bind lazily
	bindKV KVBinder           // binds kv on demand (nil once kv is set eagerly)
	code   string             // this controller's code (stamped into every PortalStatus)
	log    *logger.Logger

	mu      sync.Mutex
	current map[string][]byte // desired latest value per key ("" key absent = should be deleted)
	written map[string][]byte // last value confirmed written to KV
	dirty   chan struct{}

	cancel context.CancelFunc
	done   chan struct{}
}

// NewStatusWriter creates a writer bound to an already-open read-write ACC_STATUS
// handle.
func NewStatusWriter(kv jetstream.KeyValue, controllerCode string, log *logger.Logger) *StatusWriter {
	return newStatusWriter(kv, nil, controllerCode, log)
}

// NewStatusWriterLazy creates a writer that binds its ACC_STATUS handle on demand
// via bindKV, retried inside the drain loop. It lets the controller start before
// NATS is reachable: writes queue in memory and are flushed once the bind succeeds
// (the upward shadow is useless while offline anyway — nothing is watching it).
func NewStatusWriterLazy(bindKV KVBinder, controllerCode string, log *logger.Logger) *StatusWriter {
	return newStatusWriter(nil, bindKV, controllerCode, log)
}

func newStatusWriter(kv jetstream.KeyValue, bindKV KVBinder, controllerCode string, log *logger.Logger) *StatusWriter {
	return &StatusWriter{
		kv:      kv,
		bindKV:  bindKV,
		code:    controllerCode,
		log:     log.With("component", "statuswriter"),
		current: make(map[string][]byte),
		written: make(map[string][]byte),
		dirty:   make(chan struct{}, 1),
	}
}

// Start launches the drain goroutine. Stop ends it.
func (w *StatusWriter) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	w.cancel = cancel
	w.done = make(chan struct{})
	go func() {
		defer close(w.done)
		for {
			select {
			case <-ctx.Done():
				return
			case <-w.dirty:
				w.drain(ctx)
			}
		}
	}()
	w.log.Info("status writer started")
}

// Stop halts the drain goroutine and waits for it to exit.
func (w *StatusWriter) Stop() {
	if w.cancel != nil {
		w.cancel()
	}
	if w.done != nil {
		<-w.done
	}
}

// SetPortal records a portal's current shadow. The runtime supplies the live
// fields (including the posture's provenance source); the writer stamps the
// controller code and timestamp. Cheap and non-blocking — the actual KV put
// happens on the drain goroutine.
func (w *StatusWriter) SetPortal(code, location, door, posture, source string, held bool, at time.Time) {
	b, err := json.Marshal(statuskv.PortalStatus{
		Code:       code,
		Location:   location,
		Controller: w.code,
		Door:       door,
		Posture:    posture,
		Source:     source,
		Held:       held,
		UpdatedAt:  at.UTC().Format(time.RFC3339),
	})
	if err != nil {
		w.log.Error("marshal portal status", "portal", code, "error", err)
		return
	}
	w.set(statuskv.PrefixPortal+code, b)
}

// DeletePortal removes a portal's shadow key (called when a portal is disarmed).
func (w *StatusWriter) DeletePortal(code string) {
	w.del(statuskv.PrefixPortal + code)
}

// SetAuxInput records an auxiliary input's current shadow.
func (w *StatusWriter) SetAuxInput(code, location string, active bool, at time.Time) {
	b, err := json.Marshal(statuskv.AuxInputStatus{
		Code:       code,
		Location:   location,
		Controller: w.code,
		Active:     active,
		UpdatedAt:  at.UTC().Format(time.RFC3339),
	})
	if err != nil {
		w.log.Error("marshal aux input status", "code", code, "error", err)
		return
	}
	w.set(statuskv.PrefixAuxIn+code, b)
}

// DeleteAuxInput removes an aux input's shadow key (on disarm).
func (w *StatusWriter) DeleteAuxInput(code string) {
	w.del(statuskv.PrefixAuxIn + code)
}

// SetAuxOutput records an auxiliary output's current shadow.
func (w *StatusWriter) SetAuxOutput(code, location string, energized bool, at time.Time) {
	b, err := json.Marshal(statuskv.AuxOutputStatus{
		Code:       code,
		Location:   location,
		Controller: w.code,
		Energized:  energized,
		UpdatedAt:  at.UTC().Format(time.RFC3339),
	})
	if err != nil {
		w.log.Error("marshal aux output status", "code", code, "error", err)
		return
	}
	w.set(statuskv.PrefixAuxOut+code, b)
}

// DeleteAuxOutput removes an aux output's shadow key (on disarm).
func (w *StatusWriter) DeleteAuxOutput(code string) {
	w.del(statuskv.PrefixAuxOut + code)
}

// areaKey is this controller's shadow key for an area: area.<controller>.<code>.
// Per-controller, so each participant's view of the same area is its own row.
func (w *StatusWriter) areaKey(areaCode string) string {
	return statuskv.PrefixArea + w.code + "." + areaCode
}

// SetArea records this controller's view of an area's effective arm-state. peers
// is the full participant set (every controller with a member input), stamped so
// the console can aggregate "all armed" vs "a participant never reported."
func (w *StatusWriter) SetArea(areaCode, location, arm, source string, peers []string, at time.Time) {
	b, err := json.Marshal(statuskv.AreaStatus{
		Code:       areaCode,
		Location:   location,
		Controller: w.code,
		Arm:        arm,
		Source:     source,
		Peers:      peers,
		UpdatedAt:  at.UTC().Format(time.RFC3339),
	})
	if err != nil {
		w.log.Error("marshal area status", "code", areaCode, "error", err)
		return
	}
	w.set(w.areaKey(areaCode), b)
}

// DeleteArea removes this controller's arm shadow for an area (when the area no
// longer has a member input here, or is removed).
func (w *StatusWriter) DeleteArea(areaCode string) {
	w.del(w.areaKey(areaCode))
}

// Resync re-publishes the entire current shadow on the next drain. Wire it to the
// NATS reconnect handler: a reconnect can drop in-flight writes, so we force every
// key to be re-put (and accessd's own watcher re-syncs to receive them).
func (w *StatusWriter) Resync() {
	w.mu.Lock()
	w.written = make(map[string][]byte) // forget what we think is written → reconcile all
	w.mu.Unlock()
	w.signal()
}

// set records a desired value and signals the drain. The drain dedups against
// what was actually written, so a redundant set just yields a no-op drain.
func (w *StatusWriter) set(key string, val []byte) {
	w.mu.Lock()
	w.current[key] = val
	w.mu.Unlock()
	w.signal()
}

// del marks a key for removal and signals the drain.
func (w *StatusWriter) del(key string) {
	w.mu.Lock()
	delete(w.current, key)
	w.mu.Unlock()
	w.signal()
}

func (w *StatusWriter) signal() {
	select {
	case w.dirty <- struct{}{}:
	default:
	}
}

// drain reconciles current → KV: put every key whose desired value differs from
// what was last written, and delete every key that was written but is no longer
// desired. Successful operations advance `written`; failures leave the key
// divergent so the next signal retries it.
func (w *StatusWriter) drain(ctx context.Context) {
	if !w.ensureKV(ctx) {
		return // bucket not bound yet (NATS unreachable); retry on the next signal
	}
	w.mu.Lock()
	puts := make(map[string][]byte)
	for k, v := range w.current {
		if !bytes.Equal(w.written[k], v) {
			puts[k] = v
		}
	}
	var dels []string
	for k := range w.written {
		if _, ok := w.current[k]; !ok {
			dels = append(dels, k)
		}
	}
	w.mu.Unlock()

	okPuts := make(map[string][]byte, len(puts))
	for k, v := range puts {
		if err := w.put(ctx, k, v); err != nil {
			w.log.Error("status put failed; will retry", "key", k, "error", err)
			continue
		}
		okPuts[k] = v
	}
	okDels := make([]string, 0, len(dels))
	for _, k := range dels {
		if err := w.delete(ctx, k); err != nil {
			w.log.Error("status delete failed; will retry", "key", k, "error", err)
			continue
		}
		okDels = append(okDels, k)
	}

	w.mu.Lock()
	for k, v := range okPuts {
		w.written[k] = v
	}
	for _, k := range okDels {
		delete(w.written, k)
	}
	w.mu.Unlock()
}

// ensureKV binds the ACC_STATUS handle on first need (for the lazy constructor),
// retried on each drain. Runs only on the drain goroutine, so w.kv needs no lock.
func (w *StatusWriter) ensureKV(ctx context.Context) bool {
	if w.kv != nil {
		return true
	}
	if w.bindKV == nil {
		return false // eager constructor with a nil handle: nothing to bind
	}
	bctx, cancel := context.WithTimeout(ctx, statusOpTimeout)
	defer cancel()
	kv, err := w.bindKV(bctx)
	if err != nil {
		w.log.Warn("status KV bucket not bound yet (NATS may be unreachable); will retry", "error", err)
		return false
	}
	w.kv = kv
	w.log.Info("status KV bucket bound")
	return true
}

func (w *StatusWriter) put(ctx context.Context, key string, val []byte) error {
	opCtx, cancel := context.WithTimeout(ctx, statusOpTimeout)
	defer cancel()
	_, err := w.kv.Put(opCtx, key, val)
	return err
}

func (w *StatusWriter) delete(ctx context.Context, key string) error {
	opCtx, cancel := context.WithTimeout(ctx, statusOpTimeout)
	defer cancel()
	return w.kv.Delete(opCtx, key)
}
