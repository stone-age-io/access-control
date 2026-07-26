package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Per-location opt-in for showing a floor plan on a badge.
//
// A badge holder's Access tab can render the site's floor plan with their own doors and
// controls pinned on it — genuinely useful for "which door am I at", and a much better
// phone surface than a list of names. But the plan is a picture of the WHOLE building,
// and the items on it are scoped to the badge while the image is not: a contractor with
// one door would learn the internal layout of the site.
//
// So this is opt-in per location, default false. The instinct is the same one behind
// `portals.allow_remote_unlock` (1750000031) and `areas.allow_remote_arm` (1750000039):
// "may walk through this door" and "may see the map of the building it is in" are
// different permissions, and the second is not implied by the first.
//
// When it is off, the badge Access tab is a list — which is a complete surface in its
// own right, not a degraded one.
//
// # What this flag does and does not protect
//
// It decides who is TOLD the floor plan's URL. `locations.floorplan` (1750000011) is an
// ordinary public file field — the operator UI loads it with no file token — so the URL
// is an unguessable capability rather than an authorized resource. Making it protected
// would be a bigger change (the operator map builds its URL directly) and is not what
// this flag is for. Stated here so nobody reads the flag as access control on the image
// itself.
//
// Control-plane only, never mirrored to KV: no image data has ever reached a leaf node
// and none does now.
func init() {
	migrations.Register(func(app core.App) error {
		locations, err := app.FindCollectionByNameOrId("locations")
		if err != nil {
			return err
		}
		locations.Fields.Add(&core.BoolField{Name: "badge_floorplan"})
		if err := app.Save(locations); err != nil {
			return err
		}

		s := app.Settings()
		s.RateLimits.Rules = upsertRateLimitRules(s.RateLimits.Rules, core.RateLimitRule{
			// A page load, like GET /api/badge/me — generous, but not unbounded: it walks
			// the holder's whole grant set and reads a record per placed item.
			Label:       "GET /api/badge/live",
			Audience:    core.RateLimitRuleAudienceAuth,
			MaxRequests: 60,
			Duration:    60,
		})
		return app.Save(s)
	}, func(app core.App) error {
		if locations, err := app.FindCollectionByNameOrId("locations"); err == nil {
			locations.Fields.RemoveByName("badge_floorplan")
			if err := app.Save(locations); err != nil {
				return err
			}
		}
		s := app.Settings()
		s.RateLimits.Rules = removeRateLimitRules(s.RateLimits.Rules, "GET /api/badge/live")
		return app.Save(s)
	})
}
