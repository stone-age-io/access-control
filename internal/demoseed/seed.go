// Package demoseed populates an accessd instance with demo/showcase data for
// Northwind Traders — the same company, and the same three site codes, that the
// Stone Age platform's own `demo-seed` writes.
//
// It runs IN-PROCESS against core.App rather than through an HTTP client, which
// buys three things an external script could not have:
//
//   - The mirror's record hooks fire exactly as they do in production, so a
//     seeded deployment reaches NATS KV the same way an operator's edits do. When
//     accessd is not serving, `SyncAll` reconciles the whole graph on the next
//     boot, which is what covers seeding against a stopped instance.
//   - It cannot drift from the schema. A migration that breaks these fixtures
//     breaks `go test ./...`, unlike a PowerShell script driving the REST API.
//   - No auth dance. Every policy collection here is superuser-only, so an HTTP
//     seeder needs a live superuser session and a running server; this needs
//     neither.
//
// Seeding is IDEMPOTENT. Everything is found-or-created by its natural key — the
// `code` each policy record already carries — so re-running converges rather than
// duplicating, and a run that dies partway heals on the next one. `fill` runs only
// on create, so hand-edits to demo data survive a re-seed.
//
// It seeds NO floorplan images. `locations.floorplan` and
// `portals.floorplan_position` are a matched pair, and a position against an
// absent (or later, differently-sized) plan places markers at meaningless spots.
// KC-OFFICE is opted into `badge_floorplan` so the feature is one upload away.
package demoseed

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// Options controls the generated bulk. The curated fixtures are always seeded in
// full; Events is a floor for the event history, not a cap on it.
type Options struct {
	// Events is the number of backdated access events to converge on.
	Events int

	// Seed makes generation reproducible. Keeping it stable is what makes
	// re-runs idempotent, so there is rarely a reason to change it.
	Seed int64

	// Now is the instant the fixtures are dated relative to. Zero means
	// time.Now(). Injected so a test can assert on a fixed window.
	Now time.Time

	// Log receives progress lines. Nil discards them.
	Log func(string, ...any)
}

// Result reports what the run did.
type Result struct {
	Created map[string]int
	Matched map[string]int
}

func (r *Result) mark(collection string, created bool) {
	if created {
		r.Created[collection]++
		return
	}
	r.Matched[collection]++
}

// Summary renders the per-collection counts in a stable order.
func (r *Result) Summary() string {
	seen := map[string]bool{}
	var keys []string
	for _, m := range []map[string]int{r.Created, r.Matched} {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "  %-20s %4d created  %4d existing\n", k, r.Created[k], r.Matched[k])
	}
	return b.String()
}

type seeder struct {
	app  core.App
	opts Options
	rng  *rand.Rand
	res  *Result
	now  time.Time

	calendars   map[string]string // code -> id
	locations   map[string]string
	schedules   map[string]string
	controllers map[string]string
	areas       map[string]string
	portals     map[string]string
	auxOutputs  map[string]string
	groups      map[string]string
	roles       map[string]string
	holders     map[string]string // external_id -> id
}

