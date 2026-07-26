package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
	"github.com/stone-age-io/access-control/internal/policykv"
)

// Constrains credentials.value to the NATS KV key charset.
//
// The mirror stores a credential at KV key "cred.<value>" using the value
// verbatim, and nats.go rejects a Put whose key leaves its charset. Until now
// nothing validated it — unlike every code field, which goes through
// mirror.validToken. The failure mode was silent and operator-hostile: the record
// saves, the UI lists an active credential, the KV write fails in accessd's log,
// and the credential never reaches any controller. A credential that looks live but
// opens nothing is exactly the kind of thing a PACS must not do quietly.
//
// This is the API-boundary half of the fix, so the operator sees a validation error
// on the field they just typed. mirror.validKey is the paired defense in depth for
// writes that never pass through the API. Both read the charset from
// policykv.CredentialValuePattern so they cannot drift.
//
// Existing rows are NOT rewritten: a field Pattern is enforced on save, so any
// pre-existing malformed value stays until someone edits that record (at which
// point they get a clear error instead of a silent non-sync). Those credentials
// were already failing to mirror before this migration — nothing regresses.
//
// Deliberately paired with, but separate from, 1750000027 (the operator read
// floor): both are prerequisites for the badge tier, but they are different
// concerns and should be revertible independently.
func init() {
	migrations.Register(func(app core.App) error {
		return setCredentialValuePattern(app, policykv.CredentialValuePattern)
	}, func(app core.App) error {
		return setCredentialValuePattern(app, "")
	})
}

// setCredentialValuePattern replaces the credentials.value field, preserving its
// other options, with Pattern set to the given regexp ("" = unconstrained).
func setCredentialValuePattern(app core.App, pattern string) error {
	c, err := app.FindCollectionByNameOrId("credentials")
	if err != nil {
		return err
	}
	f, ok := c.Fields.GetByName("value").(*core.TextField)
	if !ok || f == nil {
		// Schema drifted out from under us — leave it alone rather than replacing a
		// field whose shape we no longer recognize.
		return nil
	}
	f.Pattern = pattern
	c.Fields.Add(f) // Add replaces in place when the name already exists
	return app.Save(c)
}
