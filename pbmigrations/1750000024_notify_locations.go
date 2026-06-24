package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Notification recipient scoping — route alarm email by location.
//
// 1750000023 made alarm email a two-sided opt-in (a source sets notify_on_alarm /
// notify_fire, an operator sets users.notify), but the routing was a global
// cross-product: every notify operator received every notify source's alarm,
// system-wide. In a multi-site deployment that pages the wrong people.
//
// This adds users.notify_locations: the set of locations an operator is paged for.
// accessd's notify sink now mails an operator only when notify is on AND
// (notify_locations is empty OR contains the alarm's location). Empty = all
// locations, so existing operators keep their current receive-everything behavior
// until someone narrows them — purely additive and fail-safe (a dangling location
// matches no scoped operator, so it falls through to the unscoped recipients).
// Like every other notify flag it is control-plane only: the sink reads it
// directly; it never touches policy.Decide, the KV mirror, or the controller.
func init() {
	migrations.Register(func(app core.App) error {
		locations, err := app.FindCollectionByNameOrId("locations")
		if err != nil {
			return err
		}
		users, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}
		users.Fields.Add(&core.RelationField{
			Name:         "notify_locations",
			CollectionId: locations.Id,
			MaxSelect:    9999,
		})
		return app.Save(users)
	}, func(app core.App) error {
		users, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			return nil // collection gone — nothing to revert
		}
		users.Fields.RemoveByName("notify_locations")
		return app.Save(users)
	})
}
