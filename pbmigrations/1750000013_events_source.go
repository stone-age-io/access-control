package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Adds source to events: which reader transport produced a tap — "nats" (a tap
// published over NATS) or "osdp" (a physical read on the controller's RS485
// bus). Set only on tap events; empty for state/alarm/fire rows and for taps
// from an older controller that didn't stamp it. Lets operators tell a physical
// presentation from a NATS-published one forensically. Additive and optional,
// mirroring the events.kind select field.
func init() {
	migrations.Register(func(app core.App) error {
		events, err := app.FindCollectionByNameOrId("events")
		if err != nil {
			return err
		}
		events.Fields.Add(&core.SelectField{
			Name:      "source",
			Values:    []string{"nats", "osdp"},
			MaxSelect: 1,
		})
		return app.Save(events)
	}, func(app core.App) error {
		events, err := app.FindCollectionByNameOrId("events")
		if err != nil {
			return nil // already gone
		}
		events.Fields.RemoveByName("source")
		return app.Save(events)
	})
}
