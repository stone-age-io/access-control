package subjects

import "testing"

func TestHeartbeatRoundTrip(t *testing.T) {
	s := Default()
	subj := s.Heartbeat("hq", "ctrl-hq-1")
	if subj != "acc.hq.heartbeat.ctrl.ctrl-hq-1" {
		t.Fatalf("Heartbeat = %q", subj)
	}
	loc, code, ok := s.ParseHeartbeat(subj)
	if !ok || loc != "hq" || code != "ctrl-hq-1" {
		t.Errorf("ParseHeartbeat(%q) = (%q,%q,%v), want (hq,ctrl-hq-1,true)", subj, loc, code, ok)
	}
}

func TestHeartbeatWildcard(t *testing.T) {
	if got := Default().HeartbeatWildcard(); got != "acc.*.heartbeat.ctrl.*" {
		t.Errorf("HeartbeatWildcard = %q", got)
	}
}

// A heartbeat must NOT satisfy the events stream's filter — it would otherwise be
// captured as an audit row, the very thing the heartbeat channel avoids.
func TestHeartbeatNotAnEvent(t *testing.T) {
	s := Default()
	subj := s.Heartbeat("hq", "ctrl-hq-1")
	if _, _, _, _, ok := s.ParseEvent(subj); ok {
		t.Errorf("ParseEvent(%q) ok=true, want false (heartbeat must not parse as an event)", subj)
	}
	// Nor is it a command: it is on the heartbeat channel, outside cmd and evt.
	if _, _, _, _, ok := s.ParseCommand(subj); ok {
		t.Errorf("ParseCommand(%q) ok=true, want false", subj)
	}
}

func TestParseHeartbeatRejects(t *testing.T) {
	s := Default()
	cases := []string{
		"acc.hq.evt.door.lobby-main.tap",    // a portal event
		"acc.hq.tap.door.lobby-main",        // a tap
		"acc.hq.evt.ctrl.ctrl-hq-1.state",   // ctrl-scoped but under evt
		"acc.hq.ctrl.ctrl-hq-1",             // too short
		"other.hq.heartbeat.ctrl.ctrl-hq-1", // wrong app
		"acc.hq.ctrl.ctrl-hq-1.heartbeat",   // the shape before ADR 0008
		"acc.hq.heartbeat.thing.ctrl-hq-1",  // not ctrl-scoped
	}
	for _, subj := range cases {
		if _, _, ok := s.ParseHeartbeat(subj); ok {
			t.Errorf("ParseHeartbeat(%q) ok=true, want false", subj)
		}
	}
}

// The health subject is a ctrl-scoped command on the cmd channel, and not
// an event, so a health request can never land in the audit stream.
func TestHealthSubject(t *testing.T) {
	s := Default()
	subj := s.Health("hq", "ctrl-hq-1")
	if subj != "acc.hq.cmd.ctrl.ctrl-hq-1.health" {
		t.Fatalf("Health = %q", subj)
	}
	if _, _, _, _, ok := s.ParseEvent(subj); ok {
		t.Errorf("ParseEvent(%q) ok=true, want false (a health request must not parse as an event)", subj)
	}
	if _, _, ok := s.ParseHeartbeat(subj); ok {
		t.Errorf("ParseHeartbeat(%q) ok=true, want false", subj)
	}
}
