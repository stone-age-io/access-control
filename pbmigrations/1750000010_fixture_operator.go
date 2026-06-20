package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Seeds a dev admin operator so the management UI is usable out of the box after
// the auth tier lands (1750000009). Without it, a fresh database has no
// role=admin user and — since createRule is now admin-only — the first admin
// could only be made by a superuser via the /_/ dashboard.
//
// Guarded like the other dev fixtures: it no-ops unless the base fixture ran
// (location hq exists) and no admin operator already exists, so it never seeds an
// account into a real deployment or duplicates one. Logs in as admin@local.dev
// / changeme123 — change it immediately; this is a dev convenience, not a
// production credential.
func init() {
	migrations.Register(func(app core.App) error {
		if _, err := app.FindFirstRecordByData("locations", "code", "hq"); err != nil {
			return nil // base fixture didn't run (operator data present) — do nothing
		}
		users, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}
		// Any existing admin operator means this DB has been set up — leave it alone.
		if existing, _ := app.FindAllRecords("users"); len(existing) > 0 {
			for _, u := range existing {
				if u.GetString("role") == "admin" {
					return nil
				}
			}
		}

		admin := core.NewRecord(users)
		admin.SetEmail("admin@local.dev")
		admin.SetPassword("changeme123")
		admin.SetVerified(true)
		admin.Set("name", "Dev Admin")
		admin.Set("role", "admin")
		return app.Save(admin)
	}, func(app core.App) error {
		if rec, err := app.FindFirstRecordByData("users", "email", "admin@local.dev"); err == nil {
			_ = app.Delete(rec)
		}
		return nil
	})
}
