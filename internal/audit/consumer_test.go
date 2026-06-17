package audit

import (
	"testing"

	"github.com/pocketbase/pocketbase/tests"
	"github.com/stone-age-io/access-control/internal/logger"

	// Side-effect import registers the schema (events collection) + fixture.
	_ "github.com/stone-age-io/access-control/pbmigrations"
)

func TestParseSubject(t *testing.T) {
	cases := []struct {
		subject           string
		site, point, kind string
	}{
		{"acc.evt.hq.lobby-main.tap", "hq", "lobby-main", "tap"},
		{"acc.evt.hq.lobby-main.state", "hq", "lobby-main", "state"},
		{"acc.evt.hq.lobby-main.alarm", "hq", "lobby-main", "alarm"},
		{"acc.evt.hq.fire", "hq", "", "fire"},
		{"acc.evt.hq", "", "", ""},             // too short
		{"other.evt.hq.lobby.tap", "", "", ""}, // wrong root
		{"acc.evt.hq.a.b.c", "", "", ""},       // too long
	}
	for _, tc := range cases {
		site, point, kind := parseSubject(tc.subject)
		if site != tc.site || point != tc.point || kind != tc.kind {
			t.Errorf("parseSubject(%q) = (%q,%q,%q), want (%q,%q,%q)",
				tc.subject, site, point, kind, tc.site, tc.point, tc.kind)
		}
	}
}

func newConsumer(t *testing.T) (*Consumer, *tests.TestApp) {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(app.Cleanup)
	return New(app, nil, "ACC_EVENTS", logger.NewNopLogger(), nil), app
}

// A tap event maps onto the events row and persists (validating against the
// collection schema, including the kind select).
func TestRecordFromTap(t *testing.T) {
	c, app := newConsumer(t)
	data := []byte(`{"cred":"CARD-001","user":"u_alice","allow":true,"reason":"allow_grant","ts":"2026-01-05T14:00:00Z"}`)

	rec, ok, err := c.recordFrom("acc.evt.hq.lobby-main.tap", data)
	if err != nil || !ok {
		t.Fatalf("recordFrom: ok=%v err=%v", ok, err)
	}
	if rec.GetString("site") != "hq" || rec.GetString("access_point") != "lobby-main" || rec.GetString("kind") != "tap" {
		t.Errorf("subject fields = (%q,%q,%q), want (hq,lobby-main,tap)",
			rec.GetString("site"), rec.GetString("access_point"), rec.GetString("kind"))
	}
	if rec.GetString("credential") != "CARD-001" || rec.GetString("user") != "u_alice" {
		t.Errorf("credential/user = (%q,%q)", rec.GetString("credential"), rec.GetString("user"))
	}
	if !rec.GetBool("allow") || rec.GetString("reason") != "allow_grant" {
		t.Errorf("allow/reason = (%v,%q)", rec.GetBool("allow"), rec.GetString("reason"))
	}

	// It must persist (proves it validates against the events schema).
	if err := app.Save(rec); err != nil {
		t.Fatalf("save events row: %v", err)
	}
	if _, err := app.FindFirstRecordByData("events", "credential", "CARD-001"); err != nil {
		t.Errorf("saved events row not found: %v", err)
	}
}

func TestRecordFromFire(t *testing.T) {
	c, _ := newConsumer(t)
	rec, ok, err := c.recordFrom("acc.evt.hq.fire", []byte(`{"active":true}`))
	if err != nil || !ok {
		t.Fatalf("recordFrom: ok=%v err=%v", ok, err)
	}
	if rec.GetString("site") != "hq" || rec.GetString("access_point") != "" || rec.GetString("kind") != "fire" {
		t.Errorf("fire fields = (%q,%q,%q), want (hq,,fire)",
			rec.GetString("site"), rec.GetString("access_point"), rec.GetString("kind"))
	}
}

func TestRecordFromUnrecognizedSubject(t *testing.T) {
	c, _ := newConsumer(t)
	if _, ok, err := c.recordFrom("acc.evt.hq", []byte(`{}`)); ok || err != nil {
		t.Errorf("recordFrom(too short) = ok=%v err=%v, want ok=false err=nil", ok, err)
	}
}
