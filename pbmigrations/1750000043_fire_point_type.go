package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// A fire-alarm interface (FAI) becomes an ordinary aux input point type.
//
// The consuming half of fire has always worked: the controller suppresses alarm
// emission for a location while its fire input is asserted, and that is tested. The
// TRANSPORT works too — acc.{location}.evt.fire is both a control input the
// controller subscribes to and an audited event the stream captures. What was
// missing was any way to PRODUCE the signal: drivers.FAIInput had zero
// implementations, no board profile carried an FAI line, and the only way to assert
// fire was to publish the subject by hand.
//
// Adding `fire` to aux_input.point_type closes it with data instead of code. An aux
// input already has everything an FAI needs — a controller, a logical input index, a
// contact sense, a floor-plan position, a UI to configure it, and a driver path
// (GPIO and I2C already deliver aux transitions). So the FAI stops being a special
// interface plus per-controller config that does not exist, and becomes a row an
// integrator creates like any other point. drivers.FAIInput is deleted.
//
// Fire remains LOCATION-scoped even though the point is bound to one controller:
// the controller that owns the contact publishes acc.{location}.evt.fire, and every
// controller at that location (including the publisher, harmlessly and idempotently)
// applies it. That is why the subject was location-scoped in the first place.
//
// Software's role is unchanged and deliberately narrow: suppress alarm noise, record
// it, notify. HARDWARE OWNS EGRESS — the fire panel's relay drops maglock power
// directly, and nothing here unlocks a door.
func init() {
	migrations.Register(func(app core.App) error {
		auxInput, err := app.FindCollectionByNameOrId("aux_input")
		if err != nil {
			return err
		}
		auxInput.Fields.Add(&core.SelectField{
			Name:      "point_type",
			Values:    []string{"monitor", "intrusion", "tamper_24h", "fire"},
			MaxSelect: 1,
		})
		return app.Save(auxInput)
	}, func(app core.App) error {
		auxInput, err := app.FindCollectionByNameOrId("aux_input")
		if err != nil {
			return nil // collection gone — nothing to revert
		}
		auxInput.Fields.Add(&core.SelectField{
			Name:      "point_type",
			Values:    []string{"monitor", "intrusion", "tamper_24h"},
			MaxSelect: 1,
		})
		return app.Save(auxInput)
	})
}
