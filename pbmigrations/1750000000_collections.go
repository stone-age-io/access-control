// Package pbmigrations defines the stone-access PocketBase schema as code.
//
// Importing this package for its side effects registers the migrations with
// PocketBase's AppMigrations list; the `serve` command applies any pending ones
// on startup (apis/serve.go calls RunAllMigrations).
package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
	"github.com/pocketbase/pocketbase/tools/types"
)

// The policy graph. Cross-references are PocketBase relations; the KV mirror
// (accessd) resolves them to stable codes when publishing. Access rules are
// left nil — that means superusers only, which is the right default for a PACS
// control plane. Per-org API access can be layered on later.
func init() {
	migrations.Register(func(app core.App) error {
		// --- locations: a building/campus; owns the timezone. Maps 1:1 to the
		// platform's Location entity (the {location} subject segment). ---
		locations := core.NewBaseCollection("locations")
		locations.Fields.Add(&core.TextField{Name: "code", Required: true})
		locations.Fields.Add(&core.TextField{Name: "name"})
		// timezone is an IANA name; validated in a hook (see internal/mirror).
		locations.Fields.Add(&core.TextField{Name: "timezone", Required: true})
		// fai_suppress: while the location's fire alarm input is active, suppress
		// forced/held-open alarms (hardware owns egress). Default true.
		locations.Fields.Add(&core.BoolField{Name: "fai_suppress"})
		addTimestamps(locations)
		locations.AddIndex("idx_locations_code", true, "code", "")
		if err := app.Save(locations); err != nil {
			return err
		}

		// --- schedules: reusable weekly time windows. ---
		schedules := core.NewBaseCollection("schedules")
		schedules.Fields.Add(&core.TextField{Name: "code", Required: true})
		schedules.Fields.Add(&core.TextField{Name: "name"})
		// windows: [{days:[1..7], start:"HH:MM", end:"HH:MM"}]; end<=start crosses midnight.
		schedules.Fields.Add(&core.JSONField{Name: "windows", MaxSize: 1 << 16})
		addTimestamps(schedules)
		schedules.AddIndex("idx_schedules_code", true, "code", "")
		if err := app.Save(schedules); err != nil {
			return err
		}

		// --- controllers: an edge box (e.g. a KinCony Server-Mini) that drives the
		// portals assigned to it. The box's identity is just its code; which portals
		// it drives, and the logical relay/input indices for each, live on the portal
		// records (see the `controller` relation below). `model` selects the hardware
		// template that maps those logical indices to physical GPIO/expander lines.
		// last_seen/status are written by accessd from controller heartbeats. ---
		controllers := core.NewBaseCollection("controllers")
		controllers.Fields.Add(&core.TextField{Name: "code", Required: true})
		controllers.Fields.Add(&core.TextField{Name: "name"})
		controllers.Fields.Add(&core.RelationField{
			Name:         "location",
			CollectionId: locations.Id,
			Required:     true,
			MaxSelect:    1,
		})
		controllers.Fields.Add(&core.SelectField{
			Name:      "model",
			Values:    []string{"kincony-server-mini", "kincony-pi5r8"},
			MaxSelect: 1,
		})
		// last_seen/status: liveness, written by accessd from heartbeats (not mirrored).
		controllers.Fields.Add(&core.DateField{Name: "last_seen"})
		controllers.Fields.Add(&core.SelectField{
			Name:      "status",
			Values:    []string{"online", "offline"},
			MaxSelect: 1,
		})
		addTimestamps(controllers)
		controllers.AddIndex("idx_controllers_code", true, "code", "")
		if err := app.Save(controllers); err != nil {
			return err
		}

		// --- portals: a controllable opening (door/gate/turnstile/elevator) or a
		// logical access target. Each portal is a platform Thing addressed
		// {location}.{type}.{code} on the bus, so `type` is a first-class field
		// and a single NATS token. (Formerly "access_points".) ---
		portals := core.NewBaseCollection("portals")
		portals.Fields.Add(&core.TextField{Name: "code", Required: true})
		// type: the portal kind; also the {type} subject segment, so it must be a
		// single NATS token (enforced at the mirror boundary, see internal/mirror).
		portals.Fields.Add(&core.SelectField{
			Name:      "type",
			Values:    []string{"door", "turnstile", "elevator", "gate", "logical"},
			MaxSelect: 1,
			Required:  true,
		})
		portals.Fields.Add(&core.RelationField{
			Name:         "location",
			CollectionId: locations.Id,
			Required:     true,
			MaxSelect:    1,
		})
		portals.Fields.Add(&core.TextField{Name: "name"})
		// posture: standing default; a runtime command or a scheduled-posture
		// window may override it on the controller. free_access = any tap opens
		// (no validation, strike pulses); unlocked = strike physically held open.
		portals.Fields.Add(&core.SelectField{
			Name:      "posture",
			Values:    []string{"secure", "free_access", "unlocked", "lockdown", "disabled"},
			MaxSelect: 1,
		})
		portals.Fields.Add(&core.NumberField{Name: "pulse_seconds", OnlyInt: true})
		// controller: which edge box drives this portal. Optional — a logical portal
		// or a not-yet-installed door may be unassigned (the controller simply won't
		// arm it). The remaining fields are *logical* hardware indices on that box,
		// resolved to physical lines by the controller's model template.
		portals.Fields.Add(&core.RelationField{
			Name:         "controller",
			CollectionId: controllers.Id,
			MaxSelect:    1,
		})
		portals.Fields.Add(&core.NumberField{Name: "lock_relay", OnlyInt: true})        // relay output index
		portals.Fields.Add(&core.NumberField{Name: "dps_input", OnlyInt: true})         // door-position-switch input index
		portals.Fields.Add(&core.NumberField{Name: "rex_input", OnlyInt: true})         // request-to-exit input index
		portals.Fields.Add(&core.NumberField{Name: "held_open_seconds", OnlyInt: true}) // DOTL threshold
		addTimestamps(portals)
		portals.AddIndex("idx_portals_code", true, "code", "")
		if err := app.Save(portals); err != nil {
			return err
		}

		// --- access_groups ("access levels"): a set of portals under one schedule. ---
		accessGroups := core.NewBaseCollection("access_groups")
		accessGroups.Fields.Add(&core.TextField{Name: "code", Required: true})
		accessGroups.Fields.Add(&core.TextField{Name: "name"})
		accessGroups.Fields.Add(&core.RelationField{
			Name:         "portals",
			CollectionId: portals.Id,
			MaxSelect:    9999,
		})
		accessGroups.Fields.Add(&core.RelationField{
			Name:         "schedule",
			CollectionId: schedules.Id,
			Required:     true,
			MaxSelect:    1,
		})
		addTimestamps(accessGroups)
		accessGroups.AddIndex("idx_access_groups_code", true, "code", "")
		if err := app.Save(accessGroups); err != nil {
			return err
		}

		// --- roles: a named bundle of access groups assigned to users. ---
		roles := core.NewBaseCollection("roles")
		roles.Fields.Add(&core.TextField{Name: "code", Required: true})
		roles.Fields.Add(&core.TextField{Name: "name"})
		roles.Fields.Add(&core.RelationField{
			Name:         "access_groups",
			CollectionId: accessGroups.Id,
			MaxSelect:    9999,
		})
		addTimestamps(roles)
		roles.AddIndex("idx_roles_code", true, "code", "")
		if err := app.Save(roles); err != nil {
			return err
		}

		// --- cardholders: the people a PACS is about. An AUTH collection, so one
		// person is one record whether or not they ever sign in. Named
		// "cardholders" to avoid colliding with PocketBase's built-in "users"
		// (the OPERATOR tier); mirrored to KV under the user.{id} key prefix to
		// match the policy contract. internal/badgeapi's package doc has the
		// reasoning for one collection rather than a login record beside the
		// person. ---
		cardholders := core.NewAuthCollection("cardholders")
		cardholders.Fields.Add(&core.TextField{Name: "external_id"}) // IdP/LDAP/CSV key, nullable
		cardholders.Fields.Add(&core.TextField{Name: "name"})
		// REPLACES the required system email field NewAuthCollection installs, so
		// it stays optional exactly as it was before this collection became an
		// auth collection.
		//
		// Optional because email here is UNIQUE, and plenty of cardholders
		// genuinely have no address: contractors and cleaning crews, hourly staff
		// at sites that issue no mailboxes, and the non-person cards every real
		// install carries ("Loading Dock Spare", "Fire Dept Lockbox"). Requiring it
		// would force a SYNTHETIC unique address per such person, which is worse
		// than blank in every direction — the field stops meaning "how to reach
		// this person", something has to mint them, and they can be mailed to by
		// accident. An LDAP/CSV import with a sparse email column would hard-fail
		// rather than import, and `external_id` above exists precisely because
		// identity arrives from an IdP rather than an inbox.
		//
		// This is NOT what stops such a person signing in — the AuthRule below is.
		// PocketBase re-enforces System/Hidden on a supplied field but leaves
		// Required alone, and the unique index it generates is `WHERE email != ''`,
		// so blanks do not collide.
		cardholders.Fields.Add(&core.EmailField{Name: "email"})
		cardholders.Fields.Add(&core.SelectField{
			Name:      "status",
			Values:    []string{"active", "suspended"},
			MaxSelect: 1,
		})
		cardholders.Fields.Add(&core.RelationField{
			Name:         "roles",
			CollectionId: roles.Id,
			MaxSelect:    9999,
		})
		// badge_login: may this person sign in to see their own badge? An explicit
		// operator-set flag, and THE gate — every cardholder is an auth record
		// carrying a random password nobody has seen, so this flag, via the
		// AuthRule below, is what separates "a person in the system" from "an
		// account". Nothing else is load-bearing for that.
		//
		// Deliberately NOT the built-in `verified` field, which would be a trap:
		// PocketBase WRITES `verified` itself (auth-with-otp sets it on a first
		// successful code), and requestOTP does not consult the AuthRule at all —
		// so an unverified person could request a code, submit it, have PocketBase
		// flip the flag, and only then be measured against a rule that now passes.
		// A `verified`-gated AuthRule opens itself on first use.
		cardholders.Fields.Add(&core.BoolField{Name: "badge_login"})
		// kind: which badge shape this person is. NOT an expiry and NOT an access
		// level — validity lives on the credential's valid_from/valid_until, which
		// the edge enforces. It decides what the QR encodes (an inert identifier
		// for staff, the credential VALUE for a visitor) and which lifecycle rules
		// apply (see internal/badgeapi, internal/badgesweep).
		//
		// Not required, and blank means an ordinary cardholder. That is deliberate:
		// every fork on this field asks "is this a visitor?", so the only value
		// that must be set correctly is `visitor`, and an unset field can never
		// accidentally mean "put a working credential in the QR code".
		cardholders.Fields.Add(&core.SelectField{
			Name:      "kind",
			Values:    []string{"holder", "visitor"},
			MaxSelect: 1,
		})
		// password_set: does this person know their own password? A badge login is
		// created with an unguessable throwaway, because PocketBase requires a
		// non-blank password on every auth record regardless of the enabled
		// methods — so without this flag there is no way to tell "knows their
		// password" from "has a random string nobody has ever seen". The
		// difference decides whether a password change must prove the old one.
		cardholders.Fields.Add(&core.BoolField{Name: "password_set"})
		addTimestamps(cardholders)
		// keyed in KV by PB id (user.{id}); external_id is just a lookup aid.
		cardholders.AddIndex("idx_cardholders_external_id", false, "external_id", "")

		// --- auth options ---
		// Email + password AND OTP. Password matters most at the small installs
		// least likely to run a mail server: with OTP as the only method every
		// sign-in is an emailed code, so the whole badge tier is inert without
		// SMTP. Visitors are still minted without a password and use OTP.
		cardholders.PasswordAuth.Enabled = true
		// Email only. There is no username field, and adding one would create a
		// second way to name the same person.
		cardholders.PasswordAuth.IdentityFields = []string{"email"}
		cardholders.OAuth2.Enabled = true
		cardholders.OTP.Enabled = true
		cardholders.OTP.Duration = 900 // 15 min — long enough to find the mail on a phone
		cardholders.OTP.Length = 8
		// A visitor would otherwise receive the OTP mail and then, seconds later, a
		// "new device login" alert for the same action. Confusing enough to be a
		// support call.
		cardholders.AuthAlert.Enabled = false
		// AuthRule gates who may obtain a token: an opted-in, active person. Note
		// it is checked at ISSUANCE only (apis.recordAuthResponse), NOT on every
		// authenticated request — so it stops a new sign-in and the next token
		// refresh, but does not kill a live session. Withdrawing a suspended
		// person's badge face is done server-side in internal/badgeapi, which is
		// the mechanism that acts immediately.
		cardholders.AuthRule = types.Pointer(`badge_login = true && status = "active"`)
		// ManageRule lets an operator holding `enroll` reset a stuck login's
		// password through the collection API without proving the old one.
		cardholders.ManageRule = types.Pointer(`@request.auth.collectionName = "users" && @request.auth.permissions ~ "enroll"`)
		// Explicit, and load-bearing: an auth collection reached through the
		// dashboard defaults to OPEN SIGNUP, which here would let anyone mint
		// themselves a person record. 1750000009/1750000016 narrow this to the
		// `enroll` capability; stating it here means the collection is never open
		// even for the instant between migrations.
		cardholders.CreateRule = nil // superusers only until 1750000009 runs
		if err := app.Save(cardholders); err != nil {
			return err
		}

		// --- credentials: opaque strings presented at a reader, each mapping to one user. ---
		credentials := core.NewBaseCollection("credentials")
		credentials.Fields.Add(&core.TextField{Name: "value", Required: true})
		credentials.Fields.Add(&core.SelectField{
			Name:      "type",
			Values:    []string{"nkey", "wiegand", "pin", "mobile"},
			MaxSelect: 1,
		})
		credentials.Fields.Add(&core.RelationField{
			Name:         "user",
			CollectionId: cardholders.Id,
			Required:     true,
			MaxSelect:    1,
		})
		credentials.Fields.Add(&core.SelectField{
			Name:      "status",
			Values:    []string{"active", "revoked", "suspended"},
			MaxSelect: 1,
		})
		credentials.Fields.Add(&core.TextField{Name: "label"})
		addTimestamps(credentials)
		credentials.AddIndex("idx_credentials_value", true, "value", "")
		if err := app.Save(credentials); err != nil {
			return err
		}

		// --- events: queryable projection of the JetStream audit stream. ---
		// Denormalized snapshot (plain text/codes), written by the audit consumer.
		events := core.NewBaseCollection("events")
		events.Fields.Add(&core.TextField{Name: "location"})
		events.Fields.Add(&core.TextField{Name: "portal"})
		// type: the portal type from the event subject; empty for location-scoped
		// fire events. Lets dashboards slice by portal kind (all doors, etc.).
		events.Fields.Add(&core.TextField{Name: "type"})
		events.Fields.Add(&core.SelectField{
			Name:      "kind",
			Values:    []string{"tap", "state", "alarm", "fire"},
			MaxSelect: 1,
		})
		events.Fields.Add(&core.TextField{Name: "credential"})
		events.Fields.Add(&core.TextField{Name: "user"})
		events.Fields.Add(&core.BoolField{Name: "allow"})
		events.Fields.Add(&core.TextField{Name: "reason"})
		events.Fields.Add(&core.JSONField{Name: "payload", MaxSize: 1 << 16})
		events.Fields.Add(&core.DateField{Name: "ts"})
		addTimestamps(events)
		events.AddIndex("idx_events_location_ts", false, "location, ts", "")
		return app.Save(events)
	}, func(app core.App) error {
		// Down: delete in reverse dependency order.
		for _, name := range []string{
			"events", "credentials", "cardholders", "roles",
			"access_groups", "portals", "controllers", "schedules", "locations",
		} {
			c, err := app.FindCollectionByNameOrId(name)
			if err != nil {
				continue // already gone
			}
			if err := app.Delete(c); err != nil {
				return err
			}
		}
		return nil
	})
}

// addTimestamps adds the conventional created/updated autodate fields.
func addTimestamps(c *core.Collection) {
	c.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
	c.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})
}
