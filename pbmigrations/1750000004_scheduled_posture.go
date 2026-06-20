package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Adds scheduled posture to portals: while auto_schedule's window is open, the
// controller adopts auto_posture instead of the standing posture (a runtime
// command still overrides both). Both fields are set together or not at all —
// the mirror drops a half-configured pair. This is the general mechanism behind
// "auto-unlock during business hours" (auto_posture = unlocked) and also covers,
// e.g., a wing that auto-locks-down overnight.
func init() {
	migrations.Register(func(app core.App) error {
		portals, err := app.FindCollectionByNameOrId("portals")
		if err != nil {
			return err
		}
		schedules, err := app.FindCollectionByNameOrId("schedules")
		if err != nil {
			return err
		}
		portals.Fields.Add(&core.SelectField{
			Name:      "auto_posture",
			Values:    []string{"secure", "free_access", "unlocked", "lockdown", "disabled"},
			MaxSelect: 1,
		})
		portals.Fields.Add(&core.RelationField{
			Name:         "auto_schedule",
			CollectionId: schedules.Id,
			MaxSelect:    1,
		})
		return app.Save(portals)
	}, func(app core.App) error {
		portals, err := app.FindCollectionByNameOrId("portals")
		if err != nil {
			return nil // already gone
		}
		portals.Fields.RemoveByName("auto_posture")
		portals.Fields.RemoveByName("auto_schedule")
		return app.Save(portals)
	})
}
