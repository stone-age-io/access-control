package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Adds reader_address to portals: the OSDP PD (peripheral device) address of the
// portal's reader on its controller's RS485 bus. A controller whose reader is
// "osdp" polls this address and maps card reads from it back to this portal.
// Default 0 — the conventional single-reader address; a multi-reader bus gives
// each portal a distinct address (0..126). Ignored when the reader is "nats".
// Mirrors the lock_relay/dps_input pattern: a logical hardware binding consumed
// only by the controller, never by the pure policy.Decide.
func init() {
	migrations.Register(func(app core.App) error {
		portals, err := app.FindCollectionByNameOrId("portals")
		if err != nil {
			return err
		}
		portals.Fields.Add(&core.NumberField{Name: "reader_address", OnlyInt: true})
		return app.Save(portals)
	}, func(app core.App) error {
		portals, err := app.FindCollectionByNameOrId("portals")
		if err != nil {
			return nil // already gone
		}
		portals.Fields.RemoveByName("reader_address")
		return app.Save(portals)
	})
}
