package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Guarded dev fixture for the areas/arming demo: one area (standing disarmed, so
// it raises no alarms until an operator arms it) and one `intrusion` aux input
// wired to the dev controller. Runs after the schema (1750000019) so the fields
// exist. No-op when the base fixture didn't run (operator data) or it's already
// seeded — same pattern as 1750000007's aux-I/O fixture.
func init() {
	migrations.Register(func(app core.App) error {
		hq, err := app.FindFirstRecordByData("locations", "code", "hq")
		if err != nil {
			return nil // base fixture absent — schema only
		}
		if _, err := app.FindFirstRecordByData("areas", "code", "warehouse"); err == nil {
			return nil // already seeded
		}
		controller, err := app.FindFirstRecordByData("controllers", "code", "ctrl-hq-1")
		if err != nil {
			return nil
		}

		save := func(collection string, set map[string]any) (*core.Record, error) {
			c, err := app.FindCollectionByNameOrId(collection)
			if err != nil {
				return nil, err
			}
			rec := core.NewRecord(c)
			for k, v := range set {
				rec.Set(k, v)
			}
			return rec, app.Save(rec)
		}

		area, err := save("areas", map[string]any{
			"code": "warehouse", "name": "Warehouse",
			"location": hq.Id, "arm": "disarmed",
		})
		if err != nil {
			return err
		}
		_, err = save("aux_input", map[string]any{
			"code": "warehouse-motion", "name": "Warehouse Motion",
			"location": hq.Id, "controller": controller.Id, "input_index": 6,
			"area": area.Id, "point_type": "intrusion",
		})
		return err
	}, func(app core.App) error {
		for _, r := range []struct{ col, code string }{
			{"aux_input", "warehouse-motion"}, {"areas", "warehouse"},
		} {
			if rec, err := app.FindFirstRecordByData(r.col, "code", r.code); err == nil {
				_ = app.Delete(rec)
			}
		}
		return nil
	})
}
