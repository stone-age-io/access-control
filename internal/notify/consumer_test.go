package notify

import (
	"errors"
	"testing"

	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/subjects"
)

// newNotifier builds a notifier whose SendFunc records the messages it receives
// (rendering each Event via Format, as accessd's real SendFunc does) and reports
// every event as sent.
func newNotifier(t *testing.T) (*Notifier, *[]Message) {
	t.Helper()
	var sent []Message
	send := func(ev Event) (bool, error) {
		sent = append(sent, Format(ev))
		return true, nil
	}
	return New(nil, "ACC_EVENTS", subjects.Default(), send, logger.NewNopLogger(), nil), &sent
}

func TestProcessAlarmSends(t *testing.T) {
	n, sent := newNotifier(t)
	status, err := n.process("acc.hq.door.lobby-main.evt.alarm",
		[]byte(`{"type":"forced","ts":"2026-01-05T14:00:00Z"}`))
	if err != nil || status != "ok" {
		t.Fatalf("process = (%q,%v), want (ok,nil)", status, err)
	}
	if len(*sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(*sent))
	}
	if msg := (*sent)[0]; msg.Subject == "" || msg.Body == "" {
		t.Errorf("empty message: %+v", msg)
	}
}

func TestProcessFireSends(t *testing.T) {
	n, sent := newNotifier(t)
	status, err := n.process("acc.hq.evt.fire", []byte(`{"active":true,"ts":"2026-01-05T14:00:00Z"}`))
	if err != nil || status != "ok" {
		t.Fatalf("process = (%q,%v), want (ok,nil)", status, err)
	}
	if len(*sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(*sent))
	}
}

func TestProcessSkipsTapAndState(t *testing.T) {
	n, sent := newNotifier(t)
	for _, subj := range []string{
		"acc.hq.door.lobby-main.evt.tap",
		"acc.hq.door.lobby-main.evt.state",
	} {
		status, err := n.process(subj, []byte(`{"ts":"2026-01-05T14:00:00Z"}`))
		if err != nil || status != "skip" {
			t.Errorf("process(%q) = (%q,%v), want (skip,nil)", subj, status, err)
		}
	}
	if len(*sent) != 0 {
		t.Errorf("sent %d messages, want 0", len(*sent))
	}
}

func TestProcessSkipsUnrecognizedSubject(t *testing.T) {
	n, _ := newNotifier(t)
	if status, err := n.process("acc.hq.evt", []byte(`{}`)); status != "skip" || err != nil {
		t.Errorf("process(short) = (%q,%v), want (skip,nil)", status, err)
	}
}

// A redelivered (subject, ts) is sent once, then deduped.
func TestProcessDedupsRedelivery(t *testing.T) {
	n, sent := newNotifier(t)
	subj := "acc.hq.door.lobby-main.evt.alarm"
	data := []byte(`{"type":"held","ts":"2026-01-05T14:00:00Z"}`)

	if status, _ := n.process(subj, data); status != "ok" {
		t.Fatalf("first process status = %q, want ok", status)
	}
	if status, _ := n.process(subj, data); status != "dedup" {
		t.Fatalf("second process status = %q, want dedup", status)
	}
	if len(*sent) != 1 {
		t.Errorf("sent %d messages, want 1 (dedup should suppress the resend)", len(*sent))
	}

	// A distinct ts on the same subject is a new alarm — not deduped.
	if status, _ := n.process(subj, []byte(`{"type":"held","ts":"2026-01-05T15:00:00Z"}`)); status != "ok" {
		t.Errorf("distinct-ts status = %q, want ok", status)
	}
	if len(*sent) != 2 {
		t.Errorf("sent %d messages, want 2", len(*sent))
	}
}

// A send failure surfaces as an error (→ Nak) and is NOT marked sent, so a
// redelivery retries rather than dedups.
func TestProcessSendFailureRetries(t *testing.T) {
	var calls int
	send := func(Event) (bool, error) {
		calls++
		if calls == 1 {
			return false, errors.New("smtp down")
		}
		return true, nil
	}
	n := New(nil, "ACC_EVENTS", subjects.Default(), send, logger.NewNopLogger(), nil)
	subj := "acc.hq.door.lobby-main.evt.alarm"
	data := []byte(`{"type":"forced","ts":"2026-01-05T14:00:00Z"}`)

	if _, err := n.process(subj, data); err == nil {
		t.Fatal("first process: want error (Nak), got nil")
	}
	if status, err := n.process(subj, data); status != "ok" || err != nil {
		t.Fatalf("redelivery = (%q,%v), want (ok,nil) — must retry, not dedup", status, err)
	}
	if calls != 2 {
		t.Errorf("send calls = %d, want 2", calls)
	}
}

// A send that reports sent=false (source or every operator opted out) is acked and
// skipped — NOT marked sent — so a later opt-in is re-evaluated, not deduped away.
func TestProcessNotOptedInSkips(t *testing.T) {
	var calls, wantSent int
	send := func(Event) (bool, error) {
		calls++
		return calls <= wantSent, nil // opt-in flips on once wantSent is bumped
	}
	n := New(nil, "ACC_EVENTS", subjects.Default(), send, logger.NewNopLogger(), nil)
	subj := "acc.hq.door.lobby-main.evt.alarm"
	data := []byte(`{"type":"forced","ts":"2026-01-05T14:00:00Z"}`)

	if status, err := n.process(subj, data); status != "skip" || err != nil {
		t.Fatalf("opted-out process = (%q,%v), want (skip,nil)", status, err)
	}
	// Same (subject, ts): because nothing was marked sent, the redelivery must
	// re-evaluate (call send again) rather than dedup.
	wantSent = 2
	if status, err := n.process(subj, data); status != "ok" || err != nil {
		t.Fatalf("opted-in redelivery = (%q,%v), want (ok,nil) — must re-evaluate, not dedup", status, err)
	}
	if calls != 2 {
		t.Errorf("send calls = %d, want 2", calls)
	}
}
