package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Adds operator-acknowledgement fields to events so an alarm/fire is a thing a
// human resolves, not a transient flash: acknowledged (default false), ack_by
// (operator email), ack_at (when). Set by the POST /api/events/{id}/ack route
// (internal/commandapi), gated by the `command` capability. Additive and
// optional — older rows simply read acknowledged=false.
//
// Known v1 wart (documented, accepted): the audit consumer creates a fresh row
// per delivered message, so a stream replay can resurrect an unacked duplicate.
// The alarm console bounds this with a time window; a dedicated active_alarms
// upsert-projection is the deferred clean fix.
func init() {
	migrations.Register(func(app core.App) error {
		events, err := app.FindCollectionByNameOrId("events")
		if err != nil {
			return err
		}
		events.Fields.Add(&core.BoolField{Name: "acknowledged"})
		events.Fields.Add(&core.TextField{Name: "ack_by"})
		events.Fields.Add(&core.DateField{Name: "ack_at"})
		return app.Save(events)
	}, func(app core.App) error {
		events, err := app.FindCollectionByNameOrId("events")
		if err != nil {
			return nil // already gone
		}
		events.Fields.RemoveByName("acknowledged")
		events.Fields.RemoveByName("ack_by")
		events.Fields.RemoveByName("ack_at")
		return app.Save(events)
	})
}
