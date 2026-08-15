// Package notify is the alarm notification sink: a second, independent durable
// consumer on the ACC_EVENTS JetStream stream that emails an operator when an
// alarm or fire event is published.
//
// It is parallel to internal/audit (the events-projection consumer), NOT hung
// off it. The audit consumer is a pure, at-least-once projection; coupling
// alerting there would double-send on a redelivery. So notify is its own durable
// (acc-notify) with its own delivery position.
//
// One deliberate divergence from the audit consumer: notify uses DeliverNew, not
// DeliverAll. Alerting is not a backfillable projection — a freshly created
// durable must start from "now," or enabling notifications on a system that has
// been running for months would email every alarm that ever happened. The
// durable still tracks position across accessd restarts (a brief restart
// redelivers only un-acked messages, not the whole history).
//
// Like internal/disarm, the sink is ALWAYS started and config-free; the "who"
// (recipients) and "which" (per-source opt-in) both live in PocketBase data, not
// config. This package stays PocketBase-free: it parses the event and hands the
// structured Event to a SendFunc the caller (accessd) supplies. That SendFunc owns
// every PocketBase concern — checking the source's opt-in flag, resolving the
// recipient operators, and the SMTP transport — and reports back whether it
// actually sent (sent=false is a no-op, e.g. nobody opted in, NOT an error). So
// the sink is inert until both an alarm source and an operator opt in.
package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/metrics"
	"github.com/stone-age-io/access-control/internal/subjects"
)

const (
	durableName = "acc-notify"
	// maxDeliver bounds redelivery so a dead SMTP server can't loop forever; after
	// this many attempts JetStream stops redelivering and we just log.
	maxDeliver = 5
	// ackWait is how long JetStream waits for an ack before redelivering.
	ackWait = 30 * time.Second
	// dedupTTL must exceed maxDeliver*ackWait so a message's whole redelivery window
	// is covered: it absorbs the at-least-once case where a send succeeds but the
	// ack is lost and the message is redelivered.
	dedupTTL = 5 * time.Minute
)

// Message is the rendered notification (subject + plain-text body) produced by
// Format. It is intentionally transport-free: recipients, From, and the SMTP
// transport are the caller's concern, so this package never imports PocketBase.
type Message struct {
	Subject string
	Body    string
}

// Event is the parsed pageable event the sink hands to a SendFunc. It carries the
// routing discriminators (so the caller can resolve the source's opt-in and the
// recipients) alongside the raw body (so the caller can Format it).
//
//   - portal alarm: Type is the portal kind (door/turnstile/…), Thing the portal
//     code, AlarmType one of forced/held/held_clear/no_entry.
//   - intrusion alarm: Type is the literal "area", Thing the area code, AlarmType
//     "intrusion".
//   - fire: Kind is "fire", Type/Thing/AlarmType empty (the source is the Location).
//   - controller liveness: Kind is "state", Type the literal "ctrl", Thing the
//     controller code, AlarmType the new status (online/offline).
type Event struct {
	Location  string
	Type      string
	Thing     string
	Kind      string // "alarm", "fire", or "state" (ctrl only)
	AlarmType string // forced/held/held_clear/no_entry/intrusion; status for ctrl; empty for fire
	Body      map[string]any
	TS        string
	// Seq is the event's JetStream stream sequence — the same value the audit
	// consumer writes to events.stream_seq (unique-indexed), which is what lets a
	// notification deep-link to the exact row rather than a filtered list.
	Seq uint64
	// Repage is which reminder this is (0 = the original page). Set by
	// internal/repage when re-notifying about an alarm nobody acknowledged, so the
	// rendered message says so rather than looking like a duplicate.
	Repage int
}

// Notify type tokens: the vocabulary an operator selects from in users.notify_types,
// derived from an Event by NotifyType. Stable strings — they are stored in that
// field and rendered in the UI.
const (
	TypeForced            = "forced"
	TypeHeld              = "held"
	TypeNoEntry           = "no_entry"
	TypeIntrusion         = "intrusion"
	TypeFire              = "fire"
	TypeControllerOffline = "controller_offline"
)

// NotifyType maps an Event onto the token an operator's notify_types selection uses.
// Empty means "not a pageable type" — held_clear (a clear, not a raise) and a
// controller coming back ONLINE both land here: neither is something to wake for,
// and the sink drops them before resolving recipients.
func (e Event) NotifyType() string {
	switch {
	case e.Kind == "fire":
		return TypeFire
	case e.Kind == "state" && e.Type == "ctrl":
		if e.AlarmType == "offline" {
			return TypeControllerOffline
		}
		return "" // a box coming back is good news, not a page
	case e.Kind == "alarm":
		switch e.AlarmType {
		case TypeForced, TypeHeld, TypeNoEntry, TypeIntrusion:
			return e.AlarmType
		}
		return "" // held_clear and anything unrecognized
	default:
		return ""
	}
}

