// Package audit consumes the ACC_EVENTS JetStream stream into the PocketBase
// events collection — the durable, queryable projection behind the UI timeline.
// JetStream is the system of record for events; the events collection is a
// rebuildable read model.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/metrics"
	"github.com/stone-age-io/access-control/internal/subjects"
)

const durableName = "acc-audit"

// nakBackoff is the delay before each redelivery, indexed by the attempt that just
// failed; maxDeliveries is where the consumer stops asking. Roughly five minutes of
// patience in total — long enough to ride out a database that is briefly busy, short
// enough that a genuinely broken message is not a permanent log source. len() of the
// table IS the cap, so the two can never disagree.
var nakBackoff = []time.Duration{
	time.Second, 2 * time.Second, 5 * time.Second, 15 * time.Second,
	30 * time.Second, time.Minute, 2 * time.Minute, 2 * time.Minute,
}

var maxDeliveries = uint64(len(nakBackoff)) + 1

// Consumer is a durable JetStream consumer that writes events to PocketBase.
type Consumer struct {
	app    core.App
	js     jetstream.JetStream
	stream string
	subj   subjects.Subjects
	log    *logger.Logger
	m      *metrics.Metrics
	cc     jetstream.ConsumeContext
}

// New creates an audit consumer. app writes the rows; js/stream supply events.
func New(app core.App, js jetstream.JetStream, stream string, subj subjects.Subjects, log *logger.Logger, m *metrics.Metrics) *Consumer {
	return &Consumer{app: app, js: js, stream: stream, subj: subj, log: log.With("component", "audit"), m: m}
}

