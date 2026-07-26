package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Sets default rate limits for the badge tier.
//
// These are the first routes reachable by someone who is not an operator, so they
// are the first place per-client abuse is a real concern:
//
//   - /api/badge/unlock/* actuates a door. Without a limit, a badge holder could
//     hammer every remotely-unlockable door, and each attempt costs a policy
//     evaluation plus an audit row.
//   - The OTP request endpoint mints an email per call. Unlimited, it is both a mail
//     bomb aimed at a visitor and a way to burn an install's SMTP quota.
//
// PocketBase's limiter is settings-driven (core.RateLimitRule) and bound globally to
// every route, so it covers accessd's custom routes too — no per-route middleware
// needed. Matching is by label, in this order: an exact "METHOD /path", a bare
// "/path", a collection tag like "badge_users:requestOTP", and — for any rule whose
// label ends in '/' — a path PREFIX. That last form is what lets one rule cover
// /api/badge/unlock/{portalId} for every portal id, so keeping the badge routes
// under a shared /api/badge/ prefix is load-bearing here.
//
// Set here rather than left to the installer because an unconfigured limiter is
// wide open, and "remember to add rate limits in the admin UI" is not a security
// control. Values are conservative for humans and hostile to scripts; an install
// that needs different numbers can edit them in Settings.
//
// Note for deployments behind a reverse proxy: the limiter keys on client IP, so
// Settings().TrustedProxy must be configured or every request appears to come from
// the proxy and shares one bucket. Same caveat as the audit log's request_ip.
func init() {
	migrations.Register(func(app core.App) error {
		s := app.Settings()
		s.RateLimits.Enabled = true
		s.RateLimits.Rules = upsertRateLimitRules(s.RateLimits.Rules, badgeRateLimitRules()...)
		return app.Save(s)
	}, func(app core.App) error {
		s := app.Settings()
		s.RateLimits.Rules = removeRateLimitRules(s.RateLimits.Rules, badgeRateLimitLabels()...)
		return app.Save(s)
	})
}

// badgeRateLimitRules are the rules this migration owns.
func badgeRateLimitRules() []core.RateLimitRule {
	return []core.RateLimitRule{
		// Actuating a door: someone standing at a door needs a handful of tries, not
		// dozens. Trailing '/' makes this a prefix rule covering every portal id.
		{
			Label:       "POST /api/badge/unlock/",
			Audience:    core.RateLimitRuleAudienceAuth,
			MaxRequests: 10,
			Duration:    60,
		},
		// Reading one's own badge: generous, it is a page load.
		{
			Label:       "GET /api/badge/me",
			Audience:    core.RateLimitRuleAudienceAuth,
			MaxRequests: 60,
			Duration:    60,
		},
		// OTP request — one email per call. Addressed by COLLECTION TAG rather than
		// path: PocketBase checks "badge_users:requestOTP" first and the tag survives
		// any change to the auth route paths. Audience is "all" because the caller has
		// no token yet (they are requesting the means to get one), so a @auth rule
		// would never fire — this is the anti-mail-bomb rule.
		{
			Label:       "badge_users:requestOTP",
			Audience:    core.RateLimitRuleAudienceAll,
			MaxRequests: 5,
			Duration:    60,
		},
	}
}

func badgeRateLimitLabels() []string {
	labels := make([]string, 0, 3)
	for _, r := range badgeRateLimitRules() {
		labels = append(labels, r.Label)
	}
	return labels
}

// upsertRateLimitRules adds each rule, replacing any existing rule with the same
// label rather than appending a duplicate — so a re-run is idempotent and an
// operator's edit to an unrelated rule is preserved.
func upsertRateLimitRules(existing []core.RateLimitRule, add ...core.RateLimitRule) []core.RateLimitRule {
	out := make([]core.RateLimitRule, 0, len(existing)+len(add))
	replaced := make(map[string]bool, len(add))
	for _, cur := range existing {
		var hit bool
		for _, r := range add {
			if cur.Label == r.Label {
				out = append(out, r)
				replaced[r.Label] = true
				hit = true
				break
			}
		}
		if !hit {
			out = append(out, cur)
		}
	}
	for _, r := range add {
		if !replaced[r.Label] {
			out = append(out, r)
		}
	}
	return out
}

// removeRateLimitRules drops the rules with the given labels, leaving everything
// else (including operator edits) alone.
func removeRateLimitRules(existing []core.RateLimitRule, labels ...string) []core.RateLimitRule {
	drop := make(map[string]bool, len(labels))
	for _, l := range labels {
		drop[l] = true
	}
	out := make([]core.RateLimitRule, 0, len(existing))
	for _, r := range existing {
		if !drop[r.Label] {
			out = append(out, r)
		}
	}
	return out
}
