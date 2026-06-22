package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// posture_source records WHY a portal's effective posture is what it is —
// "standing" (the configured posture), "scheduled" (auto_posture while its
// auto_schedule window is open), or "override" (an operator's runtime command).
// The controller resolves it and carries it up the device-shadow channel
// (statuskv.PortalStatus.Source); accessd's status projector writes it here, and
// the UI uses it to flag a manual override distinctly from the door's normal
// state. Additive, like the other point_status fields: empty for aux points and
// for shadows from an older controller (the UI reads empty as "standing").
func init() {
	migrations.Register(func(app core.App) error {
		ps, err := app.FindCollectionByNameOrId("point_status")
		if err != nil {
			return err
		}
		ps.Fields.Add(&core.TextField{Name: "posture_source"})
		return app.Save(ps)
	}, func(app core.App) error {
		ps, err := app.FindCollectionByNameOrId("point_status")
		if err != nil {
			return nil // already gone
		}
		ps.Fields.RemoveByName("posture_source")
		return app.Save(ps)
	})
}
