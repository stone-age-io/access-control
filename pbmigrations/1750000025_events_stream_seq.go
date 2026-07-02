package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Adds stream_seq to events: the JetStream stream sequence of the message a row
// was projected from, making the audit projection idempotent. The consumer skips
// a message whose sequence already has a row (a redelivery after a slow or
// failed ack), so at-least-once delivery no longer produces duplicate rows —
// and a stream replay no longer resurrects an already-acknowledged alarm.
//
// The unique index is partial (stream_seq != 0): rows projected before this
// migration read 0 and stay exempt, so the index can be created over existing
// data and legacy duplicates keep working.
func init() {
	migrations.Register(func(app core.App) error {
		events, err := app.FindCollectionByNameOrId("events")
		if err != nil {
			return err
		}
		events.Fields.Add(&core.NumberField{Name: "stream_seq", OnlyInt: true})
		events.AddIndex("idx_events_stream_seq", true, "stream_seq", "stream_seq != 0")
		return app.Save(events)
	}, func(app core.App) error {
		events, err := app.FindCollectionByNameOrId("events")
		if err != nil {
			return nil // already gone
		}
		events.RemoveIndex("idx_events_stream_seq")
		events.Fields.RemoveByName("stream_seq")
		return app.Save(events)
	})
}
