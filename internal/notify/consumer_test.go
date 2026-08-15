package notify

import (
	"errors"
	"strings"
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
		sent = append(sent, Format(ev, ""))
		return true, nil
	}
	return New(nil, "ACC_EVENTS", subjects.Default(), send, logger.NewNopLogger(), nil), &sent
}

func TestProcessAlarmSends(t *testing.T) {
	n, sent := newNotifier(t)
	status, err := n.process("acc.hq.door.lobby-main.evt.alarm",
		[]byte(`{"type":"forced","ts":"2026-01-05T14:00:00Z"}`), 0)
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
	status, err := n.process("acc.hq.evt.fire", []byte(`{"active":true,"ts":"2026-01-05T14:00:00Z"}`), 0)
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
		status, err := n.process(subj, []byte(`{"ts":"2026-01-05T14:00:00Z"}`), 0)
		if err != nil || status != "skip" {
			t.Errorf("process(%q) = (%q,%v), want (skip,nil)", subj, status, err)
		}
	}
	if len(*sent) != 0 {
		t.Errorf("sent %d messages, want 0", len(*sent))
	}
}

// A controller going offline is pageable; coming back online is not. Both arrive
// on the same ctrl evt.state subject, so the discrimination is in the body.
func TestProcessControllerLiveness(t *testing.T) {
	for _, tc := range []struct {
		status     string
		wantStatus string
		wantSent   int
	}{
		{"offline", "ok", 1},
		{"online", "skip", 0},
	} {
		n, sent := newNotifier(t)
		status, err := n.process("acc.hq.ctrl.ctrl-hq-1.evt.state",
			[]byte(`{"status":"`+tc.status+`","ts":"2026-01-05T14:00:00Z"}`), 0)
		if err != nil || status != tc.wantStatus {
			t.Errorf("%s: process = (%q,%v), want (%q,nil)", tc.status, status, err, tc.wantStatus)
		}
		if len(*sent) != tc.wantSent {
			t.Errorf("%s: sent %d messages, want %d", tc.status, len(*sent), tc.wantSent)
		}
	}
}

// A portal or area evt.state (posture change, arm/disarm) is a timeline event, not
// a page. The consumer filter pins the ctrl token precisely so these never arrive,
// but NotifyType is the belt to that braces.
func TestProcessSkipsNonCtrlState(t *testing.T) {
	n, sent := newNotifier(t)
	for _, subj := range []string{
		"acc.hq.door.lobby-main.evt.state",
		"acc.hq.area.zone1.evt.state",
	} {
		if status, err := n.process(subj, []byte(`{"arm":"armed","ts":"t"}`), 0); status != "skip" || err != nil {
			t.Errorf("process(%q) = (%q,%v), want (skip,nil)", subj, status, err)
		}
	}
	if len(*sent) != 0 {
		t.Errorf("sent %d messages, want 0", len(*sent))
	}
}

// held_clear is a clear, not a raise: never emailed.
func TestProcessSkipsHeldClear(t *testing.T) {
	n, sent := newNotifier(t)
	status, err := n.process("acc.hq.door.lobby-main.evt.alarm",
		[]byte(`{"type":"held_clear","ts":"2026-01-05T14:00:00Z"}`), 0)
	if status != "skip" || err != nil {
		t.Errorf("process = (%q,%v), want (skip,nil)", status, err)
	}
	if len(*sent) != 0 {
		t.Errorf("sent %d messages, want 0", len(*sent))
	}
}

