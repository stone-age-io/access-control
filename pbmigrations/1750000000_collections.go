// Package pbmigrations defines the stone-access PocketBase schema as code.
//
// Importing this package for its side effects registers the migrations with
// PocketBase's AppMigrations list; the `serve` command applies any pending ones
// on startup (apis/serve.go calls RunAllMigrations).
package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
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
		// posture: standing default; a runtime command may override it on the controller.
		portals.Fields.Add(&core.SelectField{
			Name:      "posture",
			Values:    []string{"secure", "unlocked", "lockdown", "disabled"},
			MaxSelect: 1,
		})
		portals.Fields.Add(&core.NumberField{Name: "pulse_seconds", OnlyInt: true})
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

		// --- cardholders: people who hold credentials (IdP/LDAP/CSV identities),
		// NOT PocketBase logins. Named "cardholders" to avoid colliding with
		// PocketBase's built-in "users" auth collection; mirrored to KV under the
		// user.{id} key prefix to match the policy contract. ---
		cardholders := core.NewBaseCollection("cardholders")
		cardholders.Fields.Add(&core.TextField{Name: "external_id"}) // IdP/LDAP/CSV key, nullable
		cardholders.Fields.Add(&core.TextField{Name: "name"})
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
		addTimestamps(cardholders)
		// keyed in KV by PB id (user.{id}); external_id is just a lookup aid.
		cardholders.AddIndex("idx_cardholders_external_id", false, "external_id", "")
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
			"access_groups", "portals", "schedules", "locations",
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
