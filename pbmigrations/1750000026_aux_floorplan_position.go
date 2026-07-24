package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Adds floorplan_position to aux_input and aux_output, so aux I/O can be placed
// and monitored on a location's floor plan exactly like portals (migration
// 1750000011 added the same field to portals).
//
// Like portals.floorplan_position, this is pure control-plane UI visualization:
// {x, y} pixel coordinates on the location's floorplan image (CRS.Simple, 1 unit
// = 1 image pixel); null/absent means the point isn't placed. It is NOT mirrored
// to NATS KV — the internal/policykv wire contract is explicit — so it never
// reaches a leaf node and policy.Decide stays untouched.
func init() {
	migrations.Register(func(app core.App) error {
		for _, name := range []string{"aux_input", "aux_output"} {
			c, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				return err
			}
			c.Fields.Add(&core.JSONField{Name: "floorplan_position", MaxSize: 1 << 12})
			if err := app.Save(c); err != nil {
				return err
			}
		}
		return nil
	}, func(app core.App) error {
		for _, name := range []string{"aux_input", "aux_output"} {
			if c, err := app.FindCollectionByNameOrId(name); err == nil {
				c.Fields.RemoveByName("floorplan_position")
				if err := app.Save(c); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
