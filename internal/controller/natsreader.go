package controller

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stone-age-io/access-control/internal/drivers"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/metrics"
	"github.com/stone-age-io/access-control/internal/subjects"
)

// Portal is a controller's view of one portal it drives: its code (the {thing}
// subject segment), its type (the {type} segment), and its location (the
// {location} segment). The controller resolves these from the PolicyStore, so it
// can build exact {app}.{location}.{type}.{thing}.tap subjects. Location comes
// from the portal record (binding is central), not the controller's own config.
type Portal struct {
	Code     string
	Type     string
	Location string
}

// NATSReader is the v1 "reader": instead of OSDP/RS485 hardware, it turns NATS
// messages into taps so presentations can be simulated with `nats pub`. It
// subscribes per armed portal to {app}.{location}.{type}.{thing}.tap; the
// message body is {"cred":"..."} (or a bare credential string).
//
// Subscriptions are managed dynamically: the portal reconciler calls Arm/Disarm
// as portals appear in, change in, or disappear from policy, so the controller
// tracks the system of record without a restart. It starts with no
// subscriptions and a single shared taps channel feeding the runtime.
type NATSReader struct {
	nc       *nats.Conn
	location string
	subj     subjects.Subjects
	log      *logger.Logger
	m        *metrics.Metrics
	ch       chan drivers.Tap

	mu     sync.Mutex
	subs   map[string]*nats.Subscription // portal code -> subscription
	closed bool
}

// NewNATSReader creates a reader bound to the controller's home location with no
// portals armed yet. The metrics argument may be nil.
func NewNATSReader(nc *nats.Conn, location string, subj subjects.Subjects, log *logger.Logger, m *metrics.Metrics) *NATSReader {
	return &NATSReader{
		nc:       nc,
		location: location,
		subj:     subj,
		log:      log.With("component", "nats-reader"),
		m:        m,
		ch:       make(chan drivers.Tap, 64),
		subs:     make(map[string]*nats.Subscription),
	}
}

// Arm subscribes to the tap subject for one portal. It is idempotent: arming a
// portal already armed is a no-op (the reconciler disarms first when the type
// changes, so the subject is always current).
func (r *NATSReader) Arm(p Portal) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return fmt.Errorf("reader closed")
	}
	if _, ok := r.subs[p.Code]; ok {
		return nil
	}

	location := p.Location
	if location == "" {
		location = r.location // fall back to the controller's home location
	}
	subject := r.subj.Tap(location, p.Type, p.Code)
	code := p.Code // capture
	sub, err := r.nc.Subscribe(subject, func(msg *nats.Msg) {
		tap := drivers.Tap{Portal: code, Credential: parseCred(msg.Data), At: time.Now().UTC()}
		// Non-blocking: never wedge the NATS delivery goroutine if the runtime
		// stalls. A full queue means we're already saturated; drop and count it
		// rather than block (and fail safe — a dropped tap is a denied entry).
		select {
		case r.ch <- tap:
		default:
			r.m.IncTapDropped()
			r.log.Warn("tap queue full; dropping tap", "portal", code)
		}
	})
	if err != nil {
		return fmt.Errorf("subscribe %q: %w", subject, err)
	}
	r.subs[code] = sub
	r.log.Info("reader armed", "subject", subject, "portal", code)
	return nil
}

// Disarm unsubscribes from a portal's tap subject. Unknown portals are a no-op.
func (r *NATSReader) Disarm(code string) {
	r.mu.Lock()
	sub := r.subs[code]
	delete(r.subs, code)
	r.mu.Unlock()
	if sub == nil {
		return
	}
	if err := sub.Unsubscribe(); err != nil {
		r.log.Warn("reader disarm: unsubscribe failed", "portal", code, "error", err)
	}
	r.log.Info("reader disarmed", "portal", code)
}

// Taps implements drivers.ReaderDriver.
func (r *NATSReader) Taps() <-chan drivers.Tap { return r.ch }

// Stop unsubscribes from every portal and closes the taps channel.
func (r *NATSReader) Stop() {
	r.mu.Lock()
	for code, s := range r.subs {
		_ = s.Unsubscribe()
		delete(r.subs, code)
	}
	r.closed = true
	r.mu.Unlock()
	close(r.ch)
}

// parseCred accepts {"cred":"..."} JSON or a bare credential string.
func parseCred(data []byte) string {
	var body struct {
		Cred string `json:"cred"`
	}
	if err := json.Unmarshal(data, &body); err == nil && body.Cred != "" {
		return body.Cred
	}
	return string(data)
}
