package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Per-install wiring sense for door/aux I/O — the logical contact type (normally
// open vs normally closed) and lock fail-safe behavior an installer chooses,
// distinct from the board's electrical polarity (which lives in the controller
// model's hardware profile). All fields are optional and an empty value means the
// common default, so existing portals/aux inputs keep today's behavior:
//
//   - portals.dps_contact: door-position contact, default normally-CLOSED (closed
//     when the door is shut). "no" = normally-open contact (inverts the reading).
//   - portals.rex_contact: request-to-exit contact, default normally-OPEN (closed
//     when pressed). "nc" = normally-closed contact (inverts).
//   - portals.lock_type: "strike" (default, fail-secure, energize-to-unlock) vs
//     "maglock" (fail-safe, energize-to-lock — the relay idles energized/locked).
//   - portals.rex_unlock: when true a REX press also pulses the strike, not just
//     shunts the forced alarm (default false: egress is mechanical).
//   - aux_input.contact: default normally-OPEN; "nc" inverts.
//
// These are logical-sense hints consumed only by the controller's hardware arming,
// never by the pure policy.Decide.
func init() {
	migrations.Register(func(app core.App) error {
		portals, err := app.FindCollectionByNameOrId("portals")
		if err != nil {
			return err
		}
		portals.Fields.Add(&core.SelectField{Name: "dps_contact", Values: []string{"nc", "no"}, MaxSelect: 1})
		portals.Fields.Add(&core.SelectField{Name: "rex_contact", Values: []string{"no", "nc"}, MaxSelect: 1})
		portals.Fields.Add(&core.SelectField{Name: "lock_type", Values: []string{"strike", "maglock"}, MaxSelect: 1})
		portals.Fields.Add(&core.BoolField{Name: "rex_unlock"})
		if err := app.Save(portals); err != nil {
			return err
		}

		auxInput, err := app.FindCollectionByNameOrId("aux_input")
		if err != nil {
			return err
		}
		auxInput.Fields.Add(&core.SelectField{Name: "contact", Values: []string{"no", "nc"}, MaxSelect: 1})
		return app.Save(auxInput)
	}, func(app core.App) error {
		if portals, err := app.FindCollectionByNameOrId("portals"); err == nil {
			portals.Fields.RemoveByName("dps_contact")
			portals.Fields.RemoveByName("rex_contact")
			portals.Fields.RemoveByName("lock_type")
			portals.Fields.RemoveByName("rex_unlock")
			if err := app.Save(portals); err != nil {
				return err
			}
		}
		if auxInput, err := app.FindCollectionByNameOrId("aux_input"); err == nil {
			auxInput.Fields.RemoveByName("contact")
			return app.Save(auxInput)
		}
		return nil
	})
}
