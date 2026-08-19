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

// The stream_seq idempotency contract: a projected sequence is detected
// (alreadyProjected), a racing double-write trips the unique index, and legacy
// rows (stream_seq 0, from before the field existed) stay exempt from it.
func TestStreamSeqDedupe(t *testing.T) {
	c, app := newConsumer(t)
	data := []byte(`{"cred":"CARD-002","allow":false,"reason":"deny_no_access"}`)

	rec, ok, err := c.recordFrom("acc.hq.door.lobby-main.evt.tap", data)
	if err != nil || !ok {
		t.Fatalf("recordFrom: ok=%v err=%v", ok, err)
	}
	rec.Set("stream_seq", 42)
	if err := app.Save(rec); err != nil {
		t.Fatalf("save first row: %v", err)
	}

	if !c.alreadyProjected(42) {
		t.Errorf("alreadyProjected(42) = false, want true")
	}
	if c.alreadyProjected(43) {
		t.Errorf("alreadyProjected(43) = true, want false")
	}

	// Unique-index backstop: a second row with the same sequence must not save.
	dup, _, _ := c.recordFrom("acc.hq.door.lobby-main.evt.tap", data)
	dup.Set("stream_seq", 42)
	if err := app.Save(dup); err == nil {
		t.Errorf("saving duplicate stream_seq succeeded, want unique-index error")
	}

	// Legacy rows: any number of stream_seq-less rows coexist.
	for i := 0; i < 2; i++ {
		legacy, _, _ := c.recordFrom("acc.hq.door.lobby-main.evt.tap", data)
		if err := app.Save(legacy); err != nil {
			t.Fatalf("save legacy row %d: %v", i, err)
		}
	}
}

func TestRecordFromUnrecognizedSubject(t *testing.T) {
	c, _ := newConsumer(t)
	if _, ok, err := c.recordFrom("acc.hq.evt", []byte(`{}`)); ok || err != nil {
		t.Errorf("recordFrom(too short) = ok=%v err=%v, want ok=false err=nil", ok, err)
	}
}

// The values a cmd.grant carries — an operator door-pop and a badge holder's own
// remote unlock — must reach the column. They were emitted before migration
// 1750000044 widened the select to accept them, so every one of them failed to
// project and was redelivered forever.
func TestRecordFromCommandAndBadgeSources(t *testing.T) {
	for _, source := range []string{"command", "badge", "nats", "osdp"} {
		c, app := newConsumer(t)
		data := []byte(`{"cred":"CARD-009","allow":true,"reason":"allow_command_grant","source":"` + source + `"}`)

		rec, ok, err := c.recordFrom("acc.hq.door.lobby-main.evt.tap", data)
		if err != nil || !ok {
			t.Fatalf("%s: recordFrom: ok=%v err=%v", source, ok, err)
		}
		if got := rec.GetString("source"); got != source {
			t.Errorf("%s: source = %q, want %q", source, got, source)
		}
		if err := app.Save(rec); err != nil {
			t.Fatalf("%s: save events row: %v", source, err)
		}
	}
}

// A source value from another vocabulary must not take the row down with it. The
// arm-transition event used to ship arm provenance (standing/scheduled/override)
// under `source`; PocketBase rejected the out-of-range select value, so the whole
// row failed and the consumer retried it forever. The value belongs in payload,
// where it is still queryable, and the row must still save.
func TestRecordFromForeignSourceStillProjects(t *testing.T) {
	c, app := newConsumer(t)
	data := []byte(`{"arm":"armed","previous":"disarmed","source":"override"}`)

	rec, ok, err := c.recordFrom("acc.hq.area.warehouse.evt.state", data)
	if err != nil || !ok {
		t.Fatalf("recordFrom: ok=%v err=%v", ok, err)
	}
	if got := rec.GetString("source"); got != "" {
		t.Errorf("source = %q, want empty (a foreign value must not reach the column)", got)
	}
	if err := app.Save(rec); err != nil {
		t.Fatalf("save events row: %v — a row the projection can never accept is an error loop", err)
	}

	saved, err := app.FindFirstRecordByData("events", "portal", "warehouse")
	if err != nil {
		t.Fatalf("saved events row not found: %v", err)
	}
	var payload map[string]any
	if err := saved.UnmarshalJSONField("payload", &payload); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	if payload["source"] != "override" {
		t.Errorf("payload source = %v, want override (dropped from the column, kept in payload)", payload["source"])
	}
}
