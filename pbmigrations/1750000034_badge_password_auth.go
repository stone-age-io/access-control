package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Enables EMAIL + PASSWORD sign-in on the badge tier alongside the OTP and OAuth2
// methods from migration 1750000030, and adds the `password_set` flag that makes a
// safe self-service password change possible.
//
// # Why passwords, having deliberately left them off
//
// 1750000030 disabled them so a visitor would never have to invent a password for a
// one-day pass. That reasoning still holds for VISITORS — the mint flow sets no
// password, and OTP remains their path. It does not hold for the install as a whole:
//
//   - With OTP as the only method the badge tier is INERT WITHOUT SMTP. Every sign-in
//     is an emailed code, so an install with no mail server cannot use badges at all.
//     A password handed over at enrollment removes that dependency, which matters most
//     at exactly the small installs least likely to run a mail server.
//   - A staff holder signs in repeatedly for years. Waiting on an email every time is
//     the kind of friction that gets a feature quietly abandoned.
//
// The credential-stuffing surface this reopens is answered with rate limits below
// rather than by refusing the method.
//
// # IdentityFields
//
// Email only. `badge_users` has no username field, and adding one would create a
// second way to name the same person — the email is already the unique key the mint
// and enrollment flows dedupe on.
//
// # password_set
//
// A badge login is created with an unguessable THROWAWAY password (see
// internal/badgeapi), because PocketBase requires a non-blank one on every auth
// record regardless of the enabled methods. That leaves no way to tell "this holder
// knows their password" from "this record has a random string nobody has ever seen",
// and the difference decides whether a password change may proceed without proving
// the old one:
//
//   - PocketBase's own record-update path always demands `oldPassword` from a
//     non-manager (apis.hasAuthManageAccess), so a holder who signed in by OTP could
//     never set a first password through it — they cannot know the throwaway.
//   - Skipping that proof unconditionally would be worse: for a holder who DOES have
//     a password, a stolen session could silently change it and lock them out.
//
// So `password_set` records which of the two states a record is in, and
// POST /api/badge/password requires the old password if and only if it is true.
// It is a control-plane hint, never mirrored to KV and never seen by policy.Decide.
func init() {
	migrations.Register(func(app core.App) error {
		c, err := app.FindCollectionByNameOrId("badge_users")
		if err != nil {
			return err
		}

		c.PasswordAuth.Enabled = true
		c.PasswordAuth.IdentityFields = []string{"email"}

		// False for every existing record, which is exactly right: each was created
		// with a throwaway, so none of their holders knows a password.
		c.Fields.Add(&core.BoolField{Name: "password_set"})

		if err := app.Save(c); err != nil {
			return err
		}

		s := app.Settings()
		s.RateLimits.Enabled = true
		s.RateLimits.Rules = upsertRateLimitRules(s.RateLimits.Rules, badgePasswordRateLimitRules()...)
		return app.Save(s)
	}, func(app core.App) error {
		s := app.Settings()
		s.RateLimits.Rules = removeRateLimitRules(s.RateLimits.Rules, badgePasswordRateLimitLabels()...)
		if err := app.Save(s); err != nil {
			return err
		}

		c, err := app.FindCollectionByNameOrId("badge_users")
		if err != nil {
			return nil // already gone
		}
		c.PasswordAuth.Enabled = false
		c.PasswordAuth.IdentityFields = nil
		c.Fields.RemoveByName("password_set")
		return app.Save(c)
	})
}

// badgePasswordRateLimitRules covers the endpoints password auth newly exposes.
// Both are addressed by COLLECTION TAG (see 1750000032 for why the tag form is
// preferred over a path) and both use audience "all", because a caller at either
// endpoint has no token yet — an @auth rule would never fire.
func badgePasswordRateLimitRules() []core.RateLimitRule {
	return []core.RateLimitRule{
		// The credential-stuffing rule. A person mistyping their own password needs a
		// few tries; a script wants thousands. Note this is per client IP, so
		// Settings().TrustedProxy must be set behind a reverse proxy or every attempt
		// shares one bucket — see docs/configuration.md.
		{
			Label:       "badge_users:authWithPassword",
			Audience:    core.RateLimitRuleAudienceAll,
			MaxRequests: 10,
			Duration:    60,
		},
		// One email per call, so the same mail-bomb concern as requestOTP — and tighter,
		// because unlike a sign-in code nobody legitimately needs several reset mails a
		// minute.
		{
			Label:       "badge_users:requestPasswordReset",
			Audience:    core.RateLimitRuleAudienceAll,
			MaxRequests: 3,
			Duration:    60,
		},
		// POST /api/badge/password checks the CURRENT password before changing it (see
		// internal/badgeapi/holders.go), which makes it an oracle for guessing that
		// password from a stolen session. Audience "auth" because the caller is signed
		// in by definition, and low because changing a password is a once-in-a-while act.
		{
			Label:       "POST /api/badge/password",
			Audience:    core.RateLimitRuleAudienceAuth,
			MaxRequests: 5,
			Duration:    60,
		},
	}
}

func badgePasswordRateLimitLabels() []string {
	rules := badgePasswordRateLimitRules()
	labels := make([]string, 0, len(rules))
	for _, r := range rules {
		labels = append(labels, r.Label)
	}
	return labels
}
