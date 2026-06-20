package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// point_status is the queryable projection of the ACC_STATUS KV "device shadow":
// the live state of each point the edge drives (door open/closed, effective
// posture, aux I/O). accessd's status projector upserts one row per point keyed
// by `code`; the UI subscribes for realtime. Like the events collection it is a
// rebuildable read model — never mirrored to KV (accessd writes it), and nil
// access rules keep it superuser-only.
func init() {
	migrations.Register(func(app core.App) error {
		ps := core.NewBaseCollection("point_status")
		// key is the ACC_STATUS KV key (e.g. "portal.lobby-main") — globally unique
		// across kinds, so it is the row identity the projector upserts on. code is
		// the bare display code; a portal and an aux may share a code, so code alone
		// is not unique.
		ps.Fields.Add(&core.TextField{Name: "key", Required: true})
		ps.Fields.Add(&core.TextField{Name: "code", Required: true})
		ps.Fields.Add(&core.SelectField{
			Name:      "kind",
			Values:    []string{"portal", "aux_input", "aux_output"},
			MaxSelect: 1,
		})
		// state: the headline live value — door open/closed/unknown for portals;
		// energized/off or active/inactive for aux points (aux-I/O phase).
		ps.Fields.Add(&core.TextField{Name: "state"})
		// posture/held are portal-only; left empty/false for aux points.
		ps.Fields.Add(&core.TextField{Name: "posture"})
		ps.Fields.Add(&core.BoolField{Name: "held"})
		// controller/location are stable codes carried in the shadow (self-contained,
		// no lookup back into the policy graph). Lets the UI slice by box/site.
		ps.Fields.Add(&core.TextField{Name: "controller"})
		ps.Fields.Add(&core.TextField{Name: "location"})
		// changed: the controller-stamped instant of the last change (distinct from
		// the autodate `updated`, which is when accessd wrote the row).
		ps.Fields.Add(&core.DateField{Name: "changed"})
		ps.Fields.Add(&core.JSONField{Name: "payload", MaxSize: 1 << 16})
		addTimestamps(ps)
		ps.AddIndex("idx_point_status_key", true, "key", "")
		return app.Save(ps)
	}, func(app core.App) error {
		c, err := app.FindCollectionByNameOrId("point_status")
		if err != nil {
			return nil // already gone
		}
		return app.Delete(c)
	})
}
