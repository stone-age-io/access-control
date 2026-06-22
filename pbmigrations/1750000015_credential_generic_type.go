package pbmigrations

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/migrations"
)

// Renames the credential `type` option `nkey` -> `generic`. The type is a
// control-plane label only: it never crosses the policy wire (policykv.Credential
// carries no type) and policy.Decide ignores it, so this is a pure UI/reporting
// rename with no effect on access decisions. `generic` honestly names "an opaque
// string we don't decode," leaving room for real card-format options
// (e.g. wiegand-26/wiegand-37) on a separate field later.
func init() {
	migrations.Register(func(app core.App) error {
		return retypeCredentialOption(app, "nkey", "generic",
			[]string{"generic", "wiegand", "pin", "mobile"})
	}, func(app core.App) error {
		return retypeCredentialOption(app, "generic", "nkey",
			[]string{"nkey", "wiegand", "pin", "mobile"})
	})
}

// retypeCredentialOption widens the credentials `type` select to `values` (so the
// rewritten records validate against the new option set) and then rewrites every
// record holding `from` to `to`. A missing field/collection fails closed — the
// rename simply doesn't apply rather than erroring the boot.
func retypeCredentialOption(app core.App, from, to string, values []string) error {
	credentials, err := app.FindCollectionByNameOrId("credentials")
	if err != nil {
		return err
	}
	if f, ok := credentials.Fields.GetByName("type").(*core.SelectField); ok {
		f.Values = values
	}
	if err := app.Save(credentials); err != nil {
		return err
	}
	recs, err := app.FindRecordsByFilter("credentials", "type = {:t}", "", 0, 0, dbx.Params{"t": from})
	if err != nil {
		return err
	}
	for _, r := range recs {
		r.Set("type", to)
		if err := app.Save(r); err != nil {
			return err
		}
	}
	return nil
}
