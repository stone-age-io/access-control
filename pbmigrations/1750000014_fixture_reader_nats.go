package pbmigrations

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Marks the seeded dev portals as NATS-only (reader_address = -1). The base
// fixture (1750000001) and extras (1750000005) create lobby-main/lobby-public
// before the reader_address field exists (added in 1750000008), so they cannot
// set it — and the field's zero default (0) reads as "OSDP reader at PD 0" under
// the controller's gating rule (>= 0 means a physical reader). Without this, a
// dev box run with reader "both"/"osdp" would arm both demo portals on OSDP at
// PD 0 and collide. Dev runs the simulated NATS reader, so the honest value is
// -1 (no OSDP reader). Guarded + idempotent: only touches the seeded portals if
// present, never an operator's own data.
func init() {
	migrations.Register(func(app core.App) error {
		for _, code := range []string{"lobby-main", "lobby-public"} {
			rec, err := app.FindFirstRecordByData("portals", "code", code)
			if err != nil {
				continue // not seeded (operator data, or extras didn't run) — skip
			}
			rec.Set("reader_address", -1)
			if err := app.Save(rec); err != nil {
				return err
			}
		}
		return nil
	}, func(app core.App) error {
		for _, code := range []string{"lobby-main", "lobby-public"} {
			rec, err := app.FindFirstRecordByData("portals", "code", code)
			if err != nil {
				continue
			}
			rec.Set("reader_address", 0)
			if err := app.Save(rec); err != nil {
				return err
			}
		}
		return nil
	})
}
