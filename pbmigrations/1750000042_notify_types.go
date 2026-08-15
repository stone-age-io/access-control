package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Notification severity selection + the controller-offline source opt-in.
//
// Two fields, both control-plane only (the notify sink reads them directly; neither
// touches policy.Decide, the KV mirror, or the controller).
//
// users.notify_types — WHICH kinds of event an operator is paged for. Before this,
// opting in was all-or-nothing: forced, held and intrusion arrived together. `held`
// is by far the highest-volume of the three (a propped door in July) and the least
// urgent, so bundling it with `intrusion` is the reason people switch notifications
// off entirely — and a notification system nobody has switched on protects no one.
//
// EMPTY MEANS THE DEFAULT SET, not literally "all": forced, held, intrusion, fire,
// and controller_offline. `no_entry` (a grant nobody walked through) is diagnostic
// rather than urgent and must be selected explicitly, and `held_clear` is never
// emailed at all — it is a clear, not a raise. So existing operators keep exactly
// the behavior they have today, and a future urgent type reaches everyone who never
// narrowed their selection, which is the right default for an alarm system.
//
// controllers.notify_offline — the per-source opt-in for the liveness transitions
// that accessd now emits. It completes the two-sided AND for controllers, matching
// portals/areas.notify_on_alarm and locations.notify_fire: a source opts in, an
// operator opts in, and only then does mail move. Default false, like its siblings,
// so this migration turns nothing on by itself.
//
// events.repage_count — how many reminders internal/repage has sent for an alarm
// still nobody has acknowledged. It lives on the ROW, not in memory, because the
// reminder cap has to survive an accessd restart: an in-memory counter would reset
// on every restart and turn a bounded reminder into an unbounded one.
func init() {
	migrations.Register(func(app core.App) error {
		users, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}
		users.Fields.Add(&core.SelectField{
			Name: "notify_types",
			Values: []string{
				"forced", "held", "no_entry", "intrusion", "fire", "controller_offline",
			},
			MaxSelect: 6,
		})
		if err := app.Save(users); err != nil {
			return err
		}

		controllers, err := app.FindCollectionByNameOrId("controllers")
		if err != nil {
			return err
		}
		controllers.Fields.Add(&core.BoolField{Name: "notify_offline"})
		if err := app.Save(controllers); err != nil {
			return err
		}

		events, err := app.FindCollectionByNameOrId("events")
		if err != nil {
			return err
		}
		events.Fields.Add(&core.NumberField{Name: "repage_count", OnlyInt: true})
		return app.Save(events)
	}, func(app core.App) error {
		if users, err := app.FindCollectionByNameOrId("users"); err == nil {
			users.Fields.RemoveByName("notify_types")
			if err := app.Save(users); err != nil {
				return err
			}
		}
		if controllers, err := app.FindCollectionByNameOrId("controllers"); err == nil {
			controllers.Fields.RemoveByName("notify_offline")
			if err := app.Save(controllers); err != nil {
				return err
			}
		}
		if events, err := app.FindCollectionByNameOrId("events"); err == nil {
			events.Fields.RemoveByName("repage_count")
			return app.Save(events)
		}
		return nil
	})
}
