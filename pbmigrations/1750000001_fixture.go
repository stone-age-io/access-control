package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Seeds a minimal end-to-end fixture so a fresh accessd boot has something to
// mirror and decide against:
//
//	location hq (America/New_York) → portal lobby-main (door, secure, pulse 5s)
//	schedule business-hours (M–F 08:00–17:00) → access group lobby-group
//	role staff → user alice → credential CARD-001
//
// Idempotent and safe in any environment: it no-ops if the locations collection
// is already populated, so it only ever touches a fresh dev database.
//
// Holidays, credential validity dates, and scheduled posture (auto_posture /
// auto_schedule) are seeded by 1750000005_fixture_extras.go instead — they live
// on collections/fields created by later migrations, so they cannot be seeded
// here without breaking migration ordering.
func init() {
	migrations.Register(func(app core.App) error {
		existing, err := app.FindAllRecords("locations")
		if err != nil {
			return err
		}
		if len(existing) > 0 {
			return nil // already seeded (or operator data present) — do nothing
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
			if err := app.Save(rec); err != nil {
				return nil, err
			}
			return rec, nil
		}

		location, err := save("locations", map[string]any{
			"code": "hq", "name": "Headquarters",
			"timezone": "America/New_York", "fai_suppress": true,
		})
		if err != nil {
			return err
		}

		schedule, err := save("schedules", map[string]any{
			"code": "business-hours", "name": "Business Hours (M–F 08:00–17:00)",
			"windows": []map[string]any{
				{"days": []int{1, 2, 3, 4, 5}, "start": "08:00", "end": "17:00"},
			},
		})
		if err != nil {
			return err
		}

		controller, err := save("controllers", map[string]any{
			"code": "ctrl-hq-1", "name": "HQ Controller 1",
			"location": location.Id, "model": "kincony-server-mini",
		})
		if err != nil {
			return err
		}

		portal, err := save("portals", map[string]any{
			"code": "lobby-main", "type": "door", "location": location.Id,
			"name": "Lobby Main Entrance", "posture": "secure", "pulse_seconds": 5,
			"controller": controller.Id,
			"lock_relay": 1, "dps_input": 1, "rex_input": 2, "held_open_seconds": 30,
		})
		if err != nil {
			return err
		}

		group, err := save("access_groups", map[string]any{
			"code": "lobby-group", "name": "Lobby Access",
			"portals": []string{portal.Id}, "schedule": schedule.Id,
		})
		if err != nil {
			return err
		}

		role, err := save("roles", map[string]any{
			"code": "staff", "name": "Staff",
			"access_groups": []string{group.Id},
		})
		if err != nil {
			return err
		}

		user, err := save("cardholders", map[string]any{
			"external_id": "alice", "name": "Alice Example",
			"email": "alice@example.com", "status": "active",
			"roles": []string{role.Id},
		})
		if err != nil {
			return err
		}

		_, err = save("credentials", map[string]any{
			"value": "CARD-001", "type": "wiegand", "user": user.Id,
			"status": "active", "label": "Alice badge",
		})
		return err
	}, func(app core.App) error {
		// Down: remove the fixture rows by their natural keys (leave operator data).
		removeByFilter := func(collection, field, value string) error {
			rec, err := app.FindFirstRecordByData(collection, field, value)
			if err != nil {
				return nil // not found — nothing to undo
			}
			return app.Delete(rec)
		}
		_ = removeByFilter("credentials", "value", "CARD-001")
		_ = removeByFilter("cardholders", "external_id", "alice")
		_ = removeByFilter("roles", "code", "staff")
		_ = removeByFilter("access_groups", "code", "lobby-group")
		_ = removeByFilter("portals", "code", "lobby-main")
		_ = removeByFilter("controllers", "code", "ctrl-hq-1")
		_ = removeByFilter("schedules", "code", "business-hours")
		_ = removeByFilter("locations", "code", "hq")
		return nil
	})
}
