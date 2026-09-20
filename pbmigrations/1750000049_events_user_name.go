package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Adds events.user_name: the cardholder's display name at the moment the event
// happened, resolved by the audit consumer when it projects the message.
//
// events.user has always held the PocketBase record id, and that is correct —
// policykv.User is {id, status, roles} and carries no name, so the edge has no
// name to emit, and the id is the stable join key an audit row should keep. The
// bug was only that the console rendered that id at the operator, so "who was
// turned away" read as a 15-character token. It stayed hidden because the demo
// seed wrote human names into `user` instead of ids, so seeded history looked
// right and live events did not.
//
// The name is resolved at PROJECTION time rather than at display time, and that
// is the whole point of putting it in a column:
//
//   - An audit row should say who this WAS, not who that id is called today. A
//     rename after the fact must not rewrite history.
//   - A cardholder can be deleted. Resolving in the UI means "who was denied"
//     silently goes blank exactly when someone is asking the question.
//   - One writer, rather than every view that shows an event agreeing on how to
//     look a person up.
//
// `user` keeps the id, so nothing that joins on it changes. A row whose `user`
// does not resolve — a legacy or seeded row carrying a name, an operator command
// whose actor is not a cardholder, an unknown-credential tap with no user at all
// — simply gets an empty user_name, and the UI falls back to `user`. That
// fallback is also what makes this migration additive: existing rows read
// exactly as they do now until a projection rebuild fills them in.
func init() {
	migrations.Register(func(app core.App) error {
		events, err := app.FindCollectionByNameOrId("events")
		if err != nil {
			return err
		}
		events.Fields.Add(&core.TextField{Name: "user_name"})
		return app.Save(events)
	}, func(app core.App) error {
		events, err := app.FindCollectionByNameOrId("events")
		if err != nil {
			return nil // already gone
		}
		events.Fields.RemoveByName("user_name")
		return app.Save(events)
	})
}
