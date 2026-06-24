package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Notification opt-in — move the alarm-email "who" and "which" out of accessd.yaml
// and into the data, so operators manage it in the UI instead of a config redeploy.
//
// Before this, internal/notify read a static config list (notify.recipients) and
// emailed on EVERY alarm/fire. Now the notify sink is always-on and config-free
// (like the disarm sink) and inert unless two opt-ins line up:
//
//	users.notify           an operator who wants alarm email (the recipient set)
//	portals.notify_on_alarm  a door whose forced/held alarms should email
//	areas.notify_on_alarm    an area whose intrusion alarms should email
//	locations.notify_fire    a location whose fire-input alarms should email
//
// The source flag rides the record that emits the alarm (the same idiom as
// portals.disarm_on_grant and schedules.ignore_holidays). All four are
// control-plane only — accessd's notify sink reads them directly; they never touch
// policy.Decide, the KV mirror, or the controller.
func init() {
	migrations.Register(func(app core.App) error {
		for _, spec := range []struct{ collection, field string }{
			{"users", "notify"},
			{"portals", "notify_on_alarm"},
			{"areas", "notify_on_alarm"},
			{"locations", "notify_fire"},
		} {
			c, err := app.FindCollectionByNameOrId(spec.collection)
			if err != nil {
				return err
			}
			c.Fields.Add(&core.BoolField{Name: spec.field})
			if err := app.Save(c); err != nil {
				return err
			}
		}
		return nil
	}, func(app core.App) error {
		for _, spec := range []struct{ collection, field string }{
			{"users", "notify"},
			{"portals", "notify_on_alarm"},
			{"areas", "notify_on_alarm"},
			{"locations", "notify_fire"},
		} {
			c, err := app.FindCollectionByNameOrId(spec.collection)
			if err != nil {
				continue // collection gone — nothing to revert
			}
			c.Fields.RemoveByName(spec.field)
			if err := app.Save(c); err != nil {
				return err
			}
		}
		return nil
	})
}
