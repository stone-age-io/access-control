package subjects

import "testing"

func TestBuild(t *testing.T) {
	s := Default()
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"tap", s.Tap("hq", "lobby-main"), "acc.tap.hq.lobby-main"},
		{"posture", s.Posture("hq", "lobby-main"), "acc.cmd.hq.lobby-main.posture"},
		{"unlock", s.Unlock("hq", "lobby-main"), "acc.cmd.hq.lobby-main.unlock"},
		{"postureWildcard", s.PostureWildcard("hq"), "acc.cmd.hq.*.posture"},
		{"unlockWildcard", s.UnlockWildcard("hq"), "acc.cmd.hq.*.unlock"},
		{"fire", s.Fire("hq"), "acc.evt.hq.fire"},
		{"eventTap", s.EventTap("hq", "lobby-main"), "acc.evt.hq.lobby-main.tap"},
		{"eventState", s.EventState("hq", "lobby-main"), "acc.evt.hq.lobby-main.state"},
		{"eventAlarm", s.EventAlarm("hq", "lobby-main"), "acc.evt.hq.lobby-main.alarm"},
		{"eventsWildcard", s.EventsWildcard(), "acc.evt.>"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

// The zero value behaves as the default root, and a custom root threads through.
func TestRoot(t *testing.T) {
	if got := (Subjects{}).EventsWildcard(); got != "acc.evt.>" {
		t.Errorf("zero value EventsWildcard = %q, want acc.evt.>", got)
	}
	if got := New("pacs").EventTap("hq", "door"); got != "pacs.evt.hq.door.tap" {
		t.Errorf("custom root EventTap = %q, want pacs.evt.hq.door.tap", got)
	}
}

func TestParseEvent(t *testing.T) {
	s := Default()
	cases := []struct {
		subject           string
		site, point, kind string
		ok                bool
	}{
		{"acc.evt.hq.lobby-main.tap", "hq", "lobby-main", "tap", true},
		{"acc.evt.hq.lobby-main.state", "hq", "lobby-main", "state", true},
		{"acc.evt.hq.lobby-main.alarm", "hq", "lobby-main", "alarm", true},
		{"acc.evt.hq.fire", "hq", "", "fire", true},
		{"acc.evt.hq", "", "", "", false},               // too short
		{"other.evt.hq.lobby.tap", "", "", "", false},   // wrong root
		{"acc.cmd.hq.lobby.posture", "", "", "", false}, // not an event subject
		{"acc.evt.hq.a.b.c", "", "", "", false},         // too long
	}
	for _, tc := range cases {
		site, point, kind, ok := s.ParseEvent(tc.subject)
		if site != tc.site || point != tc.point || kind != tc.kind || ok != tc.ok {
			t.Errorf("ParseEvent(%q) = (%q,%q,%q,%v), want (%q,%q,%q,%v)",
				tc.subject, site, point, kind, ok, tc.site, tc.point, tc.kind, tc.ok)
		}
	}
}

func TestParseCommand(t *testing.T) {
	s := Default()
	cases := []struct {
		subject             string
		site, point, action string
		ok                  bool
	}{
		{"acc.cmd.hq.lobby-main.posture", "hq", "lobby-main", "posture", true},
		{"acc.cmd.hq.lobby-main.unlock", "hq", "lobby-main", "unlock", true},
		{"acc.evt.hq.lobby-main.tap", "", "", "", false},  // not a command subject
		{"acc.cmd.hq.lobby-main", "", "", "", false},      // too short
		{"other.cmd.hq.lobby.posture", "", "", "", false}, // wrong root
	}
	for _, tc := range cases {
		site, point, action, ok := s.ParseCommand(tc.subject)
		if site != tc.site || point != tc.point || action != tc.action || ok != tc.ok {
			t.Errorf("ParseCommand(%q) = (%q,%q,%q,%v), want (%q,%q,%q,%v)",
				tc.subject, site, point, action, ok, tc.site, tc.point, tc.action, tc.ok)
		}
	}
}
