package main

import (
	"fmt"

	"github.com/pocketbase/pocketbase"
	"github.com/spf13/cobra"
	"github.com/stone-age-io/access-control/internal/authz"
)

// registerOperatorCommand wires `accessd operator upsert <email> <password>`,
// the console's counterpart to PocketBase's `superuser upsert`. A fresh database
// has no operator: the migrations used to seed admin@local.dev / changeme123 on
// any empty database, production included, so the first one is made here.
func registerOperatorCommand(app *pocketbase.PocketBase) {
	cmd := &cobra.Command{
		Use:   "operator",
		Short: "Manage console operators",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "upsert <email> <password>",
		Short: "Create or update a console operator with every capability",
		Long: "Creates the operator, or sets an existing one's password, and grants every " +
			"capability (enroll, policy, topology, command, operators). Use it for the first " +
			"operator on a new install and to recover a locked-out one; narrow capabilities " +
			"afterwards in the console under Operators.\n\n" +
			"Run `accessd migrate up` (or start `accessd serve` once) first.",
		Example:      "  ./accessd operator upsert you@example.com 'a-long-password'",
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, args []string) error {
			created, err := authz.UpsertOperator(app, args[0], args[1])
			if err != nil {
				return err
			}
			if created {
				fmt.Printf("created operator %s with every capability\n", args[0])
			} else {
				fmt.Printf("updated operator %s: new password, every capability\n", args[0])
			}
			return nil
		},
	})
	app.RootCmd.AddCommand(cmd)
}
