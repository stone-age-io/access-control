package subjects

import "testing"

func TestBuild(t *testing.T) {
	s := Default()
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"tap", s.Tap("hq", "door", "lobby-main"), "acc.hq.door.lobby-main.tap"},
		{"posture", s.Posture("hq", "door", "lobby-main"), "acc.hq.door.lobby-main.cmd.posture"},
		{"grant", s.Grant("hq", "door", "lobby-main"), "acc.hq.door.lobby-main.cmd.grant"},
		{"tapWildcard", s.TapWildcard("hq"), "acc.hq.*.*.tap"},
		{"postureWildcard", s.PostureWildcard("hq"), "acc.hq.*.*.cmd.posture"},
		{"grantWildcard", s.GrantWildcard("hq"), "acc.hq.*.*.cmd.grant"},
		{"output", s.Output("hq", "gate-1"), "acc.hq.auxout.gate-1.cmd.output"},
		{"outputWildcard", s.OutputWildcard("hq"), "acc.hq.*.*.cmd.output"},
		{"fire", s.Fire("hq"), "acc.hq.evt.fire"},
		{"eventTap", s.EventTap("hq", "door", "lobby-main"), "acc.hq.door.lobby-main.evt.tap"},
		{"eventState", s.EventState("hq", "door", "lobby-main"), "acc.hq.door.lobby-main.evt.state"},
		{"eventAlarm", s.EventAlarm("hq", "door", "lobby-main"), "acc.hq.door.lobby-main.evt.alarm"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

func TestEventsWildcards(t *testing.T) {
	want := []string{"acc.*.evt.fire", "acc.*.*.*.evt.>"}
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
	if got := (Subjects{}).EventsWildcards()[0]; got != "acc.*.evt.fire" {
		t.Errorf("zero value EventsWildcards()[0] = %q, want acc.*.evt.fire", got)
	}
	if got := New("pacs").EventTap("hq", "door", "d1"); got != "pacs.hq.door.d1.evt.tap" {
		t.Errorf("custom app EventTap = %q, want pacs.hq.door.d1.evt.tap", got)
	}
}

func TestParseEvent(t *testing.T) {
	s := Default()
	cases := []struct {
		subject                      string
		location, ptype, thing, kind string
		ok                           bool
	}{
		{"acc.hq.door.lobby-main.evt.tap", "hq", "door", "lobby-main", "tap", true},
		{"acc.hq.door.lobby-main.evt.state", "hq", "door", "lobby-main", "state", true},
		{"acc.hq.door.lobby-main.evt.alarm", "hq", "door", "lobby-main", "alarm", true},
		{"acc.hq.evt.fire", "hq", "", "", "fire", true},
		{"acc.hq.evt.tap", "", "", "", "", false},                      // 4-token must be fire
		{"acc.hq.evt", "", "", "", "", false},                          // too short
		{"warehouse.camera.cam-042.evt.motion", "", "", "", "", false}, // foreign Thing (not app-rooted)
		{"acc.hq.door.lobby.cmd.posture", "", "", "", "", false},       // command, not event
		{"pacs.hq.door.lobby.evt.tap", "", "", "", "", false},          // wrong app token
		{"acc.hq.door.lobby.evt.tap.extra", "", "", "", "", false},     // too long
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
		subject                        string
		location, ptype, thing, action string
		ok                             bool
	}{
		{"acc.hq.door.lobby-main.cmd.posture", "hq", "door", "lobby-main", "posture", true},
		{"acc.hq.door.lobby-main.cmd.grant", "hq", "door", "lobby-main", "grant", true},
		{"acc.hq.door.lobby-main.evt.tap", "", "", "", "", false}, // event, not command
		{"acc.hq.door.lobby-main.cmd", "", "", "", "", false},     // too short
		{"pacs.hq.door.lobby.cmd.posture", "", "", "", "", false}, // wrong app token
	}
	for _, tc := range cases {
		location, ptype, thing, action, ok := s.ParseCommand(tc.subject)
		if location != tc.location || ptype != tc.ptype || thing != tc.thing || action != tc.action || ok != tc.ok {
			t.Errorf("ParseCommand(%q) = (%q,%q,%q,%q,%v), want (%q,%q,%q,%q,%v)",
				tc.subject, location, ptype, thing, action, ok, tc.location, tc.ptype, tc.thing, tc.action, tc.ok)
		}
	}
}
