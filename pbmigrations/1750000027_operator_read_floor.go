package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/tools/types"
)

// Scopes the control-plane READ floor to the operator auth collection.
//
// Migrations 1750000009/1750000016 set every readable control-plane collection's
// list/view rule to `@request.auth.id != ""` — "any authenticated request". That
// was exactly right while `users` was the only auth collection, but the rule is
// auth-collection-AGNOSTIC: it is satisfied by an authenticated record from ANY
// auth collection. The moment a second auth tier exists (badge holders and
// visitors, migration 1750000030), every one of them would inherit operator read
// on the whole policy graph — including `credentials`, whose `value` field is the
// credential secret in plaintext. A lobby visitor could enumerate every card in
// the building.
//
// The floor becomes `@request.auth.collectionName = "users"`. Semantics for
// operators are unchanged (read stays a universal floor for any authenticated
// operator — it is deliberately NOT capability-gated; see docs/operators.md), but
// it is now bounded to the operator tier. Superusers bypass rules regardless.
//
// Write rules are untouched: they are already capability-gated
// (`@request.auth.permissions ~ "x"`), and a non-operator auth record has no
// `permissions` field, so they fail closed on their own.
//
// The custom-route half of this same widening is fixed in Go, not here:
// /api/simulate and /api/models bound bare apis.RequireAuth() (any collection) and
// now bind apis.RequireAuth("users"). /api/simulate mattered most — it is a
// decision oracle over the entire policy graph.
func init() {
	migrations.Register(func(app core.App) error {
		return setReadFloor(app, types.Pointer(operatorReadFloor))
	}, func(app core.App) error {
		return setReadFloor(app, types.Pointer(`@request.auth.id != ""`))
	})
}

// operatorReadFloor admits any authenticated record in the `users` (operator)
// collection. Compared by collection NAME because that is what the rule resolver
// exposes on @request.auth and what stays readable in the dashboard.
const operatorReadFloor = `@request.auth.collectionName = "users"`

// readFloorCollections is every collection that carried the agnostic
// `@request.auth.id != ""` read rule, gathered from the migrations that set it:
// 1750000009 (the original tier), 1750000016 (capabilities), 1750000018
// (holiday_calendars), 1750000019 (areas).
//
// Deliberately an explicit list rather than "every collection": `users`
// (self-or-`operators`) and `audit_logs` (`operators`) have TIGHTER floors, and
// sweeping them into this loop would widen them to all operators — the opposite of
// this migration's intent.
var readFloorCollections = []string{
	// people
	"cardholders", "credentials",
	// access logic
	"schedules", "access_groups", "roles", "holidays", "holiday_calendars",
	// hardware / topology
	"locations", "controllers", "portals", "aux_input", "aux_output", "areas",
	// machine-written projections (read-only surfaces for the UI)
	"events", "point_status",
}

// setReadFloor applies rule to the list+view rules of every collection in
// readFloorCollections. A collection that does not exist is skipped rather than
// failing the migration: there is nothing to tighten, and the down-migrations in
// this package take the same stance.
func setReadFloor(app core.App, rule *string) error {
	for _, name := range readFloorCollections {
		c, err := app.FindCollectionByNameOrId(name)
		if err != nil {
			continue
		}
		c.ListRule, c.ViewRule = rule, rule
		if err := app.Save(c); err != nil {
			return err
		}
	}
	return nil
}
