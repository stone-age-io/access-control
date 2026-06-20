package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Auxiliary I/O: named digital inputs and output relays bound to a controller,
// modeled like portals (code + location + controller + a logical line index) but
// without door semantics. Aux outputs are driven by the cmd.output command
// (on/off/pulse); aux inputs are observe-only. Both flow through the same mirror →
// ACC_POLICY → controller arming path as portals, and surface live state through
// ACC_STATUS into point_status. Superuser-only (nil access rules).
//
// A small guarded fixture seeds one of each on the dev controller, augmenting the
// base fixture without touching operator data.
func init() {
	migrations.Register(func(app core.App) error {
		locations, err := app.FindCollectionByNameOrId("locations")
		if err != nil {
			return err
		}
		controllers, err := app.FindCollectionByNameOrId("controllers")
		if err != nil {
			return err
		}

		// --- aux_input: a named observe-only digital input. ---
		auxInput := core.NewBaseCollection("aux_input")
		auxInput.Fields.Add(&core.TextField{Name: "code", Required: true})
		auxInput.Fields.Add(&core.TextField{Name: "name"})
		auxInput.Fields.Add(&core.RelationField{Name: "location", CollectionId: locations.Id, Required: true, MaxSelect: 1})
		auxInput.Fields.Add(&core.RelationField{Name: "controller", CollectionId: controllers.Id, MaxSelect: 1})
		// input_index: logical input index on the box; the model template maps it to a line.
		auxInput.Fields.Add(&core.NumberField{Name: "input_index", OnlyInt: true})
		addTimestamps(auxInput)
		auxInput.AddIndex("idx_aux_input_code", true, "code", "")
		if err := app.Save(auxInput); err != nil {
			return err
		}

		// --- aux_output: a named relay driven by cmd.output. ---
		auxOutput := core.NewBaseCollection("aux_output")
		auxOutput.Fields.Add(&core.TextField{Name: "code", Required: true})
		auxOutput.Fields.Add(&core.TextField{Name: "name"})
		auxOutput.Fields.Add(&core.RelationField{Name: "location", CollectionId: locations.Id, Required: true, MaxSelect: 1})
		auxOutput.Fields.Add(&core.RelationField{Name: "controller", CollectionId: controllers.Id, MaxSelect: 1})
		auxOutput.Fields.Add(&core.NumberField{Name: "relay_index", OnlyInt: true})
		// pulse_seconds: default momentary-pulse duration for the "pulse" action.
		auxOutput.Fields.Add(&core.NumberField{Name: "pulse_seconds", OnlyInt: true})
		addTimestamps(auxOutput)
		auxOutput.AddIndex("idx_aux_output_code", true, "code", "")
		if err := app.Save(auxOutput); err != nil {
			return err
		}

		// --- guarded fixture: one aux input + one aux output on the dev box. ---
		hq, err := app.FindFirstRecordByData("locations", "code", "hq")
		if err != nil {
			return nil // base fixture didn't run (operator data) — leave the schema only
		}
		if _, err := app.FindFirstRecordByData("aux_output", "code", "gate-strike"); err == nil {
			return nil // already seeded
		}
		controller, err := app.FindFirstRecordByData("controllers", "code", "ctrl-hq-1")
		if err != nil {
			return nil
		}
		save := func(collection string, set map[string]any) error {
			c, err := app.FindCollectionByNameOrId(collection)
			if err != nil {
				return err
			}
			rec := core.NewRecord(c)
			for k, v := range set {
				rec.Set(k, v)
			}
			return app.Save(rec)
		}
		if err := save("aux_input", map[string]any{
			"code": "dock-contact", "name": "Loading Dock Contact",
			"location": hq.Id, "controller": controller.Id, "input_index": 5,
		}); err != nil {
			return err
		}
		return save("aux_output", map[string]any{
			"code": "gate-strike", "name": "Parking Gate Strike",
			"location": hq.Id, "controller": controller.Id, "relay_index": 5, "pulse_seconds": 4,
		})
	}, func(app core.App) error {
		// Down: drop fixture rows, then the collections.
		for _, r := range []struct{ col, code string }{
			{"aux_output", "gate-strike"}, {"aux_input", "dock-contact"},
		} {
			if rec, err := app.FindFirstRecordByData(r.col, "code", r.code); err == nil {
				_ = app.Delete(rec)
			}
		}
		for _, name := range []string{"aux_output", "aux_input"} {
			c, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				continue
			}
			if err := app.Delete(c); err != nil {
				return err
			}
		}
		return nil
	})
}
