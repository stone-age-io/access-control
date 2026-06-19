package subjects

import "testing"

func TestHeartbeatRoundTrip(t *testing.T) {
	s := Default()
	subj := s.Heartbeat("hq", "ctrl-hq-1")
	if subj != "acc.hq.ctrl.ctrl-hq-1.heartbeat" {
		t.Fatalf("Heartbeat = %q", subj)
	}
	loc, code, ok := s.ParseHeartbeat(subj)
	if !ok || loc != "hq" || code != "ctrl-hq-1" {
		t.Errorf("ParseHeartbeat(%q) = (%q,%q,%v), want (hq,ctrl-hq-1,true)", subj, loc, code, ok)
	}
}

func TestHeartbeatWildcard(t *testing.T) {
	if got := Default().HeartbeatWildcard(); got != "acc.*.ctrl.*.heartbeat" {
		t.Errorf("HeartbeatWildcard = %q", got)
	}
}

// A heartbeat must NOT satisfy the events stream's filter — it would otherwise be
// captured as an audit row, the very thing the ctrl namespace avoids.
func TestHeartbeatNotAnEvent(t *testing.T) {
	s := Default()
	subj := s.Heartbeat("hq", "ctrl-hq-1")
	if _, _, _, _, ok := s.ParseEvent(subj); ok {
		t.Errorf("ParseEvent(%q) ok=true, want false (heartbeat must not parse as an event)", subj)
	}
	// And it is a 5-token subject, matching neither {app}.*.evt.fire (4) nor
	// {app}.*.*.*.evt.> (>=6, evt at index 4).
	if _, _, _, _, ok := s.ParseCommand(subj); ok {
		t.Errorf("ParseCommand(%q) ok=true, want false", subj)
	}
}

func TestParseHeartbeatRejects(t *testing.T) {
	s := Default()
	cases := []string{
		"acc.hq.door.lobby-main.evt.tap",    // a portal event
		"acc.hq.door.lobby-main.tap",        // a tap
		"acc.hq.ctrl.ctrl-hq-1.evt.state",   // ctrl-scoped but under evt
		"acc.hq.ctrl.ctrl-hq-1",             // too short
		"other.hq.ctrl.ctrl-hq-1.heartbeat", // wrong app
		"acc.hq.thing.ctrl-hq-1.heartbeat",  // not ctrl-scoped
	}
	for _, subj := range cases {
		if _, _, ok := s.ParseHeartbeat(subj); ok {
			t.Errorf("ParseHeartbeat(%q) ok=true, want false", subj)
		}
	}
}
