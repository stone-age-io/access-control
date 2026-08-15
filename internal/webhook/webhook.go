// Package webhook is the outbound notification sink: a fourth independent durable
// on ACC_EVENTS that POSTs each pageable event as JSON to an operator-configured URL.
//
// # Why this exists instead of editable email templates
//
// The recurring request behind "let admins edit the notification templates" is
// almost never about wording — it is about getting the event into something that
// already knows how to route, escalate, and acknowledge: PagerDuty, Slack, ntfy, an
// ITSM queue. A template engine would buy a template language, a preview UI, an
// escaping story, a per-field migration path, and a "my template broke and nobody
// got paged" failure mode, and it would still render worse than any of those tools.
// Shipping the structured event instead answers the request permanently, in about a
// page of code, and leaves the email body fixed and terse — which is what it should
// be for something read on a phone at 3am.
//
// # Why a durable, not a hook on the notify sink
//
// It is its own consumer for the same reason notify is not hung off the audit
// projection: independent delivery positions. A webhook receiver that is down must
// not stall email, and a redelivery must not double-send the other. Being a durable
// also means JetStream's Nak IS the retry mechanism, bounded by MaxDeliver — no
// bespoke retry queue, no in-process backoff loop.
//
// Unlike notify, this sink deliberately does NOT consult the per-source opt-in
// flags or users.notify. Those exist to decide who gets *email*; a webhook has a
// single configured destination whose whole purpose is to receive the feed, and
// re-filtering it through per-portal email checkboxes would make it useless for the
// "mirror everything into our NOC" case that motivates it. Configuring the URL is
// the opt-in, and it is gated behind the `operators` capability.
//
// # Outbound requests are an SSRF surface
//
// accessd will POST wherever it is told, from inside the deployment's network. The
// mitigations are deliberate and not optional: redirects are never followed (a
// benign-looking URL must not become a 302 to link-local metadata), the timeout is
// hard, the body is bounded, and the setting is writable only by an operator holding
// `operators`. This is not a full SSRF defence — an install that lets untrusted
// people hold that capability has already lost — but it removes the cheap paths.
package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/metrics"
	"github.com/stone-age-io/access-control/internal/notify"
	"github.com/stone-age-io/access-control/internal/subjects"
)

const (
	durableName = "acc-webhook"
	// maxDeliver bounds redelivery so an endpoint that is down for good cannot loop
	// forever. Higher than notify's: a webhook receiver restarting is more routine
	// than an SMTP outage, and the cost of a retry is one HTTP request.
	maxDeliver = 8
	// ackWait is how long JetStream waits for an ack before redelivering. It must
	// exceed requestTimeout so a slow-but-succeeding POST is never redelivered while
	// still in flight.
	ackWait = 60 * time.Second
	// requestTimeout bounds one POST, including connect and body read.
	requestTimeout = 10 * time.Second
	// maxResponseBytes bounds how much of a response body is read. Nothing is done
	// with it beyond error reporting, so a hostile endpoint must not be able to
	// stream indefinitely into accessd's memory.
	maxResponseBytes = 4 << 10
)

// Payload is the JSON body POSTed for one event. It is the structured event, not a
// rendered message: the receiver renders. Field names are stable — they are a wire
// contract with whatever the install has pointed this at.
type Payload struct {
	// Type is the notify-type token (forced/held/intrusion/fire/controller_offline/
	// no_entry) — the single field most receivers route on.
	Type string `json:"type"`
	// Kind/Location/Thing/ThingType mirror the event subject's parts.
	Kind      string `json:"kind"`
	Location  string `json:"location"`
	Thing     string `json:"thing"`
	ThingType string `json:"thingType"`
	// TS is the event's own timestamp (RFC3339 UTC), not the delivery time.
	TS string `json:"ts"`
	// Seq is the JetStream stream sequence — stable, unique, and the same value
	// events.stream_seq carries, so a receiver can deduplicate redeliveries and link
	// back to the console.
	Seq uint64 `json:"seq"`
	// Link is the console deep link for this event, when a console URL is configured.
	Link string `json:"link,omitempty"`
	// Body is the original event payload, verbatim, for anything the fields above
	// do not carry (an intrusion's tripped point, a controller's last-seen).
	Body map[string]any `json:"body,omitempty"`
}

// Config resolves the destination at delivery time rather than at construction, so
// an operator changing the URL takes effect without restarting accessd. Returning
// ok=false disables delivery — which is what an unconfigured install does, making
// the sink inert by default like every other notification path here.
type Config func() (url string, consoleURL string, ok bool)

// Sender delivers one payload. Abstracted so tests need no HTTP server.
type Sender func(ctx context.Context, url string, body []byte) error

// Sink consumes pageable events and POSTs them to a configured URL.
type Sink struct {
	js     jetstream.JetStream
	stream string
	subj   subjects.Subjects
	cfg    Config
	send   Sender
	log    *logger.Logger
	m      *metrics.Metrics
	cc     jetstream.ConsumeContext
}

