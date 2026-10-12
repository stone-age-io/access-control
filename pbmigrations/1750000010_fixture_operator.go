package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Intentionally empty. This migration used to seed the operator admin@local.dev /
// changeme123 with every capability. Its guard was "the base fixture ran", and
// the base fixture runs on any database whose locations collection is empty,
// so every new install, production included, got a known-password operator.
//
// The first operator now comes from `accessd operator upsert <email> <password>`,
// and `accessd demo-seed --confirm` creates admin@local.dev for the demo. The
// file stays, as a no-op, so a database that already applied it still finds it
// by name. A database seeded before this change keeps its admin@local.dev until
// someone deletes it.
func init() {
	migrations.Register(func(app core.App) error {
		return nil
	}, func(app core.App) error {
		return nil
	})
}
