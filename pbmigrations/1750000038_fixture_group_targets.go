package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Guarded dev fixture for the widened access group: grant the demo area and a new
// aux output through the existing `lobby-group`, so a fresh checkout can exercise
// badge arm/disarm and output control without hand-building a graph.
//
// Rights are arm+disarm, which is the common case; the interesting asymmetry
// (arm-only closing staff) is left for an operator to configure, since a fixture that
// demonstrated it would just look like a bug in the demo.
//
// Runs after 1750000037 (the fields) and 1750000021 (the demo area). No-op when
// either is absent or this has already been seeded — the same pattern as every other
// guarded fixture here.
func init() {
	migrations.Register(func(app core.App) error {
		group, err := app.FindFirstRecordByData("access_groups", "code", "lobby-group")
		if err != nil {
			return nil // base fixture absent — schema only
		}
		area, err := app.FindFirstRecordByData("areas", "code", "warehouse")
		if err != nil {
			return nil // 1750000021's area fixture didn't run
		}
		if len(group.GetStringSlice("areas")) > 0 {
			return nil // already seeded
		}
		hq, err := app.FindFirstRecordByData("locations", "code", "hq")
		if err != nil {
			return nil
		}
		controller, err := app.FindFirstRecordByData("controllers", "code", "ctrl-hq-1")
		if err != nil {
			return nil
		}

		// A named relay a badge holder can plausibly be trusted with: the thing that
		// lets a delivery through, not something that bypasses a door.
		output, err := app.FindFirstRecordByData("aux_output", "code", "lobby-gate")
		if err != nil {
			c, err := app.FindCollectionByNameOrId("aux_output")
			if err != nil {
				return err
			}
			output = core.NewRecord(c)
			output.Set("code", "lobby-gate")
			output.Set("name", "Lobby Vehicle Gate")
			output.Set("location", hq.Id)
			output.Set("controller", controller.Id)
			output.Set("relay_index", 4)
			output.Set("pulse_seconds", 5)
			if err := app.Save(output); err != nil {
				return err
			}
		}

		group.Set("areas", []string{area.Id})
		group.Set("aux_outputs", []string{output.Id})
		group.Set("area_rights", []string{"arm", "disarm"})
		return app.Save(group)
	}, func(app core.App) error {
		if group, err := app.FindFirstRecordByData("access_groups", "code", "lobby-group"); err == nil {
			group.Set("areas", nil)
			group.Set("aux_outputs", nil)
			group.Set("area_rights", nil)
			if err := app.Save(group); err != nil {
				return err
			}
		}
		if rec, err := app.FindFirstRecordByData("aux_output", "code", "lobby-gate"); err == nil {
			_ = app.Delete(rec)
		}
		return nil
	})
}
