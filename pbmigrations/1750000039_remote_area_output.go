package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Per-record opt-ins for the badge tier's two new actions, plus their rate limits.
//
// `access_groups.areas` (1750000037) says a person may arm or disarm an area. It does
// NOT say they may do it from a phone on the other side of town. That is the same
// distinction `portals.allow_remote_unlock` (1750000031) draws for doors — "may walk
// through" and "may open from anywhere with no presence proof" are different
// permissions — and it matters more here, not less: disarming a building remotely
// turns intrusion detection off with nobody present to notice why.
//
// So each target carries its own opt-in, default FALSE:
//
//   - `areas.allow_remote_arm`  — a badge may arm/disarm this area remotely
//   - `aux_output.allow_remote` — a badge may drive this relay remotely
//
// Without them, an operator who granted an area to a group so that a keypad at the
// door would work later would have silently enabled remote disarm for everyone in
// that group. The flag makes enabling it a deliberate, per-area act.
//
// Both are CONTROL-PLANE ONLY and deliberately not mirrored to KV: they gate a
// central route, and the edge has no remote-actuation path to gate. Same as
// allow_remote_unlock.
//
// Neither flag can widen anything on its own — policy.DecideArea / DecideOutput still
// have to grant the action, over a live snapshot of the mirrored graph. The flag is a
// second gate, never a substitute for the first.
func init() {
	migrations.Register(func(app core.App) error {
		areas, err := app.FindCollectionByNameOrId("areas")
		if err != nil {
			return err
		}
		areas.Fields.Add(&core.BoolField{Name: "allow_remote_arm"})
		if err := app.Save(areas); err != nil {
			return err
		}

		auxOutput, err := app.FindCollectionByNameOrId("aux_output")
		if err != nil {
			return err
		}
		auxOutput.Fields.Add(&core.BoolField{Name: "allow_remote"})
		if err := app.Save(auxOutput); err != nil {
			return err
		}

		s := app.Settings()
		s.RateLimits.Rules = upsertRateLimitRules(s.RateLimits.Rules, badgeActionRateLimitRules()...)
		return app.Save(s)
	}, func(app core.App) error {
		if areas, err := app.FindCollectionByNameOrId("areas"); err == nil {
			areas.Fields.RemoveByName("allow_remote_arm")
			if err := app.Save(areas); err != nil {
				return err
			}
		}
		if auxOutput, err := app.FindCollectionByNameOrId("aux_output"); err == nil {
			auxOutput.Fields.RemoveByName("allow_remote")
			if err := app.Save(auxOutput); err != nil {
				return err
			}
		}
		s := app.Settings()
		labels := make([]string, 0, 2)
		for _, r := range badgeActionRateLimitRules() {
			labels = append(labels, r.Label)
		}
		s.RateLimits.Rules = removeRateLimitRules(s.RateLimits.Rules, labels...)
		return app.Save(s)
	})
}

// badgeActionRateLimitRules limits the arm/disarm and output routes the same way
// 1750000032 limits the unlock route, and for the same reason: each call actuates
// something and writes an audit row, so an unlimited one is both a nuisance generator
// and a way to bury real activity in noise.
//
// Trailing '/' makes each a PATH PREFIX rule, which is what covers every record id
// under the route. Arming is rarer than opening a door — a person arms on the way out,
// once — so its budget is lower than unlock's ten.
func badgeActionRateLimitRules() []core.RateLimitRule {
	return []core.RateLimitRule{
		{
			Label:       "POST /api/badge/areas/",
			Audience:    core.RateLimitRuleAudienceAuth,
			MaxRequests: 6,
			Duration:    60,
		},
		{
			Label:       "POST /api/badge/outputs/",
			Audience:    core.RateLimitRuleAudienceAuth,
			MaxRequests: 10,
			Duration:    60,
		},
	}
}
