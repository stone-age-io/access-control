package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Seeds the M1–M3 demonstration data that the base fixture (1750000001) cannot,
// because it lives on collections/fields created by later migrations:
//
//	holiday Christmas (Dec 25, recurring) at hq → closes business-hours that day
//	portal lobby-public (door, auto-unlock during business-hours) → scheduled
//	  posture in action, without disturbing lobby-main's credential-tap demo
//	credential CARD-001 gains an open-ended valid_from (activation date demo)
//
// Guarded so it only augments the base dev fixture: it no-ops unless location hq
// exists (the base fixture ran) and lobby-public is absent (not already seeded),
// so it never touches an operator's own data and is safe to re-run.
func init() {
	migrations.Register(func(app core.App) error {
		hq, err := app.FindFirstRecordByData("locations", "code", "hq")
		if err != nil {
			return nil // base fixture didn't run (operator data present) — do nothing
		}
		if _, err := app.FindFirstRecordByData("portals", "code", "lobby-public"); err == nil {
			return nil // already seeded
		}
		schedule, err := app.FindFirstRecordByData("schedules", "code", "business-hours")
		if err != nil {
			return nil
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

		// A holiday closes any holiday-observing schedule on that day; recurring,
		// so it matches Dec 25 every year.
		if err := save("holidays", map[string]any{
			"location": hq.Id, "name": "Christmas",
			"date": "2026-12-25 00:00:00.000Z", "recurring": true,
		}); err != nil {
			return err
		}

		// A door that simply stands unlocked during business hours (no credential
		// needed). In no access group: auto-unlock is a property of the door,
		// independent of who may badge.
		return save("portals", map[string]any{
			"code": "lobby-public", "type": "door", "location": hq.Id,
			"name": "Public Lobby (auto-unlock)", "posture": "secure", "pulse_seconds": 5,
			"controller": controller.Id, "lock_relay": 2, "dps_input": 3, "rex_input": 4,
			"held_open_seconds": 30,
			"auto_posture":      "unlocked", "auto_schedule": schedule.Id,
		})
	}, func(app core.App) error {
		if rec, err := app.FindFirstRecordByData("portals", "code", "lobby-public"); err == nil {
			_ = app.Delete(rec)
		}
		if rec, err := app.FindFirstRecordByData("holidays", "name", "Christmas"); err == nil {
			_ = app.Delete(rec)
		}
		return nil
	})
}
