package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Adds location map/visualization fields, used only by the management UI:
//   - locations.description : free-form text
//   - locations.coordinates : geoPoint {lat, lon} for the geographic map
//   - locations.floorplan   : an uploaded floor-plan image (single file)
//   - portals.floorplan_position : {x, y} pixel coords on the location's floorplan
//
// These are pure control-plane visualization. None of them is mirrored to NATS
// KV — the internal/policykv wire contract is explicit, so new PB fields don't
// propagate — which keeps policy.Decide and the edge controller untouched and
// means no image data ever reaches a leaf node.
func init() {
	migrations.Register(func(app core.App) error {
		locations, err := app.FindCollectionByNameOrId("locations")
		if err != nil {
			return err
		}
		locations.Fields.Add(&core.TextField{Name: "description"})
		locations.Fields.Add(&core.GeoPointField{Name: "coordinates"})
		locations.Fields.Add(&core.FileField{
			Name:      "floorplan",
			MaxSelect: 1,
			MaxSize:   10 << 20, // 10 MiB
			MimeTypes: []string{"image/png", "image/jpeg", "image/webp", "image/svg+xml"},
		})
		if err := app.Save(locations); err != nil {
			return err
		}

		portals, err := app.FindCollectionByNameOrId("portals")
		if err != nil {
			return err
		}
		// {x, y} pixel coordinates on the location's floorplan image (CRS.Simple,
		// 1 unit = 1 image pixel); null/absent means the portal isn't placed.
		portals.Fields.Add(&core.JSONField{Name: "floorplan_position", MaxSize: 1 << 12})
		return app.Save(portals)
	}, func(app core.App) error {
		if locations, err := app.FindCollectionByNameOrId("locations"); err == nil {
			locations.Fields.RemoveByName("description")
			locations.Fields.RemoveByName("coordinates")
			locations.Fields.RemoveByName("floorplan")
			if err := app.Save(locations); err != nil {
				return err
			}
		}
		if portals, err := app.FindCollectionByNameOrId("portals"); err == nil {
			portals.Fields.RemoveByName("floorplan_position")
			return app.Save(portals)
		}
		return nil
	})
}
