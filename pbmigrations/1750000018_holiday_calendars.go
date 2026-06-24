package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/tools/types"
)

// Repoints holidays from per-location rows onto shareable holiday_calendars, so a
// single "Christmas" can be observed by many sites instead of duplicated per site.
//
// Model:
//   - holiday_calendars: a named, shareable set of dates ({code, name}).
//   - holidays.calendar: a holiday belongs to ONE calendar (replaces .location).
//   - locations.holiday_calendars: a site observes a SET of calendars (M:N).
//
// The controller unions a location's observed calendars into its HolidaySet (see
// PolicyStore.rebuildHolidays); the pure policy.Decide is untouched, and the
// controller wire (map[location]HolidaySet) is unchanged. holiday_calendars is
// deliberately NOT mirrored to KV — a calendar is just a grouping label, and both
// holidays and locations already carry its code, so the controller never needs the
// calendar record itself.
//
// Data migration preserves current behavior exactly: one calendar per existing
// location ("<code>-holidays"), the location's holidays moved onto it, and the
// location linked to it. Operators consolidate onto shared calendars afterward.
func init() {
	migrations.Register(func(app core.App) error {
		anyAuth := types.Pointer(`@request.auth.id != ""`)
		pPolicy := types.Pointer(`@request.auth.permissions ~ "policy"`)

		// --- holiday_calendars: same control-plane gate as holidays (read = any
		// authenticated operator; write = the `policy` capability). ---
		calendars := core.NewBaseCollection("holiday_calendars")
		calendars.Fields.Add(&core.TextField{Name: "code", Required: true})
		calendars.Fields.Add(&core.TextField{Name: "name"})
		calendars.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
		calendars.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})
		calendars.AddIndex("idx_holiday_calendars_code", true, "code", "")
		calendars.ListRule, calendars.ViewRule = anyAuth, anyAuth
		calendars.CreateRule, calendars.UpdateRule, calendars.DeleteRule = pPolicy, pPolicy, pPolicy
		if err := app.Save(calendars); err != nil {
			return err
		}

		// --- locations.holiday_calendars (M:N), mirroring the access_groups.portals
		// multi-relation convention. ---
		locations, err := app.FindCollectionByNameOrId("locations")
		if err != nil {
			return err
		}
		locations.Fields.Add(&core.RelationField{
			Name:         "holiday_calendars",
			CollectionId: calendars.Id,
			MaxSelect:    9999,
		})
		if err := app.Save(locations); err != nil {
			return err
		}

		// --- holidays.calendar (replaces .location). Required: every holiday belongs
		// to a calendar. Existing rows are backfilled below before the location field
		// is dropped. ---
		holidays, err := app.FindCollectionByNameOrId("holidays")
		if err != nil {
			return err
		}
		holidays.Fields.Add(&core.RelationField{
			Name:         "calendar",
			CollectionId: calendars.Id,
			Required:     true,
			MaxSelect:    1,
		})
		if err := app.Save(holidays); err != nil {
			return err
		}

		// --- data migration: one calendar per location, carry its holidays + link it. ---
		locs, err := app.FindAllRecords("locations")
		if err != nil {
			return err
		}
		calByLoc := make(map[string]*core.Record, len(locs))
		for _, loc := range locs {
			cal := core.NewRecord(calendars)
			cal.Set("code", loc.GetString("code")+"-holidays")
			cal.Set("name", defaultName(loc.GetString("name"), loc.GetString("code"))+" holidays")
			if err := app.Save(cal); err != nil {
				return err
			}
			calByLoc[loc.Id] = cal
			loc.Set("holiday_calendars", []string{cal.Id})
			if err := app.Save(loc); err != nil {
				return err
			}
		}
		hols, err := app.FindAllRecords("holidays")
		if err != nil {
			return err
		}
		for _, h := range hols {
			cal := calByLoc[h.GetString("location")]
			if cal == nil {
				continue // dangling location → leave calendar unset (fail-safe: closes nothing)
			}
			h.Set("calendar", cal.Id)
			if err := app.Save(h); err != nil {
				return err
			}
		}

		// --- drop holidays.location + its index; calendar is the single source now. ---
		holidays.RemoveIndex("idx_holidays_location")
		holidays.Fields.RemoveByName("location")
		holidays.AddIndex("idx_holidays_calendar", false, "calendar", "")
		return app.Save(holidays)
	}, func(app core.App) error {
		// Down (best-effort): M:N can't perfectly round-trip a holiday observed by
		// multiple sites, so a holiday is re-homed to the first location observing its
		// calendar. Restores the holidays.location relation, drops the calendar wiring,
		// and deletes the holiday_calendars collection.
		holidays, err := app.FindCollectionByNameOrId("holidays")
		if err != nil {
			return err
		}
		locations, err := app.FindCollectionByNameOrId("locations")
		if err != nil {
			return err
		}

		// Reverse map: calendar id -> first location id observing it.
		locByCal := map[string]string{}
		locs, err := app.FindAllRecords("locations")
		if err != nil {
			return err
		}
		for _, loc := range locs {
			for _, calID := range loc.GetStringSlice("holiday_calendars") {
				if _, seen := locByCal[calID]; !seen {
					locByCal[calID] = loc.Id
				}
			}
		}

		// Re-add holidays.location (not required on the down path: an orphaned holiday
		// whose calendar no location observes simply gets no location).
		holidays.RemoveIndex("idx_holidays_calendar")
		holidays.Fields.Add(&core.RelationField{
			Name:         "location",
			CollectionId: locations.Id,
			MaxSelect:    1,
		})
		if err := app.Save(holidays); err != nil {
			return err
		}
		hols, err := app.FindAllRecords("holidays")
		if err != nil {
			return err
		}
		for _, h := range hols {
			if locID := locByCal[h.GetString("calendar")]; locID != "" {
				h.Set("location", locID)
				if err := app.Save(h); err != nil {
					return err
				}
			}
		}
		holidays.Fields.RemoveByName("calendar")
		holidays.AddIndex("idx_holidays_location", false, "location", "")
		if err := app.Save(holidays); err != nil {
			return err
		}

		locations.Fields.RemoveByName("holiday_calendars")
		if err := app.Save(locations); err != nil {
			return err
		}
		if calendars, err := app.FindCollectionByNameOrId("holiday_calendars"); err == nil {
			return app.Delete(calendars)
		}
		return nil
	})
}

// defaultName returns name if non-empty, else the fallback (a location's code), so
// a calendar derived from a code-only location still reads sensibly.
func defaultName(name, fallback string) string {
	if name != "" {
		return name
	}
	return fallback
}
