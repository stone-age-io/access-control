// Package audit consumes the ACC_EVENTS JetStream stream into the PocketBase
// events collection — the durable, queryable projection behind the UI timeline.
// JetStream is the system of record for events; the events collection is a
// rebuildable read model.
package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/metrics"
	"github.com/stone-age-io/access-control/internal/subjects"
)

const durableName = "acc-audit"

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
// history; the durable tracks position across restarts. At-least-once: a
// redelivery after a failed write may produce a duplicate row (acceptable for
// v1 audit).
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
			c.log.Error("audit write failed; will redeliver", "subject", msg.Subject(), "error", err)
			c.m.IncAuditWrite("error")
			_ = msg.Nak()
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
	return c.app.Save(rec)
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
	if ts := str(body["ts"]); ts != "" {
		rec.Set("ts", ts)
	}
	rec.Set("payload", body)
	return rec, true, nil
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
