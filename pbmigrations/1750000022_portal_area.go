package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Portals as area members (intrusion-lite, second cut).
//
// A portal can now join an `area` exactly as an aux_input does (1750000019). This
// makes a full door (reader/DPS/REX) a monitored point of the area: while the area
// is armed, the portal's *forced* condition — a DPS open with no authorizing
// grant/REX, which the door state machine already detects — escalates to an area
// intrusion alarm. A bare contact (aux_input typed `intrusion`) trips on any open
// while armed; a portal has a reader, so an authorized open is normal passage and
// only an unauthorized one is intrusion. Membership is consumed only by the
// controller runtime, never by policy.Decide.
//
// `disarm_on_grant` makes the portal an *entry* door: a valid credential grant
// here durably disarms the portal's area (accessd's disarm sink writes
// arm_override, mirrored to KV, converging every peer controller). It is
// orthogonal to membership — a member door without the flag is a monitored door
// (forced-while-armed = intrusion) whose valid badges simply pass.
func init() {
	migrations.Register(func(app core.App) error {
		areas, err := app.FindCollectionByNameOrId("areas")
		if err != nil {
			return err
		}
		portals, err := app.FindCollectionByNameOrId("portals")
		if err != nil {
			return err
		}
		portals.Fields.Add(&core.RelationField{Name: "area", CollectionId: areas.Id, MaxSelect: 1})
		portals.Fields.Add(&core.BoolField{Name: "disarm_on_grant"})
		return app.Save(portals)
	}, func(app core.App) error {
		portals, err := app.FindCollectionByNameOrId("portals")
		if err != nil {
			return nil
		}
		portals.Fields.RemoveByName("area")
		portals.Fields.RemoveByName("disarm_on_grant")
		return app.Save(portals)
	})
}
