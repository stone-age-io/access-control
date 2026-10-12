package authz

import (
	"fmt"

	"github.com/pocketbase/pocketbase/core"
)

// AllCapabilities is every value users.permissions accepts.
var AllCapabilities = []string{CapEnroll, CapPolicy, CapTopology, CapCommand, CapOperators}

// UpsertOperator makes email an operator with password and every capability,
// creating the users record if there is none. It is how a fresh install gets its
// first operator (`accessd operator upsert`) and how a locked-out one gets back
// in, so it grants everything either way: whoever can run it already holds the
// database file.
//
// It refuses to write before the migrations that define users.permissions have
// run. PocketBase drops writes to fields that do not exist, so it would
// otherwise report success and leave an operator who can change nothing.
func UpsertOperator(app core.App, email, password string) (created bool, err error) {
	users, err := app.FindCollectionByNameOrId("users")
	if err != nil || users.Fields.GetByName("permissions") == nil {
		return false, fmt.Errorf("users.permissions does not exist yet: run `accessd migrate up` first")
	}

	rec, err := app.FindAuthRecordByEmail(users, email)
	if err != nil {
		rec = core.NewRecord(users)
		rec.SetEmail(email)
		created = true
	}
	rec.SetPassword(password)
	rec.SetVerified(true)
	rec.Set("permissions", AllCapabilities)
	if err := app.Save(rec); err != nil {
		return false, err
	}
	return created, nil
}
