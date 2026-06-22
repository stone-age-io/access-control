package audit

import (
	"testing"

	"github.com/pocketbase/pocketbase/tests"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/subjects"

	// Side-effect import registers the schema (events collection) + fixture.
	_ "github.com/stone-age-io/access-control/pbmigrations"
)

func newConsumer(t *testing.T) (*Consumer, *tests.TestApp) {
	t.Helper()
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp: %v", err)
	}
	t.Cleanup(app.Cleanup)
	return New(app, nil, "ACC_EVENTS", subjects.Default(), logger.NewNopLogger(), nil), app
}

// A tap event maps onto the events row and persists (validating against the
// collection schema, including the kind select).
func TestRecordFromTap(t *testing.T) {
	c, app := newConsumer(t)
	data := []byte(`{"cred":"CARD-001","user":"u_alice","allow":true,"reason":"allow_grant","ts":"2026-01-05T14:00:00Z","source":"osdp"}`)

	rec, ok, err := c.recordFrom("acc.hq.door.lobby-main.evt.tap", data)
	if err != nil || !ok {
		t.Fatalf("recordFrom: ok=%v err=%v", ok, err)
	}
	if rec.GetString("location") != "hq" || rec.GetString("portal") != "lobby-main" ||
		rec.GetString("type") != "door" || rec.GetString("kind") != "tap" {
		t.Errorf("subject fields = (%q,%q,%q,%q), want (hq,lobby-main,door,tap)",
			rec.GetString("location"), rec.GetString("portal"), rec.GetString("type"), rec.GetString("kind"))
	}
	if rec.GetString("credential") != "CARD-001" || rec.GetString("user") != "u_alice" {
		t.Errorf("credential/user = (%q,%q)", rec.GetString("credential"), rec.GetString("user"))
	}
	if !rec.GetBool("allow") || rec.GetString("reason") != "allow_grant" {
		t.Errorf("allow/reason = (%v,%q)", rec.GetBool("allow"), rec.GetString("reason"))
	}
	if rec.GetString("source") != "osdp" {
		t.Errorf("source = %q, want osdp", rec.GetString("source"))
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
	rec, ok, err := c.recordFrom("acc.hq.evt.fire", []byte(`{"active":true}`))
	if err != nil || !ok {
		t.Fatalf("recordFrom: ok=%v err=%v", ok, err)
	}
	if rec.GetString("location") != "hq" || rec.GetString("portal") != "" ||
		rec.GetString("type") != "" || rec.GetString("kind") != "fire" {
		t.Errorf("fire fields = (%q,%q,%q,%q), want (hq,,,fire)",
			rec.GetString("location"), rec.GetString("portal"), rec.GetString("type"), rec.GetString("kind"))
	}
}

func TestRecordFromUnrecognizedSubject(t *testing.T) {
	c, _ := newConsumer(t)
	if _, ok, err := c.recordFrom("acc.hq.evt", []byte(`{}`)); ok || err != nil {
		t.Errorf("recordFrom(too short) = ok=%v err=%v, want ok=false err=nil", ok, err)
	}
}
