package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Widens events.source to the two values the code already emits: "command" (an
// operator's cmd.grant door-pop) and "badge" (a cardholder's own remote unlock).
//
// 1750000013 created the field for reader transports only — "nats" and "osdp" —
// and the commit that taught cmd.grant to carry provenance added
// subjects.SourceCommand/SourceBadge on the emitting side without ever widening
// the select on the projecting side. The result was not a missing column value
// but an ERROR LOOP: PocketBase rejects an out-of-range select value, the audit
// consumer treated the rejection as retryable, and JetStream redelivered the
// message immediately and forever. Every operator door-pop and every badge
// unlock was one such message.
//
// The vocabulary is coherent — all four answer "how did this event arrive" — so
// widening is the right fix here, as opposed to the arm-transition event, which
// was putting arm-state provenance (standing/scheduled/override) under the same
// key and had to be renamed instead: that is a different question that happened
// to share a word.
func init() {
	migrations.Register(func(app core.App) error {
		events, err := app.FindCollectionByNameOrId("events")
		if err != nil {
			return err
		}
		f, ok := events.Fields.GetByName("source").(*core.SelectField)
		if !ok {
			return nil // field gone or retyped; nothing to widen
		}
		f.Values = []string{"nats", "osdp", "command", "badge"}
		return app.Save(events)
	}, func(app core.App) error {
		events, err := app.FindCollectionByNameOrId("events")
		if err != nil {
			return nil // already gone
		}
		f, ok := events.Fields.GetByName("source").(*core.SelectField)
		if !ok {
			return nil
		}
		f.Values = []string{"nats", "osdp"}
		return app.Save(events)
	})
}
