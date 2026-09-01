package demoseed

import (
	"fmt"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/spf13/cobra"
)

// RegisterCommand wires the `demo-seed` subcommand onto accessd's root command.
//
// Guarded behind --confirm on purpose: this ships in the binary an operator runs
// in production, and `accessd demo-seed` typo'd against a live system would put
// fictional PEOPLE WITH WORKING CREDENTIALS on real doors. The flag is the whole
// safety mechanism. Keep it required.
//
//	./accessd demo-seed --confirm
//	./accessd demo-seed --confirm --events 800
func RegisterCommand(app *pocketbase.PocketBase) {
	cmd := &cobra.Command{
		Use:   "demo-seed",
		Short: "Populate this instance with demo/showcase data (idempotent)",
		Long: "Seeds Northwind Traders: three sites, four controllers, ten portals, four " +
			"armed areas, aux inputs and outputs, schedules and a holiday calendar, " +
			"access groups with arm/disarm rights, roles, cardholders with badge logins, " +
			"visitor passes, and a backdated event history with open alarms.\n\n" +
			"The three site codes are the ones the Stone Age platform's own demo-seed " +
			"writes for its Northwind organization, so the two demos describe one " +
			"company.\n\n" +
			"Idempotent: re-running tops up rather than duplicating, and a run that fails " +
			"partway heals on the next one.\n\n" +
			"NOT for production installs — every record is fictional, and the people it " +
			"creates hold working credentials on real doors.",
		// Every error below explains what to do next; cobra's default is to
		// follow that with the full flag list, which pushes the explanation off a
		// short terminal and answers a question nobody asked.
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			confirm, _ := cmd.Flags().GetBool("confirm")
			events, _ := cmd.Flags().GetInt("events")
			seed, _ := cmd.Flags().GetInt64("seed")

			if !confirm {
				return fmt.Errorf(
					"refusing to seed without --confirm\n\n"+
						"This creates %d fictional cardholders holding WORKING CREDENTIALS on\n"+
						"every door it seeds, and %d of them can sign in with the password %q.\n"+
						"Never run it against a production access control system.",
					len(cardholders), badgeLoginCount(), DemoPassword)
			}

			if err := preflight(app); err != nil {
				return err
			}

			res, err := Run(app, Options{
				Events: events,
				Seed:   seed,
				Log:    func(format string, args ...any) { fmt.Printf(format+"\n", args...) },
			})
			if res != nil {
				fmt.Print("\n", res.Summary())
			}
			if err != nil {
				return err
			}

			fmt.Printf("\ndone — every demo badge login uses password %q\n", DemoPassword)
			fmt.Println("try:  dana@northwind.example    (staff + IT, all three sites)")
			fmt.Println("      elena@northwind.example  (warehouse: can arm AND disarm)")
			fmt.Println("      priya@northwind.example  (cleaning: can arm, CANNOT disarm)")
			fmt.Println("\nIf accessd is not running, the policy graph reaches NATS KV on its")
			fmt.Println("next `serve` — SyncAll reconciles the whole graph on boot.")
			return nil
		},
	}

	cmd.Flags().Bool("confirm", false, "required: acknowledge this writes fictional data into this instance")
	cmd.Flags().Int("events", 240, "backdated access events to converge on")
	cmd.Flags().Int64("seed", 20260831, "PRNG seed; changing it regenerates different (but still stable) history")

	app.RootCmd.AddCommand(cmd)
}

func badgeLoginCount() int {
	n := 0
	for _, ch := range cardholders {
		if ch.BadgeLogin && ch.Email != "" {
			n++
		}
	}
	return n
}

// preflight refuses to run against a database the seed would silently corrupt,
// and says which step is missing.
//
// accessd runs migrations on `serve`, not on a subcommand, so a fresh data
// directory reaches `demo-seed` with no collections at all. PocketBase discards a
// write to a field that does not exist without an error, so a seed run before the
// migrations would report success and leave records missing exactly the fields
// the policy graph is built on.
func preflight(app core.App) error {
	required := []struct {
		collection string
		fields     []string
	}{
		{"locations", []string{"code", "timezone", "holiday_calendars"}},
		{"portals", []string{"code", "area", "allow_remote_unlock", "lock_type"}},
		{"areas", []string{"code", "auto_arm", "allow_remote_arm"}},
		{"aux_input", []string{"code", "point_type", "contact"}},
		{"aux_output", []string{"code", "allow_remote"}},
		{"access_groups", []string{"portals", "areas", "aux_outputs", "area_rights"}},
		{"cardholders", []string{"external_id", "kind", "badge_login"}},
		{"credentials", []string{"value", "valid_from", "valid_until"}},
		{"events", []string{"kind", "source", "acknowledged"}},
	}
	for _, r := range required {
		col, err := app.FindCollectionByNameOrId(r.collection)
		if err != nil {
			return fmt.Errorf(
				"collection %q does not exist — this data directory has no schema.\n"+
					"Run `accessd migrate up` (or start `accessd serve` once) before seeding.",
				r.collection)
		}
		for _, f := range r.fields {
			if col.Fields.GetByName(f) == nil {
				return fmt.Errorf(
					"%s.%s does not exist — the migrations are behind this binary.\n"+
						"Run `accessd migrate up` before seeding; PocketBase drops writes to\n"+
						"missing fields silently, so seeding now would report success and\n"+
						"write records the policy graph cannot use.",
					r.collection, f)
			}
		}
	}
	return nil
}
