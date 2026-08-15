package webhook

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/subjects"
)

// newSink builds a sink with a configured destination and a recording sender.
func newSink(t *testing.T) (*Sink, *[]Payload) {
	t.Helper()
	got := &[]Payload{}
	send := func(_ context.Context, _ string, body []byte) error {
		var p Payload
		if err := json.Unmarshal(body, &p); err != nil {
			t.Errorf("payload is not valid JSON: %v", err)
			return nil
		}
		*got = append(*got, p)
		return nil
	}
	cfg := func() (string, string, bool) { return "https://hook.test/in", "https://acs.test", true }
	return New(nil, "ACC_EVENTS", subjects.Default(), cfg, send, logger.NewNopLogger(), nil), got
}

func TestProcessPostsAlarm(t *testing.T) {
	s, got := newSink(t)
	status, err := s.process(context.Background(), "acc.hq.door.lobby-main.evt.alarm",
		[]byte(`{"type":"forced","ts":"2026-01-05T14:00:00Z"}`), 42)
	if err != nil || status != "ok" {
		t.Fatalf("process = (%q,%v), want (ok,nil)", status, err)
	}
	if len(*got) != 1 {
		t.Fatalf("posts = %d, want 1", len(*got))
	}
	p := (*got)[0]
	if p.Type != "forced" || p.Location != "hq" || p.Thing != "lobby-main" || p.ThingType != "door" {
		t.Errorf("payload routing fields = %+v", p)
	}
	if p.Seq != 42 {
		t.Errorf("seq = %d, want 42", p.Seq)
	}
	if p.Link != "https://acs.test/alarms?seq=42" {
		t.Errorf("link = %q", p.Link)
	}
	if p.Body["type"] != "forced" {
		t.Errorf("body not carried verbatim: %v", p.Body)
	}
}

// A controller going offline is forwarded; coming back online is not — the sink
// shares notify's classification so email and webhook never disagree about what
// counts as an event worth forwarding.
func TestProcessControllerLiveness(t *testing.T) {
	for _, tc := range []struct {
		status   string
		want     string
		wantPost int
	}{
		{"offline", "ok", 1},
		{"online", "skip", 0},
	} {
		s, got := newSink(t)
		status, err := s.process(context.Background(), "acc.hq.ctrl.ctrl-hq-1.evt.state",
			[]byte(`{"status":"`+tc.status+`","ts":"t"}`), 1)
		if err != nil || status != tc.want {
			t.Errorf("%s: process = (%q,%v), want (%q,nil)", tc.status, status, err, tc.want)
		}
		if len(*got) != tc.wantPost {
			t.Errorf("%s: posts = %d, want %d", tc.status, len(*got), tc.wantPost)
		}
	}
}

// Non-pageable events are dropped before any HTTP call.
func TestProcessSkipsNonPageable(t *testing.T) {
	s, got := newSink(t)
	for _, tc := range []struct{ subject, body string }{
		{"acc.hq.door.lobby-main.evt.alarm", `{"type":"held_clear"}`},
		{"acc.hq.door.lobby-main.evt.tap", `{"allow":true}`},
		{"acc.hq.area.zone1.evt.state", `{"arm":"armed"}`},
		{"acc.hq.evt", `{}`}, // unparseable subject
	} {
		if status, err := s.process(context.Background(), tc.subject, []byte(tc.body), 1); status != "skip" || err != nil {
			t.Errorf("process(%q) = (%q,%v), want (skip,nil)", tc.subject, status, err)
		}
	}
	if len(*got) != 0 {
		t.Errorf("posts = %d, want 0", len(*got))
	}
}

// With no URL configured the sink is inert — it never calls the sender at all.
func TestProcessInertWithoutURL(t *testing.T) {
	var calls int
	send := func(context.Context, string, []byte) error { calls++; return nil }
	cfg := func() (string, string, bool) { return "", "", false }
	s := New(nil, "ACC_EVENTS", subjects.Default(), cfg, send, logger.NewNopLogger(), nil)

	if status, err := s.process(context.Background(), "acc.hq.door.lobby-main.evt.alarm",
		[]byte(`{"type":"forced"}`), 1); status != "skip" || err != nil {
		t.Errorf("process = (%q,%v), want (skip,nil)", status, err)
	}
	if calls != 0 {
		t.Errorf("sender called %d times with no URL configured, want 0", calls)
	}
}

// A delivery failure surfaces as an error so JetStream Naks and redelivers — the
// retry mechanism is the durable, not a bespoke queue.
func TestProcessDeliveryFailureRetries(t *testing.T) {
	send := func(context.Context, string, []byte) error { return errors.New("connection refused") }
	cfg := func() (string, string, bool) { return "https://hook.test/in", "", true }
	s := New(nil, "ACC_EVENTS", subjects.Default(), cfg, send, logger.NewNopLogger(), nil)

	if _, err := s.process(context.Background(), "acc.hq.door.lobby-main.evt.alarm",
		[]byte(`{"type":"forced"}`), 1); err == nil {
		t.Error("want error (Nak) on delivery failure, got nil")
	}
}

// The default sender treats a non-2xx as a failure rather than silently succeeding.
func TestDefaultSenderRejectsNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("nope"))
	}))
	defer srv.Close()

	if err := defaultSender()(context.Background(), srv.URL, []byte(`{}`)); err == nil {
		t.Error("want error for a 500 response, got nil")
	}
}

// Redirects are NEVER followed: a benign-looking destination must not be able to
// bounce the request onward to an internal service.
func TestDefaultSenderDoesNotFollowRedirects(t *testing.T) {
	var reached bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirector.Close()

	err := defaultSender()(context.Background(), redirector.URL, []byte(`{}`))
	if reached {
		t.Error("redirect was followed; the sink must never chase a 3xx")
	}
	// The 302 itself is surfaced as a failure, so a misconfigured endpoint is loud.
	if err == nil {
		t.Error("want error for an unfollowed 302, got nil")
	}
}

// A 2xx is a success.
func TestDefaultSenderAcceptsSuccess(t *testing.T) {
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = readAll(r)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	if err := defaultSender()(context.Background(), srv.URL, []byte(`{"type":"forced"}`)); err != nil {
		t.Fatalf("want nil for a 202, got %v", err)
	}
	if string(body) != `{"type":"forced"}` {
		t.Errorf("posted body = %q", string(body))
	}
}

func readAll(r *http.Request) ([]byte, error) {
	defer func() { _ = r.Body.Close() }()
	buf := make([]byte, 0, 512)
	tmp := make([]byte, 512)
	for {
		n, err := r.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			return buf, nil
		}
	}
}