// SendFunc delivers one notification. accessd supplies one backed by PocketBase
// (source opt-in lookup + recipient resolution + mailer); tests supply a fake. It
// reports whether it actually sent: sent=false is a benign no-op (the source or
// every operator has opted out) that the sink acks and skips. A returned error
// triggers a Nak (redelivery) — use it only for genuine failures (SMTP down, a DB
// read error), never for "nobody wanted this".
type SendFunc func(Event) (sent bool, err error)

// Notifier consumes alarm/fire events and emails them.
type Notifier struct {
	js     jetstream.JetStream
	stream string
	subj   subjects.Subjects
	send   SendFunc
	log    *logger.Logger
	m      *metrics.Metrics
	cc     jetstream.ConsumeContext

	mu   sync.Mutex
	seen map[string]time.Time // dedup key -> first-sent instant
}

// New creates a notifier. send must be non-nil.
func New(js jetstream.JetStream, stream string, subj subjects.Subjects, send SendFunc, log *logger.Logger, m *metrics.Metrics) *Notifier {
	return &Notifier{
		js:     js,
		stream: stream,
		subj:   subj,
		send:   send,
		log:    log.With("component", "notify"),
		m:      m,
		seen:   make(map[string]time.Time),
	}
}

// Start creates (or updates) the durable consumer and begins consuming. It
// filters to alarm/fire subjects only — taps/state never reach handle — and
// delivers only NEW messages (see the package doc).
func (n *Notifier) Start(ctx context.Context) error {
	w := n.subj.NotifyWildcards()
	cons, err := n.js.CreateOrUpdateConsumer(ctx, n.stream, jetstream.ConsumerConfig{
		Durable:        durableName,
		AckPolicy:      jetstream.AckExplicitPolicy,
		FilterSubjects: w,
		DeliverPolicy:  jetstream.DeliverNewPolicy,
		MaxDeliver:     maxDeliver,
		AckWait:        ackWait,
	})
	if err != nil {
		return fmt.Errorf("create notify consumer on stream %q: %w", n.stream, err)
	}

	cc, err := cons.Consume(func(msg jetstream.Msg) {
		// The stream sequence is what a notification deep-links on; it is not in the
		// body, so it is read here and threaded into process (which stays free of
		// jetstream.Msg so tests can drive it directly). An unavailable sequence
		// degrades to no link, never to a dropped notification.
		var seq uint64
		if meta, merr := msg.Metadata(); merr == nil {
			seq = meta.Sequence.Stream
		}
		status, err := n.process(msg.Subject(), msg.Data(), seq)
		if err != nil {
			n.log.Error("notify send failed; will redeliver", "subject", msg.Subject(), "error", err)
			n.m.IncNotify("error")
			_ = msg.Nak()
			return
		}
		n.m.IncNotify(status)
		_ = msg.Ack()
	})
	if err != nil {
		return fmt.Errorf("start notify consume: %w", err)
	}
	n.cc = cc
	n.log.Info("notify consumer started", "stream", n.stream, "durable", durableName, "filter", w)
	return nil
}

// Stop halts consumption.
func (n *Notifier) Stop() {
	if n.cc != nil {
		n.cc.Stop()
	}
}

// process handles one event identified by (subject, body). It returns a status to
// record and ack on ("ok" sent / "skip" not-an-alarm or nobody-opted-in / "dedup"
// already-sent), or a non-nil error for a send failure that should be Nak'd
// (redelivered). It takes no jetstream.Msg so tests can drive it directly.
func (n *Notifier) process(subject string, data []byte, seq uint64) (string, error) {
	location, ptype, thing, kind, ok := n.subj.ParseEvent(subject)
	if !ok {
		return "skip", nil // unrecognized subject: ack and skip
	}
	if kind != "alarm" && kind != "fire" && kind != "state" {
		return "skip", nil // ordinary taps are not alerted on
	}

	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		body = map[string]any{}
	}
	ts := str(body["ts"])

	ev := Event{
		Location:  location,
		Type:      ptype,
		Thing:     thing,
		Kind:      kind,
		AlarmType: str(body["type"]),
		Body:      body,
		TS:        ts,
		Seq:       seq,
	}
	// A controller liveness event carries its new state in "status", not "type".
	if kind == "state" {
		ev.AlarmType = str(body["status"])
	}
	// Drop non-pageable events (a held_clear, a box coming back online, a portal or
	// area posture/arm change) before touching the database. The consumer filter is
	// deliberately coarse — it pins subjects, not body discriminators — so this is
	// where the fine-grained decision lives.
	if ev.NotifyType() == "" {
		return "skip", nil
	}

	if n.alreadySent(subject, ts) {
		return "dedup", nil // a redelivery we already emailed about
	}

	sent, err := n.send(ev)
	if err != nil {
		return "", err
	}
	if !sent {
		return "skip", nil // source or every operator opted out — nothing emailed
	}
	n.markSent(subject, ts)
	return "ok", nil
}

