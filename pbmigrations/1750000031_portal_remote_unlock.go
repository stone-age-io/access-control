package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Adds portals.allow_remote_unlock — the per-door opt-in for badge-holder remote
// unlock (internal/badgeapi).
//
// Remote unlock is authorized by policy.Decide against the holder's own credential,
// so it can never exceed what their badge opens in person. That is the right floor,
// but it is not sufficient: "this person may walk through this door" and "this person
// may open this door from anywhere, with no presence proof" are genuinely different
// permissions. A server-room door can legitimately be on someone's badge while being
// something nobody should open from a phone.
//
// Default FALSE, so remote access is an explicit per-door act rather than something
// an install inherits by upgrading. Existing portals stay closed to it.
//
// NOT mirrored to NATS KV, and deliberately so: the check runs centrally in accessd
// before it publishes cmd.grant, never at the edge. From the controller's point of
// view a badge remote unlock is an ordinary operator grant — same subject, same
// physical effect — so the edge needs no new field, no new code path, and no new
// failure mode. Same shape as the floorplan fields in 1750000026.
func init() {
	migrations.Register(func(app core.App) error {
		c, err := app.FindCollectionByNameOrId("portals")
		if err != nil {
			return err
		}
		c.Fields.Add(&core.BoolField{Name: "allow_remote_unlock"})
		return app.Save(c)
	}, func(app core.App) error {
		c, err := app.FindCollectionByNameOrId("portals")
		if err != nil {
			return nil // already gone
		}
		c.Fields.RemoveByName("allow_remote_unlock")
		return app.Save(c)
	})
}
