package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Adds roles.visitor_preset — marks a role as offerable in the visitor mint flow.
//
// The flag is on ROLES, not access_groups, because the graph is
// cardholder → roles → access_groups → (portals + schedule): a role is the unit a
// cardholder can actually be assigned. Marking a group would leave the mint flow
// with nothing to attach it to.
//
// Why a curated preset at all, rather than letting the operator pick doors per
// visitor: minting an access_group (or role) per visitor would add a row and a KV
// key per guest, pollute the policy graph with thousands of single-use entries, and
// create a garbage-collection problem nobody wants to own. An operator curating a
// few reusable roles ("Lobby + Elevator, business hours") keeps the graph the size
// of the building rather than the size of its visitor log.
//
// Mirrored? The FLAG is not — policykv.Role carries only code + groups, so this is
// control-plane-only metadata and the edge never sees it. The role itself is
// mirrored as usual, so a visitor's access works exactly like anyone else's.
func init() {
	migrations.Register(func(app core.App) error {
		c, err := app.FindCollectionByNameOrId("roles")
		if err != nil {
			return err
		}
		c.Fields.Add(&core.BoolField{Name: "visitor_preset"})
		return app.Save(c)
	}, func(app core.App) error {
		c, err := app.FindCollectionByNameOrId("roles")
		if err != nil {
			return nil // already gone
		}
		c.Fields.RemoveByName("visitor_preset")
		return app.Save(c)
	})
}