// dedupKey identifies a logical event for the dedup window. ts is included so two
// genuinely distinct alarms on the same subject are not collapsed.
func dedupKey(subject, ts string) string { return subject + "\x00" + ts }

// alreadySent reports whether this (subject, ts) was emailed within the dedup TTL.
func (n *Notifier) alreadySent(subject, ts string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	at, ok := n.seen[dedupKey(subject, ts)]
	return ok && time.Since(at) < dedupTTL
}

// markSent records a successful send and opportunistically prunes stale entries
// (alarm volume is low, so a full scan is cheap and avoids a separate timer).
func (n *Notifier) markSent(subject, ts string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	now := time.Now()
	for k, at := range n.seen {
		if now.Sub(at) >= dedupTTL {
			delete(n.seen, k)
		}
	}
	n.seen[dedupKey(subject, ts)] = now
}

// Format renders an event into an operator-readable notification. Plain text:
// notifications go to phones/pagers, so keep it terse and greppable. The caller's
// SendFunc uses this to produce the message body once it has decided to send.
//
// consoleURL is the install's base URL (PocketBase's Settings().Meta.AppURL). When
// set, and when the event has a stream sequence, the message ends with a link
// straight to that row in the alarm console — the difference between "a forced alarm
// happened somewhere" and one tap to acknowledge it. Empty consoleURL (or a missing
// sequence) simply omits the link.
func Format(ev Event, consoleURL string) Message {
	var msg Message
	switch {
	case ev.Kind == "fire":
		msg = Message{
			Subject: fmt.Sprintf("[stone-access] FIRE input active at %s", ev.Location),
			Body:    fmt.Sprintf("Fire-alarm input active at location %q.\nts: %s\n", ev.Location, ev.TS),
		}
	case ev.Kind == "state" && ev.Type == "ctrl":
		msg = Message{
			Subject: fmt.Sprintf("[stone-access] controller %s: %s (%s)", ev.AlarmType, ev.Thing, ev.Location),
			Body: fmt.Sprintf("Controller %q at location %q is %s.\n"+
				"Doors it drives now decide on cached policy, or default-deny if it has none.\n"+
				"last seen: %s\nts: %s\n",
				ev.Thing, ev.Location, ev.AlarmType, str(ev.Body["lastSeen"]), ev.TS),
		}
	default: // alarm
		body := fmt.Sprintf("Alarm type: %s\nlocation: %s\ntype: %s\nthing: %s\n",
			ev.AlarmType, ev.Location, ev.Type, ev.Thing)
		if point := str(ev.Body["point"]); point != "" {
			body += fmt.Sprintf("point: %s\n", point) // intrusion alarms name the tripped aux input
		}
		body += fmt.Sprintf("ts: %s\n", ev.TS)
		msg = Message{
			Subject: fmt.Sprintf("[stone-access] %s alarm: %s/%s/%s", ev.AlarmType, ev.Location, ev.Type, ev.Thing),
			Body:    body,
		}
	}
	if link := DeepLink(consoleURL, ev.Seq); link != "" {
		msg.Body += "\n" + link + "\n"
	}
	// A reminder must be distinguishable from the original page in a crowded inbox,
	// and must say why it arrived: nobody has acknowledged it.
	if ev.Repage > 0 {
		msg.Subject = fmt.Sprintf("[reminder %d] %s", ev.Repage, msg.Subject)
		msg.Body = fmt.Sprintf("STILL UNACKNOWLEDGED (reminder %d).\n\n%s", ev.Repage, msg.Body)
	}
	return msg
}

// DeepLink builds the console URL for one event, keyed by its JetStream stream
// sequence. The audit projection writes that sequence to events.stream_seq under a
// unique index, so it identifies the row exactly — and it is known here, whereas the
// row's PocketBase id is not (the audit consumer is an independent durable, and may
// not even have projected the row yet when this email is sent).
//
// Empty when there is no console URL configured or no sequence available.
func DeepLink(consoleURL string, seq uint64) string {
	if consoleURL == "" || seq == 0 {
		return ""
	}
	return fmt.Sprintf("%s/alarms?seq=%d", strings.TrimRight(consoleURL, "/"), seq)
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
