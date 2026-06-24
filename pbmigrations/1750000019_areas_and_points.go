package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/tools/types"
)

// Intrusion-lite arming: areas + point typing.
//
// An `area` is a logical, single-location, multi-controller grouping with an
// arm-state. When armed, member aux inputs typed `intrusion` raise alarms; a
// `tamper_24h` input alarms regardless of arm-state; a `monitor` input (the
// default) stays observe-only. Arm-state is resolved like scheduled posture —
// arm_override → auto_arm (while auto_schedule open) → standing arm — but lives in
// DURABLE KV (an arm_override field, mirrored), not a RAM override, so a reboot
// can't silently disarm. Fail-safe direction is the inverse of access: an
// unresolved configuration falls back to standing (default disarmed); it never
// spuriously arms.
//
// Area *config* is `topology`-gated (membership/schedules), matching aux_io; the
// operational arm/disarm is `command`-gated via the custom route, which writes
// arm_override through app.Save (bypassing these rules).
func init() {
	migrations.Register(func(app core.App) error {
		locations, err := app.FindCollectionByNameOrId("locations")
		if err != nil {
			return err
		}
		schedules, err := app.FindCollectionByNameOrId("schedules")
		if err != nil {
			return err
		}

		// --- areas: the arm-state grouping. ---
		areas := core.NewBaseCollection("areas")
		areas.Fields.Add(&core.TextField{Name: "code", Required: true})
		areas.Fields.Add(&core.TextField{Name: "name"})
		areas.Fields.Add(&core.RelationField{Name: "location", CollectionId: locations.Id, Required: true, MaxSelect: 1})
		// arm: the STANDING floor. Empty ⇒ disarmed.
		areas.Fields.Add(&core.SelectField{Name: "arm", Values: []string{"disarmed", "armed"}, MaxSelect: 1})
		// arm_override: the durable operator override (empty ⇒ none). Written by the
		// arm/disarm route, mirrored to KV; this is what makes arming reboot-safe.
		areas.Fields.Add(&core.SelectField{Name: "arm_override", Values: []string{"armed", "disarmed"}, MaxSelect: 1})
		// auto_arm + auto_schedule: scheduled arm-state, both-or-neither (the mirror
		// drops a half-configured pair), cloning portals.auto_posture/auto_schedule.
		areas.Fields.Add(&core.SelectField{Name: "auto_arm", Values: []string{"disarmed", "armed"}, MaxSelect: 1})
		areas.Fields.Add(&core.RelationField{Name: "auto_schedule", CollectionId: schedules.Id, MaxSelect: 1})
		addTimestamps(areas)
		areas.AddIndex("idx_areas_code", true, "code", "")

		anyAuth := types.Pointer(`@request.auth.id != ""`)
		pTopology := types.Pointer(`@request.auth.permissions ~ "topology"`)
		pOps := types.Pointer(`@request.auth.permissions ~ "operators"`)
		areas.ListRule, areas.ViewRule = anyAuth, anyAuth
		areas.CreateRule, areas.UpdateRule, areas.DeleteRule = pTopology, pTopology, pOps
		if err := app.Save(areas); err != nil {
			return err
		}

		// --- aux_input: membership (area) + point typing. ---
		auxInput, err := app.FindCollectionByNameOrId("aux_input")
		if err != nil {
			return err
		}
		auxInput.Fields.Add(&core.RelationField{Name: "area", CollectionId: areas.Id, MaxSelect: 1})
		auxInput.Fields.Add(&core.SelectField{
			Name:      "point_type",
			Values:    []string{"monitor", "intrusion", "tamper_24h"},
			MaxSelect: 1,
		})
		if err := app.Save(auxInput); err != nil {
			return err
		}

		// --- point_status.kind: allow the area arm-shadow rows. ---
		return addPointStatusKind(app, "area")
	}, func(app core.App) error {
		// Down: drop the aux_input additions, revert the kind select, delete areas.
		if auxInput, err := app.FindCollectionByNameOrId("aux_input"); err == nil {
			auxInput.Fields.RemoveByName("area")
			auxInput.Fields.RemoveByName("point_type")
			if err := app.Save(auxInput); err != nil {
				return err
			}
		}
		_ = removePointStatusKind(app, "area")
		if c, err := app.FindCollectionByNameOrId("areas"); err == nil {
			if err := app.Delete(c); err != nil {
				return err
			}
		}
		return nil
	})
}

// addPointStatusKind appends a value to point_status.kind's select if absent.
func addPointStatusKind(app core.App, value string) error {
	c, err := app.FindCollectionByNameOrId("point_status")
	if err != nil {
		return err
	}
	f, ok := c.Fields.GetByName("kind").(*core.SelectField)
	if !ok {
		return nil
	}
	for _, v := range f.Values {
		if v == value {
			return nil // already present
		}
	}
	f.Values = append(f.Values, value)
	return app.Save(c)
}

// removePointStatusKind removes a value from point_status.kind's select.
func removePointStatusKind(app core.App, value string) error {
	c, err := app.FindCollectionByNameOrId("point_status")
	if err != nil {
		return nil
	}
	f, ok := c.Fields.GetByName("kind").(*core.SelectField)
	if !ok {
		return nil
	}
	out := f.Values[:0]
	for _, v := range f.Values {
		if v != value {
			out = append(out, v)
		}
	}
	f.Values = out
	return app.Save(c)
}
