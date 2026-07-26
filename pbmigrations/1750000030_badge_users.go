package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/tools/types"
)

// Adds the BADGE TIER: a second auth collection for the people a PACS is about —
// cardholders and visitors who sign in to see their own badge and, where the portal
// permits it, unlock a door remotely. It is emphatically NOT an operator tier: a
// badge record has no `permissions` field and must never read the policy graph.
//
// Why a separate collection instead of making `cardholders` an auth collection:
//
//   - PocketBase fixes collection type at creation, so converting `cardholders`
//     would mean a new collection plus a data copy — repointing the
//     `credentials.user` relation and changing every KV `user.{id}` key, since the
//     mirror keys cardholders by PocketBase id. That is a wire-contract change for
//     no gain.
//   - It would force a login onto badge-only staff who should never have one.
//   - The separation is the right model anyway: a login is control-plane, a
//     credential is data-plane. Adding or removing a login must not touch the policy
//     graph, and here it cannot — `badge_users` is absent from
//     mirror.mirroredCollections, so no hook fires and no KV key exists.
//
// Revocation semantics are therefore unchanged: disabling someone's access is still
// `credentials.status = revoked` (or the cardholder's status), which propagates
// through the mirror to KV. Deleting a badge login only removes their ability to
// SEE the badge; their card still works. That asymmetry is deliberate and is
// documented in docs/operators.md.
//
// Auth methods: OTP + OAuth2, password DISABLED. A visitor should never have to
// invent and manage a password for a one-day pass, and disabling it removes the
// credential-stuffing surface entirely. OTP also does real work here: it turns the
// badge link from a pure bearer token into "link + proof of control of the invited
// inbox", which is what makes emailing a badge defensible. Providers for OAuth2 are
// left for the installer to configure in the admin UI (PocketBase allows an enabled
// OAuth2 config with no providers).
//
// Note for anything that CREATES a badge_users record: PocketBase requires a
// non-blank `password` on an auth record even with password auth disabled — the
// field validator is independent of the auth-method option. The value is unusable
// for sign-in (the API refuses a disabled method), so callers set an unguessable
// throwaway rather than something a person could be told. See internal/badgeapi.
//
// Prerequisite: migration 1750000027. Before it, the policy collections' read floor
// was `@request.auth.id != ""` — auth-collection-agnostic — so creating this
// collection would have handed every visitor operator read on the whole graph,
// including `credentials.value` in plaintext.
func init() {
	migrations.Register(func(app core.App) error {
		cardholders, err := app.FindCollectionByNameOrId("cardholders")
		if err != nil {
			return err
		}

		c := core.NewAuthCollection("badge_users")

		// The policy identity this login speaks for. Required and unique: a login
		// with no cardholder could not be authorized for anything (policy.Decide
		// needs a credential), and two logins for one person would make the audit
		// trail ambiguous.
		c.Fields.Add(&core.RelationField{
			Name:         "cardholder",
			CollectionId: cardholders.Id,
			Required:     true,
			MaxSelect:    1,
		})
		// Discriminates the two badge shapes. It is NOT an expiry or an access
		// level — validity lives on the credential's valid_from/valid_until, which
		// the edge already enforces. `kind` drives presentation and lifecycle:
		// which QR payload the badge carries (an identifier for staff, the credential
		// value for a visitor) and whether the expiry sweep may disable the login.
		c.Fields.Add(&core.SelectField{
			Name:      "kind",
			Values:    []string{"holder", "visitor"},
			MaxSelect: 1,
			Required:  true,
		})
		addTimestamps(c)

		// One login per cardholder.
		c.AddIndex("idx_badge_users_cardholder", true, "cardholder", "")

		// --- auth options ---
		c.PasswordAuth.Enabled = false
		c.PasswordAuth.IdentityFields = nil
		c.OAuth2.Enabled = true
		c.OTP.Enabled = true
		c.OTP.Duration = 900 // 15 min — long enough to find the email on a phone
		c.OTP.Length = 8
		// A visitor receives the OTP email and then, seconds later, a "new device
		// login" alert for the same action. Confusing enough to be a support call.
		c.AuthAlert.Enabled = false

		// --- rules ---
		// Read/update: the badge holder sees ONLY their own record; operators
		// holding `enroll` manage the tier. The second clause names the operator
		// collection, so a badge token can never satisfy it.
		self := types.Pointer(`id = @request.auth.id || (` + operatorEnroll + `)`)
		c.ListRule, c.ViewRule, c.UpdateRule = self, self, self
		// Explicit, because an auth collection's default create rule is OPEN SIGNUP.
		// Anyone could otherwise mint themselves a badge login.
		c.CreateRule = types.Pointer(operatorEnroll)
		c.DeleteRule = types.Pointer(operatorOperators)
		// Setting another record's auth data (e.g. resetting a stuck login).
		c.ManageRule = types.Pointer(operatorEnroll)

		return app.Save(c)
	}, func(app core.App) error {
		c, err := app.FindCollectionByNameOrId("badge_users")
		if err != nil {
			return nil // already gone
		}
		return app.Delete(c)
	})
}

// Operator-scoped capability rule fragments. Each names the `users` collection as
// well as the capability: `permissions` does not exist on a badge record, so the
// capability test alone would merely be false rather than wrong — but stating the
// collection keeps the intent legible and survives a future tier that happens to
// have a `permissions` field.
const (
	operatorEnroll    = `@request.auth.collectionName = "users" && @request.auth.permissions ~ "enroll"`
	operatorOperators = `@request.auth.collectionName = "users" && @request.auth.permissions ~ "operators"`
)
