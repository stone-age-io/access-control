package subjects

import (
	"testing"

	kitsubjects "github.com/stone-age-io/kit/subjects"
)

func TestBuild(t *testing.T) {
	s := Default()
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"tap", s.Tap("hq", "door", "lobby-main"), "acc.hq.tap.door.lobby-main"},
		{"posture", s.Posture("hq", "door", "lobby-main"), "acc.hq.cmd.door.lobby-main.posture"},
		{"grant", s.Grant("hq", "door", "lobby-main"), "acc.hq.cmd.door.lobby-main.grant"},
		{"tapWildcard", s.TapWildcard("hq"), "acc.hq.tap.*.*"},
		{"postureWildcard", s.PostureWildcard("hq"), "acc.hq.cmd.*.*.posture"},
		{"grantWildcard", s.GrantWildcard("hq"), "acc.hq.cmd.*.*.grant"},
		{"output", s.Output("hq", "gate-1"), "acc.hq.cmd.auxout.gate-1.output"},
		{"outputWildcard", s.OutputWildcard("hq"), "acc.hq.cmd.*.*.output"},
		{"fire", s.Fire("hq"), "acc.hq.evt.fire"},
		{"eventTap", s.EventTap("hq", "door", "lobby-main"), "acc.hq.evt.door.lobby-main.tap"},
		{"eventState", s.EventState("hq", "door", "lobby-main"), "acc.hq.evt.door.lobby-main.state"},
		{"eventAlarm", s.EventAlarm("hq", "door", "lobby-main"), "acc.hq.evt.door.lobby-main.alarm"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

func TestEventsWildcards(t *testing.T) {
	want := []string{"acc.*.evt.>"}
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
	if got := (Subjects{}).EventsWildcards()[0]; got != "acc.*.evt.>" {
		t.Errorf("zero value EventsWildcards()[0] = %q, want acc.*.evt.>", got)
	}
	if got := New("pacs").EventTap("hq", "door", "d1"); got != "pacs.hq.evt.door.d1.tap" {
		t.Errorf("custom app EventTap = %q, want pacs.hq.evt.door.d1.tap", got)
	}
}

func TestParseEvent(t *testing.T) {
	s := Default()
	cases := []struct {
		subject                      string
		location, ptype, thing, kind string
		ok                           bool
	}{
		{"acc.hq.evt.door.lobby-main.tap", "hq", "door", "lobby-main", "tap", true},
		{"acc.hq.evt.door.lobby-main.state", "hq", "door", "lobby-main", "state", true},
		{"acc.hq.evt.door.lobby-main.alarm", "hq", "door", "lobby-main", "alarm", true},
		{"acc.hq.evt.fire", "hq", "", "", "fire", true},
		{"acc.hq.evt.tap", "", "", "", "", false},                      // 4-token must be fire
		{"acc.hq.evt", "", "", "", "", false},                          // too short
		{"warehouse.camera.cam-042.evt.motion", "", "", "", "", false}, // foreign Thing (not app-rooted)
		{"acc.hq.cmd.door.lobby.posture", "", "", "", "", false},       // command, not event
		{"pacs.hq.evt.door.lobby.tap", "", "", "", "", false},          // wrong app token
		{"acc.hq.door.lobby.evt.tap", "", "", "", "", false},           // the shape before ADR 0008
		{"acc.hq.evt.door.lobby.tap.extra", "", "", "", "", false},     // too long
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
		{"acc.hq.cmd.door.lobby-main.posture", "hq", "door", "lobby-main", "posture", true},
		{"acc.hq.cmd.door.lobby-main.grant", "hq", "door", "lobby-main", "grant", true},
		{"acc.hq.evt.door.lobby-main.tap", "", "", "", "", false}, // event, not command
		{"acc.hq.cmd.door.lobby-main", "", "", "", "", false},     // too short
		{"pacs.hq.cmd.door.lobby.posture", "", "", "", "", false}, // wrong app token
		{"acc.hq.door.lobby.cmd.posture", "", "", "", "", false},  // the shape before ADR 0008
	}
	for _, tc := range cases {
		location, ptype, thing, action, ok := s.ParseCommand(tc.subject)
		if location != tc.location || ptype != tc.ptype || thing != tc.thing || action != tc.action || ok != tc.ok {
			t.Errorf("ParseCommand(%q) = (%q,%q,%q,%q,%v), want (%q,%q,%q,%q,%v)",
				tc.subject, location, ptype, thing, action, ok, tc.location, tc.ptype, tc.thing, tc.action, tc.ok)
		}
	}
}

// Every event publish carries a Nats-Msg-Id, fresh per event; a heartbeat or a
// command carries none, since no stream captures it.
func TestEventMsg(t *testing.T) {
	s := Default()
	for _, subj := range []string{s.EventTap("hq", "door", "d1"), s.Fire("hq"), s.EventState("hq", CtrlType, "c1")} {
		a, b := EventMsg(subj, nil), EventMsg(subj, nil)
		id := a.Header.Get("Nats-Msg-Id")
		if id == "" {
			t.Errorf("%s: no Nats-Msg-Id", subj)
		}
		if id == b.Header.Get("Nats-Msg-Id") {
			t.Errorf("%s: two events got the same Nats-Msg-Id", subj)
		}
	}
	for _, subj := range []string{s.Heartbeat("hq", "c1"), s.Grant("hq", "door", "d1"), s.Tap("hq", "door", "d1")} {
		if m := EventMsg(subj, nil); m.Header != nil {
			t.Errorf("%s: header %v, want none", subj, m.Header)
		}
	}
}

// The stream subject and each consumer's filters, through the kit's overlap
// check (ADR 0008). JetStream refuses a consumer whose filters overlap, and the
// notify sink has three.
func TestFiltersDisjoint(t *testing.T) {
	s := Default()
	if err := kitsubjects.Disjoint(s.NotifyWildcards()...); err != nil {
		t.Errorf("notify filters: %v", err)
	}
	stream := s.EventsWildcards()[0]
	for _, f := range append(s.NotifyWildcards(), s.TapEventWildcard()) {
		if !kitsubjects.Overlap(stream, f) {
			t.Errorf("consumer filter %s is outside the stream subject %s", f, stream)
		}
	}
	for _, f := range []string{s.TapWildcard("hq"), s.PostureWildcard("hq"), s.HeartbeatWildcard(), s.Health("hq", "c1")} {
		if kitsubjects.Overlap(stream, f) {
			t.Errorf("%s is not an event but the stream subject %s captures it", f, stream)
		}
	}
}
