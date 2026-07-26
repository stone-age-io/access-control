package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Access groups grant AREAS and AUX OUTPUTS, not just portals.
//
// Until now an access group was {portals, schedule} and the only thing a person's
// badge could authorize was opening a door. Arm/disarm and aux-output control were
// operator-only capabilities on the `users` tier, reachable solely through
// internal/commandapi. That left no way to say "the warehouse crew may disarm the
// warehouse on their shift" — which is an ordinary PACS requirement, and is the
// same sentence as "may open these doors on their shift" with a different target.
//
// # Why the same collection, rather than area_groups / output_groups
//
// The schedule is the entire reason access groups exist. "Warehouse staff, Mon–Fri
// 06:00–18:00" is one window whether it authorizes a door, a disarm, or a gate
// relay, and separate collections would force an operator to recreate that pairing
// once per target kind and keep three of them in step. The graph in CLAUDE.md —
// user → roles → access groups → (targets + one schedule) — keeps its shape; only
// the leaf widens. The relations are independent, so an area-only or output-only
// group is just one with no portals: an empty relation grants nothing, which is
// the fail-closed direction already.
//
// # Why arm and disarm are separate rights
//
// Disarming turns intrusion detection off; arming turns it on. "May arm but not
// disarm" is a real role — closing staff, cleaners, anyone who locks up but should
// not be able to silence the building — so a single boolean would be wrong in a
// way that is expensive to correct later (retrofitting a right onto rows that were
// saved meaning "both" needs a data migration, and guessing wrong grants access).
// `area_rights` is therefore a two-value multi-select from the start.
//
// EMPTY area_rights grants NOTHING, deliberately: the fail-closed reading. That
// makes a half-filled group (areas chosen, rights forgotten) silently
// non-functional, so policy.DecideArea reports it as its own reason code —
// `deny_no_area_right`, distinct from `deny_no_access` — and the group form
// pre-selects both rights the moment an area is added. A validator that required
// rights whenever `areas` is non-empty is not expressible as a collection rule,
// and a hook to enforce it would be more machinery than a diagnosable reason code.
//
// Nothing here changes the collection's rules: a group is `policy`-gated exactly as
// before, because choosing who may disarm an area is the same kind of decision as
// choosing who may open a door.
func init() {
	migrations.Register(func(app core.App) error {
		groups, err := app.FindCollectionByNameOrId("access_groups")
		if err != nil {
			return err
		}
		areas, err := app.FindCollectionByNameOrId("areas")
		if err != nil {
			return err
		}
		auxOutput, err := app.FindCollectionByNameOrId("aux_output")
		if err != nil {
			return err
		}

		groups.Fields.Add(&core.RelationField{
			Name:         "areas",
			CollectionId: areas.Id,
			MaxSelect:    9999,
		})
		groups.Fields.Add(&core.RelationField{
			Name:         "aux_outputs",
			CollectionId: auxOutput.Id,
			MaxSelect:    9999,
		})
		// Which arm actions the areas above are granted for. Two values, both
		// selectable; empty grants neither.
		groups.Fields.Add(&core.SelectField{
			Name:      "area_rights",
			Values:    []string{"arm", "disarm"},
			MaxSelect: 2,
		})
		return app.Save(groups)
	}, func(app core.App) error {
		groups, err := app.FindCollectionByNameOrId("access_groups")
		if err != nil {
			return nil // already gone
		}
		groups.Fields.RemoveByName("areas")
		groups.Fields.RemoveByName("aux_outputs")
		groups.Fields.RemoveByName("area_rights")
		return app.Save(groups)
	})
}