// Start creates (or updates) the durable consumer and begins consuming. It
// delivers from the start of the stream so the events table reflects the full
// history; the durable tracks position across restarts. At-least-once, made
// idempotent: each row carries the message's JetStream stream sequence
// (stream_seq, unique-indexed), and a redelivery whose row already landed is
// acked and skipped instead of duplicating it.
func (c *Consumer) Start(ctx context.Context) error {
	cons, err := c.js.CreateOrUpdateConsumer(ctx, c.stream, jetstream.ConsumerConfig{
		Durable:        durableName,
		AckPolicy:      jetstream.AckExplicitPolicy,
		FilterSubjects: c.subj.EventsWildcards(),
		DeliverPolicy:  jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return fmt.Errorf("create audit consumer on stream %q: %w", c.stream, err)
	}

	cc, err := cons.Consume(func(msg jetstream.Msg) {
		if err := c.handle(msg); err != nil {
			c.retry(msg, err)
			return
		}
		c.m.IncAuditWrite("ok")
		_ = msg.Ack()
	})
	if err != nil {
		return fmt.Errorf("start audit consume: %w", err)
	}
	c.cc = cc
	c.log.Info("audit consumer started", "stream", c.stream, "durable", durableName)
	return nil
}

// retry backs off, and eventually gives up, on a message that will not project.
//
// It used to be a bare Nak, which asks for immediate redelivery, with no delivery
// cap on the consumer. That is correct for a transient failure (a locked database
// mid-migration) and pathological for a permanent one: a single record PocketBase
// will never accept — a select value out of range, say — is retried as fast as the
// process can log, forever, and the operator sees a log file filling with one
// event. Which is exactly what happened.
//
// Rather than classify errors (the taxonomy is PocketBase's and it moves), treat
// every failure the same way and let the delivery count decide: back off so a
// transient outage still resolves itself, then Term so a poison message stops
// rather than outliving the deployment. Terminating loses the projection ROW, not
// the event — JetStream is the system of record and `events` is rebuildable, so
// the cost of giving up is one missing row until a rebuild, against an unbounded
// loop for the alternative.
func (c *Consumer) retry(msg jetstream.Msg, cause error) {
	c.m.IncAuditWrite("error")

	attempt := uint64(1)
	if meta, err := msg.Metadata(); err == nil && meta.NumDelivered > 0 {
		attempt = meta.NumDelivered
	}

	if attempt >= maxDeliveries {
		c.log.Error("audit write failed; giving up on this message",
			"subject", msg.Subject(), "error", cause, "attempts", attempt,
			"note", "the events row is missing until the projection is rebuilt")
		_ = msg.Term()
		return
	}

	delay := nakBackoff[int(attempt)-1]
	c.log.Error("audit write failed; will redeliver",
		"subject", msg.Subject(), "error", cause, "attempt", attempt, "retryIn", delay)
	_ = msg.NakWithDelay(delay)
}

// Stop halts consumption.
func (c *Consumer) Stop() {
	if c.cc != nil {
		c.cc.Stop()
	}
}

func (c *Consumer) handle(msg jetstream.Msg) error {
	rec, ok, err := c.recordFrom(msg.Subject(), msg.Data())
	if err != nil {
		return err
	}
	if !ok {
		c.log.Warn("audit: unrecognized subject, acking", "subject", msg.Subject())
		return nil // ack and skip; not retryable
	}
	if meta, err := msg.Metadata(); err == nil {
		if c.alreadyProjected(meta.Sequence.Stream) {
			c.log.Debug("audit: already projected, acking", "subject", msg.Subject(), "seq", meta.Sequence.Stream)
			return nil
		}
		rec.Set("stream_seq", meta.Sequence.Stream)
	}
	return c.app.Save(rec)
}

// alreadyProjected reports whether an events row for this stream sequence
// already exists — a redelivery of a message whose write landed but whose ack
// didn't. A lookup failure reads as "not projected": the subsequent Save either
// succeeds or trips the unique index and redelivers, so the check erring on the
// side of writing never loses an event.
func (c *Consumer) alreadyProjected(seq uint64) bool {
	_, err := c.app.FindFirstRecordByFilter("events", "stream_seq = {:seq}", dbx.Params{"seq": seq})
	return err == nil
}

// recordFrom builds (but does not save) an events record from an event subject
// and body. ok is false for an unrecognized subject. Split out for testing.
func (c *Consumer) recordFrom(subject string, data []byte) (*core.Record, bool, error) {
	location, ptype, portal, kind, ok := c.subj.ParseEvent(subject)
	if !ok {
		return nil, false, nil
	}

	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		body = map[string]any{"raw": string(data)}
	}

	col, err := c.app.FindCollectionByNameOrId("events")
	if err != nil {
		return nil, false, err
	}
	rec := core.NewRecord(col)
	rec.Set("location", location)
	rec.Set("portal", portal)
	rec.Set("type", ptype)
	rec.Set("kind", kind)
	rec.Set("credential", str(body["cred"]))
	rec.Set("user", str(body["user"]))
	if allow, ok := body["allow"].(bool); ok {
		rec.Set("allow", allow)
	}
	rec.Set("reason", str(body["reason"]))
	c.setSource(rec, col, str(body["source"]))
	if ts := str(body["ts"]); ts != "" {
		rec.Set("ts", ts)
	}
	rec.Set("payload", body)
	return rec, true, nil
}

// setSource writes body["source"] onto the row only if events.source actually
// accepts it, asking the collection rather than carrying a second copy of the
// list. Anything else is dropped from the column and left in `payload`, where it
// is still queryable and still forensically present.
//
// This exists because the alternative is not a wrong value in a column, it is an
// error loop. events.source is a select; PocketBase rejects an out-of-range value
// for the whole record, so ONE event body carrying a word from another vocabulary
// takes down the projection of that message permanently — and a durable consumer
// retries permanent failures as eagerly as transient ones. It has happened twice
// already from opposite directions: an emitter added values the schema did not
// have (command/badge, now migrated in), and an unrelated feature reused the key
// for a different question (the arm shadow's standing/scheduled/override, now
// `armSource`). The schema is the authority on what this column may hold, so ask
// it, and let a projection be slightly lossy rather than stuck.
func (c *Consumer) setSource(rec *core.Record, col *core.Collection, source string) {
	if source == "" {
		return
	}
	f, ok := col.Fields.GetByName("source").(*core.SelectField)
	if !ok {
		return // retyped or removed; payload still carries the value
	}
	if slices.Contains(f.Values, source) {
		rec.Set("source", source)
		return
	}
	c.log.Warn("audit: unknown event source, keeping it in payload only",
		"source", source, "accepted", f.Values)
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
