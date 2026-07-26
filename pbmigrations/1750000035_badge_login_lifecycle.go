package pbmigrations

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/tools/types"
)

// Three corrections to the badge login's lifecycle, all of them consequences of the
// same realisation: a badge login is 1:1 with a cardholder and is best treated as a
// PROPERTY of that person rather than as an entity with a life of its own.
//
// # 1. The cardholder relation cascades
//
// 1750000030 left `cardholder` non-cascading, so deleting a cardholder left an orphan
// login behind: it still authenticated, and every badge route then failed to resolve a
// cardholder — a login for a person the system no longer has. Nothing about that state
// is useful, and it is a credential-shaped object surviving the deletion of the thing
// it was about. Cascade makes "this person is gone" mean their login is too.
//
// Combined with badgeapi's visitor-delete hook, a cardholder delete now also revokes a
// visitor's pass on the way out, which is the fail-safe direction.
//
// # 2. Read is the operator floor, as it is everywhere else in the control plane
//
// The list/view rule required `enroll`, which made this the only part of the graph an
// authenticated operator could not read. The consequence was a UI that had to HIDE the
// badge-login field from anyone without `enroll` — a page with a silent hole in it,
// where the operator cannot tell "no login" from "not allowed to look". Read has been
// a universal floor for operators since 1750000016 (and scoped to the `users`
// collection specifically by 1750000027); writes are what capabilities gate.
//
// Safe because a badge_users row holds nothing secret: email, kind, password_set, and
// the cardholder id. The password hash and tokenKey are system fields the API never
// serialises. Contrast `credentials.value`, which is a working key and is exactly why
// 1750000027 pinned the read floor to the operator collection in the first place —
// that pin is preserved here: the badge tier still sees only its own row.
//
// # 3. Issuing and removing a login take the same capability
//
// Delete required `operators` while create required `enroll`, on the reasoning that
// removing a login is account administration. In practice it made the UI express one
// act — "this person can/cannot see their badge" — as two buttons with different
// permissions, which is the kind of asymmetry that exists to serve a screen rather than
// a threat model. It is now `enroll` both ways: whoever may hand out a badge login may
// take it back. `operators` remains what it was, the capability for administering the
// OPERATOR tier, where privilege actually escalates.
func init() {
	migrations.Register(func(app core.App) error {
		c, err := app.FindCollectionByNameOrId("badge_users")
		if err != nil {
			return err
		}

		rel, ok := c.Fields.GetByName("cardholder").(*core.RelationField)
		if !ok {
			// Not reachable with the schema 1750000030 defines; refuse rather than
			// silently skipping, since the whole point is that the field cascades.
			return errors.New("badge_users.cardholder is not a relation field")
		}
		rel.CascadeDelete = true

		selfOrOperator := types.Pointer(`id = @request.auth.id || @request.auth.collectionName = "users"`)
		c.ListRule, c.ViewRule = selfOrOperator, selfOrOperator
		// Update stays narrower than read: the holder's own record, or `enroll`. The
		// field-level guard in internal/badgeapi (protectedBadgeFields) is what keeps
		// "my own record" from meaning "any column on it".
		c.UpdateRule = types.Pointer(`id = @request.auth.id || (` + operatorEnroll + `)`)
		c.DeleteRule = types.Pointer(operatorEnroll)

		return app.Save(c)
	}, func(app core.App) error {
		c, err := app.FindCollectionByNameOrId("badge_users")
		if err != nil {
			return nil // already gone
		}
		if rel, ok := c.Fields.GetByName("cardholder").(*core.RelationField); ok {
			rel.CascadeDelete = false
		}
		self := types.Pointer(`id = @request.auth.id || (` + operatorEnroll + `)`)
		c.ListRule, c.ViewRule, c.UpdateRule = self, self, self
		c.DeleteRule = types.Pointer(operatorOperators)
		return app.Save(c)
	})
}
