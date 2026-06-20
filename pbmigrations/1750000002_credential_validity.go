package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Adds optional activation/expiry bounds to credentials. A presentation before
// valid_from or after valid_until denies (see policy.Decide); both empty means
// the credential is unbounded, the previous behavior. The controller parses
// these once on KV apply, so the decision hot path stays parse-free.
func init() {
	migrations.Register(func(app core.App) error {
		credentials, err := app.FindCollectionByNameOrId("credentials")
		if err != nil {
			return err
		}
		credentials.Fields.Add(&core.DateField{Name: "valid_from"})
		credentials.Fields.Add(&core.DateField{Name: "valid_until"})
		return app.Save(credentials)
	}, func(app core.App) error {
		credentials, err := app.FindCollectionByNameOrId("credentials")
		if err != nil {
			return nil // already gone
		}
		credentials.Fields.RemoveByName("valid_from")
		credentials.Fields.RemoveByName("valid_until")
		return app.Save(credentials)
	})
}
