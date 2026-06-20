package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Adds holiday calendars. A holiday closes every window of a schedule that
// observes holidays (see policy.windowOpen), for the holiday's location.
//
// The opt-out is stored inverted as schedules.ignore_holidays (a bool defaults
// to false in PocketBase, and we want observe-by-default): the mirror publishes
// ObserveHolidays = !ignore_holidays, so a freshly created schedule — via UI,
// API, CSV, or migration — observes holidays without anyone remembering to set a
// flag. Everything downstream of the wire uses the positive ObserveHolidays.
func init() {
	migrations.Register(func(app core.App) error {
		locations, err := app.FindCollectionByNameOrId("locations")
		if err != nil {
			return err
		}

		// --- holidays: dates a location is closed. Superuser-only (nil rules). ---
		holidays := core.NewBaseCollection("holidays")
		holidays.Fields.Add(&core.RelationField{
			Name:         "location",
			CollectionId: locations.Id,
			Required:     true,
			MaxSelect:    1,
		})
		// date: the local calendar day the site is closed (only the date part is used).
		holidays.Fields.Add(&core.DateField{Name: "date", Required: true})
		holidays.Fields.Add(&core.TextField{Name: "name"})
		// recurring: match this month/day every year (for fixed-date holidays).
		holidays.Fields.Add(&core.BoolField{Name: "recurring"})
		holidays.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
		holidays.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})
		holidays.AddIndex("idx_holidays_location", false, "location", "")
		if err := app.Save(holidays); err != nil {
			return err
		}

		// --- schedules.ignore_holidays (inverted opt-out; default observe) ---
		schedules, err := app.FindCollectionByNameOrId("schedules")
		if err != nil {
			return err
		}
		schedules.Fields.Add(&core.BoolField{Name: "ignore_holidays"})
		return app.Save(schedules)
	}, func(app core.App) error {
		if schedules, err := app.FindCollectionByNameOrId("schedules"); err == nil {
			schedules.Fields.RemoveByName("ignore_holidays")
			if err := app.Save(schedules); err != nil {
				return err
			}
		}
		if holidays, err := app.FindCollectionByNameOrId("holidays"); err == nil {
			return app.Delete(holidays)
		}
		return nil
	})
}