// NotifyType is the token users.notify_types selects on.
func TestNotifyType(t *testing.T) {
	for _, tc := range []struct {
		name string
		ev   Event
		want string
	}{
		{"forced", Event{Kind: "alarm", AlarmType: "forced"}, TypeForced},
		{"held", Event{Kind: "alarm", AlarmType: "held"}, TypeHeld},
		{"no_entry", Event{Kind: "alarm", AlarmType: "no_entry"}, TypeNoEntry},
		{"intrusion", Event{Kind: "alarm", AlarmType: "intrusion"}, TypeIntrusion},
		{"fire", Event{Kind: "fire"}, TypeFire},
		{"ctrl offline", Event{Kind: "state", Type: "ctrl", AlarmType: "offline"}, TypeControllerOffline},
		{"ctrl online", Event{Kind: "state", Type: "ctrl", AlarmType: "online"}, ""},
		{"held_clear", Event{Kind: "alarm", AlarmType: "held_clear"}, ""},
		{"portal state", Event{Kind: "state", Type: "door"}, ""},
		{"tap", Event{Kind: "tap"}, ""},
	} {
		if got := tc.ev.NotifyType(); got != tc.want {
			t.Errorf("%s: NotifyType() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The deep link identifies the row by JetStream stream sequence — the same value
// the audit projection writes to events.stream_seq under a unique index.
func TestDeepLink(t *testing.T) {
	for _, tc := range []struct {
		url  string
		seq  uint64
		want string
	}{
		{"https://acs.example.com", 42, "https://acs.example.com/alarms?seq=42"},
		{"https://acs.example.com/", 42, "https://acs.example.com/alarms?seq=42"}, // trailing slash
		{"", 42, ""},              // no console URL configured
		{"https://x.test", 0, ""}, // no sequence available
	} {
		if got := DeepLink(tc.url, tc.seq); got != tc.want {
			t.Errorf("DeepLink(%q,%d) = %q, want %q", tc.url, tc.seq, got, tc.want)
		}
	}
}

// A rendered notification carries the link when both halves are available, and is
// unchanged when they are not.
func TestFormatIncludesDeepLink(t *testing.T) {
	ev := Event{Location: "hq", Type: "door", Thing: "lobby-main", Kind: "alarm", AlarmType: "forced", Seq: 7}
	if body := Format(ev, "https://acs.example.com").Body; !strings.Contains(body, "/alarms?seq=7") {
		t.Errorf("body missing deep link:\n%s", body)
	}
	if body := Format(ev, "").Body; strings.Contains(body, "/alarms?seq=") {
		t.Errorf("body has a link with no console URL configured:\n%s", body)
	}
}

func TestProcessSkipsUnrecognizedSubject(t *testing.T) {
	n, _ := newNotifier(t)
	if status, err := n.process("acc.hq.evt", []byte(`{}`), 0); status != "skip" || err != nil {
		t.Errorf("process(short) = (%q,%v), want (skip,nil)", status, err)
	}
}

// A redelivered (subject, ts) is sent once, then deduped.
func TestProcessDedupsRedelivery(t *testing.T) {
	n, sent := newNotifier(t)
	subj := "acc.hq.door.lobby-main.evt.alarm"
	data := []byte(`{"type":"held","ts":"2026-01-05T14:00:00Z"}`)

	if status, _ := n.process(subj, data, 0); status != "ok" {
		t.Fatalf("first process status = %q, want ok", status)
	}
	if status, _ := n.process(subj, data, 0); status != "dedup" {
		t.Fatalf("second process status = %q, want dedup", status)
	}
	if len(*sent) != 1 {
		t.Errorf("sent %d messages, want 1 (dedup should suppress the resend)", len(*sent))
	}

	// A distinct ts on the same subject is a new alarm — not deduped.
	if status, _ := n.process(subj, []byte(`{"type":"held","ts":"2026-01-05T15:00:00Z"}`), 0); status != "ok" {
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

	if _, err := n.process(subj, data, 0); err == nil {
		t.Fatal("first process: want error (Nak), got nil")
	}
	if status, err := n.process(subj, data, 0); status != "ok" || err != nil {
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

	if status, err := n.process(subj, data, 0); status != "skip" || err != nil {
		t.Fatalf("opted-out process = (%q,%v), want (skip,nil)", status, err)
	}
	// Same (subject, ts): because nothing was marked sent, the redelivery must
	// re-evaluate (call send again) rather than dedup.
	wantSent = 2
	if status, err := n.process(subj, data, 0); status != "ok" || err != nil {
		t.Fatalf("opted-in redelivery = (%q,%v), want (ok,nil) — must re-evaluate, not dedup", status, err)
	}
	if calls != 2 {
		t.Errorf("send calls = %d, want 2", calls)
	}
}
