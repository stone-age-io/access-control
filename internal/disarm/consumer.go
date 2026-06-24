// Package disarm is the entry-disarm sink: a durable consumer on the ACC_EVENTS
// JetStream stream that durably disarms an area when a valid credential grant
// lands at one of its entry (disarm_on_grant) portals.
//
// It is the central half of "a valid badge at an entry door disarms the area."
// Arm-state must be DURABLE and central (a reboot must not silently re-arm, and an
// area can span several controllers), so disarm cannot be a local edge action —
// there is deliberately no cmd.arm on the wire. Instead the edge emits the grant
// it already emits (evt.tap), this sink observes it, and accessd writes the same
// durable areas.arm_override the manual disarm route writes; the mirror then
// propagates it to KV where every peer controller converges.
//
// Like internal/notify (the alarm-email sink), this is its OWN durable on
// ACC_EVENTS with DeliverNew — NOT hung off the audit projection. A projection
// rebuild replays history into the events collection; if disarm logic lived there
// it would re-disarm every historical grant. DeliverNew means it only ever acts on
// new grants. The disarm itself is idempotent (re-disarming a disarmed area is a
// no-op), so a redelivery needs no dedup.
package disarm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/metrics"
	"github.com/stone-age-io/access-control/internal/subjects"
)

const (
	durableName = "acc-disarm"
	// maxDeliver bounds redelivery so a wedged write can't loop forever.
	maxDeliver = 5
	// ackWait is how long JetStream waits for an ack before redelivering.
	ackWait = 30 * time.Second
)

// DisarmFunc durably disarms the area the granted entry portal belongs to. accessd
// supplies one backed by PocketBase; tests supply a fake. It returns whether it
// actually disarmed (false = a no-op: not an entry portal, no area, unknown
// record, or the area is already disarmed / can never be armed). A returned error
// triggers a Nak (redelivery).
type DisarmFunc func(portal, cred string) (disarmed bool, err error)

// tapBody is the subset of the evt.tap payload the sink reads (mirrors
// controller.TapEvent's wire tags).
type tapBody struct {
	Cred  string `json:"cred"`
	Allow bool   `json:"allow"`
}

// Disarmer consumes portal decision events and disarms on authorizing grants.
type Disarmer struct {
	js     jetstream.JetStream
	stream string
	subj   subjects.Subjects
	disarm DisarmFunc
	log    *logger.Logger
	m      *metrics.Metrics
	cc     jetstream.ConsumeContext
}

// New creates a disarmer. disarm must be non-nil.
func New(js jetstream.JetStream, stream string, subj subjects.Subjects, disarm DisarmFunc, log *logger.Logger, m *metrics.Metrics) *Disarmer {
	return &Disarmer{
		js:     js,
		stream: stream,
		subj:   subj,
		disarm: disarm,
		log:    log.With("component", "disarm"),
		m:      m,
	}
}

// Start creates (or updates) the durable consumer and begins consuming. It filters
// to tap events only and delivers only NEW messages (see the package doc).
func (d *Disarmer) Start(ctx context.Context) error {
	filter := d.subj.TapEventWildcard()
	cons, err := d.js.CreateOrUpdateConsumer(ctx, d.stream, jetstream.ConsumerConfig{
		Durable:        durableName,
		AckPolicy:      jetstream.AckExplicitPolicy,
		FilterSubjects: []string{filter},
		DeliverPolicy:  jetstream.DeliverNewPolicy,
		MaxDeliver:     maxDeliver,
		AckWait:        ackWait,
	})
	if err != nil {
		return fmt.Errorf("create disarm consumer on stream %q: %w", d.stream, err)
	}

	cc, err := cons.Consume(func(msg jetstream.Msg) {
		status, err := d.process(msg.Subject(), msg.Data())
		if err != nil {
			d.log.Error("disarm failed; will redeliver", "subject", msg.Subject(), "error", err)
			d.m.IncDisarm("error")
			_ = msg.Nak()
			return
		}
		d.m.IncDisarm(status)
		_ = msg.Ack()
	})
	if err != nil {
		return fmt.Errorf("start disarm consume: %w", err)
	}
	d.cc = cc
	d.log.Info("disarm sink started", "stream", d.stream, "durable", durableName, "filter", filter)
	return nil
}

// Stop halts consumption.
func (d *Disarmer) Stop() {
	if d.cc != nil {
		d.cc.Stop()
	}
}

// process handles one event identified by (subject, body). It returns a status to
// record and ack on ("disarmed" / "skip"), or a non-nil error for a write failure
// to Nak (redeliver). It takes no jetstream.Msg so tests can drive it directly.
//
// Only a real CREDENTIAL grant disarms: a deny is ignored, and an operator remote
// grant (cmd.grant) carries no credential — so a remote door-pop can't silently
// disarm the building.
func (d *Disarmer) process(subject string, data []byte) (string, error) {
	_, _, thing, kind, ok := d.subj.ParseEvent(subject)
	if !ok || kind != "tap" {
		return "skip", nil
	}
	var ev tapBody
	if err := json.Unmarshal(data, &ev); err != nil {
		return "skip", nil // malformed: ack and skip
	}
	if !ev.Allow || ev.Cred == "" {
		return "skip", nil
	}
	disarmed, err := d.disarm(thing, ev.Cred)
	if err != nil {
		return "", err
	}
	if disarmed {
		return "disarmed", nil
	}
	return "skip", nil
}