// New creates a webhook sink. cfg must be non-nil; a nil send uses the default
// hardened HTTP sender.
func New(js jetstream.JetStream, stream string, subj subjects.Subjects, cfg Config, send Sender, log *logger.Logger, m *metrics.Metrics) *Sink {
	if send == nil {
		send = defaultSender()
	}
	return &Sink{
		js:     js,
		stream: stream,
		subj:   subj,
		cfg:    cfg,
		send:   send,
		log:    log.With("component", "webhook"),
		m:      m,
	}
}

// Start creates (or updates) the durable consumer and begins consuming. Like the
// other reactive sinks it uses DeliverNew: a freshly configured webhook must not
// replay every event since the install was built.
func (s *Sink) Start(ctx context.Context) error {
	w := s.subj.NotifyWildcards()
	cons, err := s.js.CreateOrUpdateConsumer(ctx, s.stream, jetstream.ConsumerConfig{
		Durable:        durableName,
		AckPolicy:      jetstream.AckExplicitPolicy,
		FilterSubjects: w,
		DeliverPolicy:  jetstream.DeliverNewPolicy,
		MaxDeliver:     maxDeliver,
		AckWait:        ackWait,
	})
	if err != nil {
		return fmt.Errorf("create webhook consumer on stream %q: %w", s.stream, err)
	}

	cc, err := cons.Consume(func(msg jetstream.Msg) {
		var seq uint64
		if meta, merr := msg.Metadata(); merr == nil {
			seq = meta.Sequence.Stream
		}
		status, err := s.process(context.Background(), msg.Subject(), msg.Data(), seq)
		if err != nil {
			s.log.Error("webhook post failed; will redeliver", "subject", msg.Subject(), "error", err)
			s.m.IncWebhook("error")
			_ = msg.Nak()
			return
		}
		s.m.IncWebhook(status)
		_ = msg.Ack()
	})
	if err != nil {
		return fmt.Errorf("start webhook consume: %w", err)
	}
	s.cc = cc
	s.log.Info("webhook sink started", "stream", s.stream, "durable", durableName, "filter", w)
	return nil
}

// Stop halts consumption.
func (s *Sink) Stop() {
	if s.cc != nil {
		s.cc.Stop()
	}
}

// process handles one event. It returns a status to ack on ("ok" / "skip"), or an
// error to Nak (redeliver). It takes no jetstream.Msg so tests can drive it directly.
func (s *Sink) process(ctx context.Context, subject string, data []byte, seq uint64) (string, error) {
	url, consoleURL, ok := s.cfg()
	if !ok || url == "" {
		return "skip", nil // no destination configured: inert
	}

	location, ptype, thing, kind, ok := s.subj.ParseEvent(subject)
	if !ok {
		return "skip", nil
	}

	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		body = map[string]any{}
	}

	ev := notify.Event{
		Location:  location,
		Type:      ptype,
		Thing:     thing,
		Kind:      kind,
		AlarmType: str(body["type"]),
		Body:      body,
		TS:        str(body["ts"]),
		Seq:       seq,
	}
	if kind == "state" {
		ev.AlarmType = str(body["status"])
	}
	// Share the sink's classification so email and webhook agree about what counts
	// as an event worth forwarding — including the exclusions (held_clear, a box
	// coming back online).
	notifyType := ev.NotifyType()
	if notifyType == "" {
		return "skip", nil
	}

	payload, err := json.Marshal(Payload{
		Type:      notifyType,
		Kind:      ev.Kind,
		Location:  ev.Location,
		Thing:     ev.Thing,
		ThingType: ev.Type,
		TS:        ev.TS,
		Seq:       ev.Seq,
		Link:      notify.DeepLink(consoleURL, ev.Seq),
		Body:      ev.Body,
	})
	if err != nil {
		// Unencodable payload is not retryable — ack and drop rather than loop.
		s.log.Error("webhook payload encode failed; dropping", "subject", subject, "error", err)
		return "skip", nil
	}

	if err := s.send(ctx, url, payload); err != nil {
		return "", err // Nak → redeliver, bounded by MaxDeliver
	}
	return "ok", nil
}

// defaultSender builds the hardened HTTP sender. Redirects are NEVER followed: a
// destination that looks benign must not be able to bounce the request to a
// link-local metadata endpoint or an internal service.
func defaultSender() Sender {
	client := &http.Client{
		Timeout: requestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return func(ctx context.Context, url string, body []byte) error {
		ctx, cancel := context.WithTimeout(ctx, requestTimeout)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "stone-access")

		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()
		// Drain a bounded amount so the connection can be reused, and so a hostile
		// endpoint cannot stream indefinitely into memory.
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))

		// A 3xx reaches here because redirects are not followed; treat it as a
		// misconfiguration rather than silently succeeding.
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return fmt.Errorf("webhook endpoint returned %s: %s", resp.Status, truncate(string(snippet), 200))
		}
		return nil
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