// Run seeds the app. Safe to call repeatedly.
func Run(app core.App, opts Options) (*Result, error) {
	if opts.Events <= 0 {
		opts.Events = 240
	}
	if opts.Seed == 0 {
		opts.Seed = 20260831
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	if opts.Log == nil {
		opts.Log = func(string, ...any) {}
	}

	s := &seeder{
		app: app, opts: opts,
		rng: rand.New(rand.NewSource(opts.Seed)),
		res: &Result{Created: map[string]int{}, Matched: map[string]int{}},
		now: opts.Now.UTC(),

		calendars: map[string]string{}, locations: map[string]string{},
		schedules: map[string]string{}, controllers: map[string]string{},
		areas: map[string]string{}, portals: map[string]string{},
		auxOutputs: map[string]string{}, groups: map[string]string{},
		roles: map[string]string{}, holders: map[string]string{},
	}

	// Order is load-bearing at every step: a portal names an area, an area names
	// a schedule, a group names portals/areas/outputs, a role names groups, and a
	// cardholder names roles. Each phase resolves what the next one references.
	steps := []struct {
		name string
		fn   func() error
	}{
		{"holiday calendars", s.seedCalendars},
		{"locations", s.seedLocations},
		{"schedules", s.seedSchedules},
		{"controllers", s.seedControllers},
		{"areas", s.seedAreas},
		{"portals", s.seedPortals},
		{"aux points", s.seedAux},
		{"access groups", s.seedGroups},
		{"roles", s.seedRoles},
		{"people and credentials", s.seedCardholders},
		{"event history", s.seedEvents},
	}
	for _, step := range steps {
		opts.Log("seeding %s...", step.name)
		if err := step.fn(); err != nil {
			return s.res, fmt.Errorf("%s: %w", step.name, err)
		}
	}
	return s.res, nil
}

// ------------------------------------------------------------------ helpers

// ensure is the found-or-create primitive, keyed on the natural code every policy
// record carries. `fill` runs only when the record is new.
func (s *seeder) ensure(collection, filter string, params dbx.Params, fill func(*core.Record)) (*core.Record, bool, error) {
	if existing, err := s.app.FindFirstRecordByFilter(collection, filter, params); err == nil && existing != nil {
		s.res.mark(collection, false)
		return existing, false, nil
	}
	c, err := s.app.FindCollectionByNameOrId(collection)
	if err != nil {
		return nil, false, fmt.Errorf("find collection %s: %w", collection, err)
	}
	rec := core.NewRecord(c)
	fill(rec)
	if err := s.app.Save(rec); err != nil {
		return nil, false, fmt.Errorf("save %s: %w", collection, err)
	}
	s.res.mark(collection, true)
	return rec, true, nil
}

// byCode resolves a fixture reference, failing loudly. A dangling reference here
// renders as an empty section in the UI rather than an error, so it is worth
// catching at seed time.
func byCode(m map[string]string, kind, code string) (string, error) {
	id, ok := m[code]
	if !ok {
		return "", fmt.Errorf("unknown %s %q", kind, code)
	}
	return id, nil
}

func (s *seeder) idsFor(m map[string]string, kind string, codes []string) ([]string, error) {
	out := make([]string, 0, len(codes))
	for _, c := range codes {
		id, err := byCode(m, kind, c)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

// at returns a timestamp `days` from now at a plausible hour, jittered so a
// generated batch does not stack on one minute.
func (s *seeder) at(days int, loHour, hiHour int) time.Time {
	d := s.now.AddDate(0, 0, days)
	h := loHour + s.rng.Intn(hiHour-loHour+1)
	return time.Date(d.Year(), d.Month(), d.Day(), h, s.rng.Intn(60), s.rng.Intn(60), 0, time.UTC)
}

// ------------------------------------------------------------ holiday calendars

func (s *seeder) seedCalendars() error {
	for _, c := range calendars {
		rec, _, err := s.ensure("holiday_calendars", "code = {:c}", dbx.Params{"c": c.Code},
			func(r *core.Record) {
				r.Set("code", c.Code)
				r.Set("name", c.Name)
			})
		if err != nil {
			return err
		}
		s.calendars[c.Code] = rec.Id

		for _, h := range c.Dates {
			if _, _, err := s.ensure("holidays", "calendar = {:cal} && name = {:n}",
				dbx.Params{"cal": rec.Id, "n": h.Name}, func(r *core.Record) {
					r.Set("calendar", rec.Id)
					r.Set("name", h.Name)
					r.Set("date", h.Date)
					r.Set("recurring", h.Recurring)
				}); err != nil {
				return err
			}
		}
	}
	return nil
}

// ------------------------------------------------------------------ locations

func (s *seeder) seedLocations() error {
	for _, l := range locations {
		calIDs, err := s.idsFor(s.calendars, "holiday calendar", l.Calendars)
		if err != nil {
			return err
		}

		rec, _, err := s.ensure("locations", "code = {:c}", dbx.Params{"c": l.Code},
			func(r *core.Record) {
				r.Set("code", l.Code)
				r.Set("name", l.Name)
				r.Set("description", l.Description)
				r.Set("timezone", l.Timezone)
				r.Set("fai_suppress", l.FAISuppress)
				r.Set("notify_fire", l.NotifyFire)
				r.Set("badge_floorplan", l.BadgeFloorplan)
				r.Set("coordinates", types.GeoPoint{Lat: l.Lat, Lon: l.Lon})
				if len(calIDs) > 0 {
					r.Set("holiday_calendars", calIDs)
				}
			})
		if err != nil {
			return err
		}
		s.locations[l.Code] = rec.Id
	}
	return nil
}

// ------------------------------------------------------------------ schedules

func (s *seeder) seedSchedules() error {
	for _, sc := range schedules {
		windows := make([]map[string]any, 0, len(sc.Windows))
		for _, w := range sc.Windows {
			windows = append(windows, map[string]any{
				"days": w.Days, "start": w.Start, "end": w.End,
			})
		}

		rec, _, err := s.ensure("schedules", "code = {:c}", dbx.Params{"c": sc.Code},
			func(r *core.Record) {
				r.Set("code", sc.Code)
				r.Set("name", sc.Name)
				r.Set("windows", windows)
				r.Set("ignore_holidays", sc.IgnoreHolidays)
			})
		if err != nil {
			return err
		}
		s.schedules[sc.Code] = rec.Id
	}
	return nil
}

// ---------------------------------------------------------------- controllers

func (s *seeder) seedControllers() error {
	for _, c := range controllers {
		locID, err := byCode(s.locations, "location", c.Location)
		if err != nil {
			return err
		}

		rec, _, err := s.ensure("controllers", "code = {:c}", dbx.Params{"c": c.Code},
			func(r *core.Record) {
				r.Set("code", c.Code)
				r.Set("name", c.Name)
				r.Set("location", locID)
				r.Set("model", c.Model)
				r.Set("notify_offline", c.NotifyOffline)
				// `status` and `last_seen` are written by the health monitor from
				// real heartbeats. Left unset on purpose: a seeded "online" would
				// claim a controller is reachable when nothing has ever beaten,
				// and the offline state is the honest one for hardware that does
				// not exist.
			})
		if err != nil {
			return err
		}
		s.controllers[c.Code] = rec.Id
	}
	return nil
}

// --------------------------------------------------------------------- areas

func (s *seeder) seedAreas() error {
	for _, a := range areas {
		locID, err := byCode(s.locations, "location", a.Location)
		if err != nil {
			return err
		}
		var schedID string
		if a.AutoSchedule != "" {
			if schedID, err = byCode(s.schedules, "schedule", a.AutoSchedule); err != nil {
				return err
			}
		}

		rec, _, err := s.ensure("areas", "code = {:c}", dbx.Params{"c": a.Code},
			func(r *core.Record) {
				r.Set("code", a.Code)
				r.Set("name", a.Name)
				r.Set("location", locID)
				r.Set("arm", a.Arm)
				if a.AutoArm != "" {
					r.Set("auto_arm", a.AutoArm)
				}
				if schedID != "" {
					r.Set("auto_schedule", schedID)
				}
				r.Set("notify_on_alarm", a.NotifyOnAlarm)
				r.Set("allow_remote_arm", a.AllowRemoteArm)
				// arm_override is deliberately left empty. It is a runtime
				// override an operator or the entry-disarm sink sets; a seeded
				// one would pin the area against its own schedule.
			})
		if err != nil {
			return err
		}
		s.areas[a.Code] = rec.Id
	}
	return nil
}

// ------------------------------------------------------------------- portals

func (s *seeder) seedPortals() error {
	for _, p := range portals {
		locID, err := byCode(s.locations, "location", p.Location)
		if err != nil {
			return err
		}
		ctrlID, err := byCode(s.controllers, "controller", p.Controller)
		if err != nil {
			return err
		}
		var areaID, schedID string
		if p.Area != "" {
			if areaID, err = byCode(s.areas, "area", p.Area); err != nil {
				return err
			}
		}
		if p.AutoSchedule != "" {
			if schedID, err = byCode(s.schedules, "schedule", p.AutoSchedule); err != nil {
				return err
			}
		}

		rec, _, err := s.ensure("portals", "code = {:c}", dbx.Params{"c": p.Code},
			func(r *core.Record) {
				r.Set("code", p.Code)
				r.Set("name", p.Name)
				r.Set("type", p.Type)
				r.Set("location", locID)
				r.Set("controller", ctrlID)
				r.Set("posture", p.Posture)
				r.Set("pulse_seconds", p.PulseSeconds)
				r.Set("lock_relay", p.LockRelay)
				r.Set("dps_input", p.DPSInput)
				r.Set("rex_input", p.RexInput)
				r.Set("held_open_seconds", p.HeldOpenSeconds)
				r.Set("lock_type", p.LockType)
				r.Set("dps_contact", p.DPSContact)
				r.Set("rex_contact", p.RexContact)
				r.Set("rex_unlock", p.RexUnlock)
				r.Set("reader_address", p.ReaderAddress)
				r.Set("disarm_on_grant", p.DisarmOnGrant)
				r.Set("allow_remote_unlock", p.AllowRemoteUnlock)
				r.Set("notify_on_alarm", p.NotifyOnAlarm)
				if areaID != "" {
					r.Set("area", areaID)
				}
				if p.AutoPosture != "" {
					r.Set("auto_posture", p.AutoPosture)
					r.Set("auto_schedule", schedID)
				}
			})
		if err != nil {
			return err
		}
		s.portals[p.Code] = rec.Id
	}
	return nil
}

// ---------------------------------------------------------------- aux points

func (s *seeder) seedAux() error {
	for _, in := range auxInputs {
		locID, err := byCode(s.locations, "location", in.Location)
		if err != nil {
			return err
		}
		ctrlID, err := byCode(s.controllers, "controller", in.Controller)
		if err != nil {
			return err
		}
		var areaID string
		if in.Area != "" {
			if areaID, err = byCode(s.areas, "area", in.Area); err != nil {
				return err
			}
		}

		if _, _, err := s.ensure("aux_input", "code = {:c}", dbx.Params{"c": in.Code},
			func(r *core.Record) {
				r.Set("code", in.Code)
				r.Set("name", in.Name)
				r.Set("location", locID)
				r.Set("controller", ctrlID)
				r.Set("input_index", in.InputIndex)
				r.Set("point_type", in.Kind)
				r.Set("contact", in.Contact)
				if areaID != "" {
					r.Set("area", areaID)
				}
			}); err != nil {
			return err
		}
	}

	for _, out := range auxOutputs {
		locID, err := byCode(s.locations, "location", out.Location)
		if err != nil {
			return err
		}
		ctrlID, err := byCode(s.controllers, "controller", out.Controller)
		if err != nil {
			return err
		}

		rec, _, err := s.ensure("aux_output", "code = {:c}", dbx.Params{"c": out.Code},
			func(r *core.Record) {
				r.Set("code", out.Code)
				r.Set("name", out.Name)
				r.Set("location", locID)
				r.Set("controller", ctrlID)
				r.Set("relay_index", out.RelayIndex)
				r.Set("pulse_seconds", out.PulseSeconds)
				r.Set("allow_remote", out.AllowRemote)
			})
		if err != nil {
			return err
		}
		s.auxOutputs[out.Code] = rec.Id
	}
	return nil
}

// ------------------------------------------------------------- access groups

func (s *seeder) seedGroups() error {
	for _, g := range groups {
		schedID, err := byCode(s.schedules, "schedule", g.Schedule)
		if err != nil {
			return err
		}
		portalIDs, err := s.idsFor(s.portals, "portal", g.Portals)
		if err != nil {
			return err
		}
		areaIDs, err := s.idsFor(s.areas, "area", g.Areas)
		if err != nil {
			return err
		}
		outputIDs, err := s.idsFor(s.auxOutputs, "aux output", g.AuxOutputs)
		if err != nil {
			return err
		}

		rec, _, err := s.ensure("access_groups", "code = {:c}", dbx.Params{"c": g.Code},
			func(r *core.Record) {
				r.Set("code", g.Code)
				r.Set("name", g.Name)
				r.Set("schedule", schedID)
				r.Set("portals", portalIDs)
				r.Set("areas", areaIDs)
				r.Set("aux_outputs", outputIDs)
				if len(g.AreaRights) > 0 {
					r.Set("area_rights", g.AreaRights)
				}
			})
		if err != nil {
			return err
		}
		s.groups[g.Code] = rec.Id
	}
	return nil
}

// --------------------------------------------------------------------- roles

func (s *seeder) seedRoles() error {
	for _, role := range roles {
		groupIDs, err := s.idsFor(s.groups, "access group", role.Groups)
		if err != nil {
			return err
		}

		rec, _, err := s.ensure("roles", "code = {:c}", dbx.Params{"c": role.Code},
			func(r *core.Record) {
				r.Set("code", role.Code)
				r.Set("name", role.Name)
				r.Set("access_groups", groupIDs)
			})
		if err != nil {
			return err
		}
		s.roles[role.Code] = rec.Id
	}
	return nil
}

// --------------------------------------------------- cardholders + credentials

func (s *seeder) seedCardholders() error {
	for _, ch := range cardholders {
		roleIDs, err := s.idsFor(s.roles, "role", ch.Roles)
		if err != nil {
			return err
		}

		rec, _, err := s.ensure("cardholders", "external_id = {:x}", dbx.Params{"x": ch.ExternalID},
			func(r *core.Record) {
				r.Set("external_id", ch.ExternalID)
				r.Set("name", ch.Name)
				r.Set("status", ch.Status)
				r.Set("kind", ch.Kind)
				r.Set("roles", roleIDs)
				// cardholders is an AUTH collection, so PocketBase requires a
				// non-blank password on every record whether or not the person can
				// ever sign in. Record.SetPassword is just Set("password", ...);
				// the field hashes on save.
				r.Set("password", DemoPassword)
				if ch.Email != "" {
					r.Set("email", ch.Email)
					r.Set("emailVisibility", true)
					r.Set("verified", true)
					r.Set("badge_login", ch.BadgeLogin)
					r.Set("password_set", true)
				}
				// A holder with no email gets no badge_login and no password_set:
				// it cannot sign in by any method, which is the whole point of the
				// drawer cards below.
			})
		if err != nil {
			return err
		}
		s.holders[ch.ExternalID] = rec.Id

		if ch.Credential == "" {
			continue
		}
		if _, _, err := s.ensure("credentials", "value = {:v}", dbx.Params{"v": ch.Credential},
			func(r *core.Record) {
				r.Set("value", ch.Credential)
				r.Set("type", ch.CredentialType)
				r.Set("user", rec.Id)
				r.Set("status", ch.CredentialStatus)
				r.Set("label", ch.CredentialLabel)
				if ch.Bounded {
					r.Set("valid_from", s.now.AddDate(0, 0, ch.ValidFromDays).Format(time.RFC3339))
					r.Set("valid_until", s.now.AddDate(0, 0, ch.ValidUntilDays).Format(time.RFC3339))
				}
			}); err != nil {
			return err
		}
	}
	return nil
}
