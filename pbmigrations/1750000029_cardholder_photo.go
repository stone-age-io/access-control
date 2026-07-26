package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Adds a photo to cardholders: the identification half of a badge, and useful on
// its own at a guard desk or on an alarm/event detail ("who was that credential?").
//
// NOT mirrored to NATS KV. policykv.User is an explicit payload carrying only
// status + roles, so a new PocketBase field is excluded by construction — the
// decision path never sees it and a leaf node never stores a person's face.
//
// Protected: true is deliberate. An unprotected PocketBase file is served from a
// guessable-once-known URL with NO authorization — share the link and the photo is
// public forever. A protected file requires a short-lived file token
// (pb.files.getToken() in the SDK), so access follows the auth session. This is the
// first real PII in the schema and the first field where a leaked URL, not a leaked
// login, is the exposure; the cost is one extra token fetch per page in the UI.
//
// Read access still follows the collection's read floor, which after 1750000027 is
// the operator tier — so every operator can see every photo. That is a deliberate
// choice for a PACS (a guard needs to verify a face) and is documented in
// docs/operators.md rather than being gated by a capability.
func init() {
	migrations.Register(func(app core.App) error {
		c, err := app.FindCollectionByNameOrId("cardholders")
		if err != nil {
			return err
		}
		c.Fields.Add(&core.FileField{
			Name:      "photo",
			MaxSelect: 1,
			MaxSize:   5 << 20, // 5 MiB — forgiving of a straight-from-phone upload
			MimeTypes: []string{"image/jpeg", "image/png", "image/webp"},
			// Square crops: the list avatar and the badge view. Both are generated
			// lazily on first request, so adding sizes costs nothing up front.
			Thumbs:    []string{"100x100", "400x400"},
			Protected: true,
		})
		return app.Save(c)
	}, func(app core.App) error {
		c, err := app.FindCollectionByNameOrId("cardholders")
		if err != nil {
			return nil // already gone
		}
		// Note: dropping the field does not delete already-uploaded files from
		// storage. That is PocketBase's behavior, not something to paper over here —
		// an install reverting this should prune pb_data/storage itself.
		c.Fields.RemoveByName("photo")
		return app.Save(c)
	})
}
