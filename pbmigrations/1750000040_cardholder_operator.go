package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Link a cardholder to the operator account of the same human.
//
// One person can hold an account in both tiers — the guard who badges in through the
// door and also runs the console, the facilities manager who carries a card. They are
// two records because they are two authorities, and they stay two records. This is only
// a pointer, so an operator can be shown their OWN badge (GET /api/badge/me accepts an
// operator token and resolves through this) instead of having to sign in twice.
//
// # Why the relation lives on `cardholders` and not on `users`
//
// This is the load-bearing decision. `users.UpdateRule` is self (1750000009/1750000016)
// — an operator updates their own record to change their password — so a
// `users.cardholder` field would be SELF-WRITABLE. Any operator, including a read-only
// one, could repoint it at any cardholder and inherit that badge: their doors, and
// remote unlock on them. That is exactly the escalation shape 490cc91 removed from
// `cardholders` by taking the self clause off its write rules.
//
// Putting the pointer on `cardholders` closes it by construction. Writes there are
// `enroll`-gated with no self clause, and anyone who holds `enroll` can already create
// a cardholder and assign it any role — so linking themselves is inside the authority
// they already have, not a way past it. An operator with only `command`, or with no
// capabilities at all, cannot write the field.
//
// It also reads correctly: "this person is also operator X" is a fact about the person,
// and it belongs on the person's record next to their other identity facts.
//
// # Why not join on email
//
// Both collections have a unique email, so `cardholders.email = users.email` would need
// no schema at all. It is a bad idea: anyone with the `operators` capability could set
// their own operator email to a cardholder's and silently inherit that badge. An
// authority link must be an explicit, separately-gated write, never a coincidence of
// two strings.
//
// The unique index is PARTIAL (`WHERE operator != ''`), the same shape PocketBase uses
// for optional email: two cardholders must not claim one operator, but the
// overwhelming majority of cardholders have no operator account and must not collide
// on empty.
func init() {
	migrations.Register(func(app core.App) error {
		cardholders, err := app.FindCollectionByNameOrId("cardholders")
		if err != nil {
			return err
		}
		users, err := app.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}
		cardholders.Fields.Add(&core.RelationField{
			Name:         "operator",
			CollectionId: users.Id,
			MaxSelect:    1,
			// No CascadeDelete: deleting an operator account must not delete the person
			// or their credentials. Losing console access is not losing your badge.
		})
		cardholders.AddIndex("idx_cardholders_operator", true, "operator", "operator != ''")
		return app.Save(cardholders)
	}, func(app core.App) error {
		cardholders, err := app.FindCollectionByNameOrId("cardholders")
		if err != nil {
			return nil
		}
		cardholders.RemoveIndex("idx_cardholders_operator")
		cardholders.Fields.RemoveByName("operator")
		return app.Save(cardholders)
	})
}
