package subjects

import "testing"

func TestBuild(t *testing.T) {
	s := Default()
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"tap", s.Tap("hq", "door", "lobby-main"), "hq.door.lobby-main.acc.tap"},
		{"posture", s.Posture("hq", "door", "lobby-main"), "hq.door.lobby-main.acc.cmd.posture"},
		{"unlock", s.Unlock("hq", "door", "lobby-main"), "hq.door.lobby-main.acc.cmd.unlock"},
		{"tapWildcard", s.TapWildcard("hq"), "hq.*.*.acc.tap"},
		{"postureWildcard", s.PostureWildcard("hq"), "hq.*.*.acc.cmd.posture"},
		{"unlockWildcard", s.UnlockWildcard("hq"), "hq.*.*.acc.cmd.unlock"},
		{"fire", s.Fire("hq"), "hq.acc.evt.fire"},
		{"eventTap", s.EventTap("hq", "door", "lobby-main"), "hq.door.lobby-main.acc.evt.tap"},
		{"eventState", s.EventState("hq", "door", "lobby-main"), "hq.door.lobby-main.acc.evt.state"},
		{"eventAlarm", s.EventAlarm("hq", "door", "lobby-main"), "hq.door.lobby-main.acc.evt.alarm"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

func TestEventsWildcards(t *testing.T) {
	want := []string{"*.acc.evt.>", "*.*.*.acc.evt.>"}
	got := Default().EventsWildcards()
	if len(got) != len(want) {
		t.Fatalf("EventsWildcards() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("EventsWildcards()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// The zero value behaves as the default app, and a custom app threads through.
func TestApp(t *testing.T) {
	if got := (Subjects{}).EventsWildcards()[0]; got != "*.acc.evt.>" {
		t.Errorf("zero value EventsWildcards()[0] = %q, want *.acc.evt.>", got)
	}
	if got := New("pacs").EventTap("hq", "door", "d1"); got != "hq.door.d1.pacs.evt.tap" {
		t.Errorf("custom app EventTap = %q, want hq.door.d1.pacs.evt.tap", got)
	}
}

func TestParseEvent(t *testing.T) {
	s := Default()
	cases := []struct {
		subject                       string
		location, ptype, thing, kind string
		ok                            bool
	}{
		{"hq.door.lobby-main.acc.evt.tap", "hq", "door", "lobby-main", "tap", true},
		{"hq.door.lobby-main.acc.evt.state", "hq", "door", "lobby-main", "state", true},
		{"hq.door.lobby-main.acc.evt.alarm", "hq", "door", "lobby-main", "alarm", true},
		{"hq.acc.evt.fire", "hq", "", "", "fire", true},
		{"hq.acc.evt.tap", "", "", "", "", false},                  // 4-token must be fire
		{"hq.acc.evt", "", "", "", "", false},                      // too short
		{"warehouse.camera.cam-042.evt.motion", "", "", "", "", false}, // foreign Thing (no app seg)
		{"hq.door.lobby.acc.cmd.posture", "", "", "", "", false},   // command, not event
		{"hq.door.lobby.x.evt.tap", "", "", "", "", false},         // wrong app token
		{"hq.door.lobby.acc.evt.tap.extra", "", "", "", "", false}, // too long
	}
	for _, tc := range cases {
		location, ptype, thing, kind, ok := s.ParseEvent(tc.subject)
		if location != tc.location || ptype != tc.ptype || thing != tc.thing || kind != tc.kind || ok != tc.ok {
			t.Errorf("ParseEvent(%q) = (%q,%q,%q,%q,%v), want (%q,%q,%q,%q,%v)",
				tc.subject, location, ptype, thing, kind, ok, tc.location, tc.ptype, tc.thing, tc.kind, tc.ok)
		}
	}
}

func TestParseCommand(t *testing.T) {
	s := Default()
	cases := []struct {
		subject                         string
		location, ptype, thing, action string
		ok                              bool
	}{
		{"hq.door.lobby-main.acc.cmd.posture", "hq", "door", "lobby-main", "posture", true},
		{"hq.door.lobby-main.acc.cmd.unlock", "hq", "door", "lobby-main", "unlock", true},
		{"hq.door.lobby-main.acc.evt.tap", "", "", "", "", false}, // event, not command
		{"hq.door.lobby-main.acc.cmd", "", "", "", "", false},     // too short
		{"hq.door.lobby.x.cmd.posture", "", "", "", "", false},    // wrong app token
	}
	for _, tc := range cases {
		location, ptype, thing, action, ok := s.ParseCommand(tc.subject)
		if location != tc.location || ptype != tc.ptype || thing != tc.thing || action != tc.action || ok != tc.ok {
			t.Errorf("ParseCommand(%q) = (%q,%q,%q,%q,%v), want (%q,%q,%q,%q,%v)",
				tc.subject, location, ptype, thing, action, ok, tc.location, tc.ptype, tc.thing, tc.action, tc.ok)
		}
	}
}
