package pbmigrations

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Makes `credentials.user` cascade, so deleting a person deletes their cards.
//
// # The state this removes
//
// `credentials.user` is a REQUIRED relation, and PocketBase refuses to delete a record
// that is the target of one:
//
//	the record cannot be deleted because it is part of a required reference in
//	record <id> (credentials collection)
//
// So before this migration a cardholder holding any credential — active, revoked, it
// makes no difference — could not be deleted at all. The Cardholders page's delete
// button failed on every person who had ever been issued a card, which is all of them.
// Ending a visit was only possible because a visitor's LOGIN was a separate record
// that could be deleted instead, and that shortcut is gone now that the login and the
// person are one row (1750000000).
//
// # Why cascade rather than blocking
//
// A credential is an opaque string that means "the bearer is this person". With the
// person deleted it does not become an orphan record to be tidied up later — it
// becomes a key that opens doors and resolves to nobody, which is strictly worse than
// either keeping both or deleting both. Deleting both is what an operator pressing
// "delete this person" means.
//
// It is safe for the audit trail: `events` and `audit_logs` store denormalized text
// (the credential value and cardholder id as strings), not relations, so history
// survives the records it describes. That is the same property that lets the events
// projection be rebuilt from JetStream.
//
// It is safe at the edge: PocketBase's cascade calls app.Delete on each referencing
// record (core.deleteRefRecords), so the ordinary delete hooks fire and
// internal/mirror prunes every `cred.{value}` key from ACC_POLICY. A cascade does not
// bypass the mirror, so it cannot leave a working key in KV.
//
// Retention is unaffected: nothing deletes a person automatically. The visitor sweep
// (internal/badgesweep) revokes expired credentials and deliberately deletes nothing —
// how long to keep a record of who visited belongs to the install, not to a
// background job. This migration only makes the operator's explicit delete work.
func init() {
	migrations.Register(func(app core.App) error {
		return setCredentialUserCascade(app, true)
	}, func(app core.App) error {
		return setCredentialUserCascade(app, false)
	})
}

func setCredentialUserCascade(app core.App, cascade bool) error {
	c, err := app.FindCollectionByNameOrId("credentials")
	if err != nil {
		return err
	}
	rel, ok := c.Fields.GetByName("user").(*core.RelationField)
	if !ok {
		return errors.New("credentials.user is not a relation field")
	}
	rel.CascadeDelete = cascade
	return app.Save(c)
}
