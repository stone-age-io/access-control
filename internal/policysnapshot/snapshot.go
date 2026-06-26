// Package policysnapshot builds a read-only, point-in-time snapshot of the policy
// graph from the ACC_POLICY KV entries and answers "would this credential open
// this portal at this instant?" by running the real, shared policy.Decide — the
// same pure function the edge controller runs.
//
// It exists so accessd can offer an access *simulator* (a what-if / commissioning
// tool) WITHOUT importing the edge runtime (internal/controller), which pulls in
// the hardware drivers and the OSDP engine — wrong to link into the central binary.
//
// The decision logic itself is NOT duplicated: policy.Decide and policy.ScheduleOpen
// live in internal/policy and are reused verbatim. What this package re-implements
// is the small, mechanical KV-wire → policy-type mapping that the controller's
// PolicyStore also performs (the two are kept honest by the shared policykv wire
// contract and by this package's own tests). Only the maps the access decision and
// posture resolution need are built — aux inputs, aux outputs, areas, and
// controllers are irrelevant to policy.Decide and are skipped.
package policysnapshot

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/stone-age-io/access-control/internal/policy"
	"github.com/stone-age-io/access-control/internal/policykv"
)

// Posture-source labels reported by Simulate. They match the UI's PostureSource
// type ('standing' | 'scheduled' | 'override') so the frontend can render them
// the same way it renders a live portal's posture source.
const (
	SourceStanding  = "standing"
	SourceScheduled = "scheduled"
	SourceOverride  = "override"
)

// Snapshot is an immutable policy graph plus the per-location timezones the
// decision needs. It owns no I/O and no locks; build it with Build and treat it
// as read-only.
type Snapshot struct {
	graph     policy.Policy
	locs      map[string]*time.Location // location code -> resolved timezone (UTC fallback)
	tzName    map[string]string         // location code -> IANA tz name (for display)
	locations map[string]policykv.Location
	holidays  map[string]policykv.Holiday // keyed by KV holiday id
}

// Result is the outcome of a simulated presentation: the real policy.Decision plus
// the effective posture it was evaluated under and enough context for the UI to
// explain it.
type Result struct {
	Allow         bool   `json:"allow"`
	Reason        string `json:"reason"`
	User          string `json:"user"`
	Pulse         int    `json:"pulse"`
	Posture       string `json:"posture"`       // effective posture fed to Decide
	PostureSource string `json:"postureSource"` // standing | scheduled | override
	PortalKnown   bool   `json:"portalKnown"`
	CredKnown     bool   `json:"credKnown"`
	Location      string `json:"location"` // the portal's location code
	Timezone      string `json:"timezone"` // IANA tz the decision evaluated in
}

// Build assembles a Snapshot from a set of ACC_POLICY KV entries (key -> raw JSON
// value). It is pure and fail-safe: a malformed value is skipped (as if absent),
// mirroring PolicyStore — so a snapshot of partial/corrupt policy denies rather
// than crashes. Entry order is irrelevant; holiday sets are joined at the end.
func Build(entries map[string][]byte) *Snapshot {
	s := &Snapshot{
		graph: policy.Policy{
			Schedules: make(map[string]policy.Schedule),
			Portals:   make(map[string]policy.Portal),
			Users:     make(map[string]policy.User),
			Roles:     make(map[string]policy.Role),
			Groups:    make(map[string]policy.AccessGroup),
			Creds:     make(map[string]policy.Credential),
			Holidays:  make(map[string]policy.HolidaySet),
		},
		locs:      make(map[string]*time.Location),
		tzName:    make(map[string]string),
		locations: make(map[string]policykv.Location),
		holidays:  make(map[string]policykv.Holiday),
	}

	for key, value := range entries {
		switch {
		case strings.HasPrefix(key, policykv.PrefixLocation):
			var w policykv.Location
			if json.Unmarshal(value, &w) != nil {
				continue
			}
			s.locations[w.Code] = w
			loc, err := time.LoadLocation(w.Timezone)
			if err != nil {
				loc = time.UTC // matches PolicyStore: a bad tz falls back to UTC
			}
			s.locs[w.Code] = loc
			s.tzName[w.Code] = w.Timezone

		case strings.HasPrefix(key, policykv.PrefixSched):
			var w policykv.Schedule
			if json.Unmarshal(value, &w) != nil {
				continue
			}
			s.graph.Schedules[w.Code] = toSchedule(w)

		case strings.HasPrefix(key, policykv.PrefixPortal):
			var w policykv.Portal
			if json.Unmarshal(value, &w) != nil {
				continue
			}
			s.graph.Portals[w.Code] = policy.Portal{
				Code: w.Code, Type: w.Type, Location: w.Location,
				Posture: w.Posture, PulseSeconds: w.PulseSeconds,
				AutoPosture: w.AutoPosture, AutoSchedule: w.AutoSchedule,
			}

		case strings.HasPrefix(key, policykv.PrefixGroup):
			var w policykv.AccessGroup
			if json.Unmarshal(value, &w) != nil {
				continue
			}
			s.graph.Groups[w.Code] = policy.AccessGroup{
				Code: w.Code, Portals: toSet(w.Portals), Schedule: w.Schedule,
			}

		case strings.HasPrefix(key, policykv.PrefixRole):
			var w policykv.Role
			if json.Unmarshal(value, &w) != nil {
				continue
			}
			s.graph.Roles[w.Code] = policy.Role{Code: w.Code, Groups: w.Groups}

		case strings.HasPrefix(key, policykv.PrefixUser):
			var w policykv.User
			if json.Unmarshal(value, &w) != nil {
				continue
			}
			s.graph.Users[w.ID] = policy.User{ID: w.ID, Status: w.Status, Roles: w.Roles}

		case strings.HasPrefix(key, policykv.PrefixCred):
			var w policykv.Credential
			if json.Unmarshal(value, &w) != nil {
				continue
			}
			validFrom, ok1 := parseOptionalTime(w.ValidFrom)
			validUntil, ok2 := parseOptionalTime(w.ValidUntil)
			if !ok1 || !ok2 {
				// Fail closed exactly as PolicyStore does: an unparseable bound means a
				// corrupt value, so drop the credential (it reads as unknown) rather
				// than honor a half-parsed validity window.
				continue
			}
			s.graph.Creds[w.Value] = policy.Credential{
				Value: w.Value, User: w.User, Status: w.Status,
				ValidFrom: validFrom, ValidUntil: validUntil,
			}

		case strings.HasPrefix(key, policykv.PrefixHoliday):
			var w policykv.Holiday
			if json.Unmarshal(value, &w) != nil {
				continue
			}
			s.holidays[strings.TrimPrefix(key, policykv.PrefixHoliday)] = w

		default:
			// controller / auxin / auxout / area: irrelevant to the access decision.
		}
	}

	s.rebuildHolidays()
	return s
}

// Simulate runs the real policy.Decide for a credential value at a portal code and
// instant, after resolving the portal's effective posture (override → scheduled →
// standing) exactly as the controller does. A non-empty override forces that
// posture (the what-if "set it to lockdown" case); "" means resolve normally.
func (s *Snapshot) Simulate(cred, portal string, atUTC time.Time, override string) Result {
	r := Result{}
	ap, portalKnown := s.graph.Portals[portal]
	r.PortalKnown = portalKnown
	if portalKnown {
		r.Location = ap.Location
	}
	_, r.CredKnown = s.graph.Creds[cred]

	posture, source := s.resolvePosture(portal, override, atUTC)
	r.Posture, r.PostureSource = posture, source

	loc := time.UTC
	if portalKnown {
		if l, ok := s.locs[ap.Location]; ok && l != nil {
			loc = l
			r.Timezone = s.tzName[ap.Location]
		}
	}

	d := policy.Decide(&s.graph, loc, posture, cred, portal, atUTC)
	r.Allow, r.Reason, r.User, r.Pulse = d.Allow, d.Reason, d.User, d.Pulse
	return r
}

// resolvePosture mirrors PolicyStore.ResolvePosture: a passed override wins, else
// the scheduled auto_posture while its window is open, else the standing posture.
// Unlike the live store there is no "keep previous hold" concern, so an unresolved
// auto_schedule simply falls back to standing.
func (s *Snapshot) resolvePosture(portalCode, override string, atUTC time.Time) (posture, source string) {
	if override != "" {
		return override, SourceOverride
	}
	ap, ok := s.graph.Portals[portalCode]
	if !ok {
		return "", SourceStanding
	}
	if ap.AutoSchedule != "" {
		sched, schedOK := s.graph.Schedules[ap.AutoSchedule]
		loc, locOK := s.locs[ap.Location]
		if schedOK && locOK && loc != nil &&
			policy.ScheduleOpen(sched, loc, atUTC, s.graph.Holidays[ap.Location]) {
			return ap.AutoPosture, SourceScheduled
		}
	}
	return ap.Posture, SourceStanding
}

func toSchedule(w policykv.Schedule) policy.Schedule {
	windows := make([]policy.Window, len(w.Windows))
	for i, win := range w.Windows {
		windows[i] = policy.Window{Days: win.Days, Start: win.Start, End: win.End}
	}
	return policy.Schedule{Windows: windows, ObserveHolidays: w.ObserveHolidays}
}

func toSet(codes []string) map[string]struct{} {
	set := make(map[string]struct{}, len(codes))
	for _, c := range codes {
		set[c] = struct{}{}
	}
	return set
}

// parseOptionalTime parses an optional RFC 3339 timestamp. Empty is a valid
// "unbounded" bound (zero time, ok); a non-empty unparseable value returns ok=false
// so the caller fails closed.
func parseOptionalTime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, true
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t.UTC(), true
}

// rebuildHolidays joins holiday records (grouped by calendar) against each
// location's observed calendars into per-location HolidaySets — the same union the
// controller's PolicyStore performs.
func (s *Snapshot) rebuildHolidays() {
	byCalendar := make(map[string]policy.HolidaySet)
	for _, h := range s.holidays {
		if h.Calendar == "" || len(h.Date) != 10 {
			continue // dangling/malformed: fail-safe skip
		}
		set := byCalendar[h.Calendar]
		if h.Recurring {
			if set.Recurring == nil {
				set.Recurring = make(map[string]struct{})
			}
			set.Recurring[h.Date[5:]] = struct{}{} // "YYYY-MM-DD" -> "MM-DD"
		} else {
			if set.Dates == nil {
				set.Dates = make(map[string]struct{})
			}
			set.Dates[h.Date] = struct{}{}
		}
		byCalendar[h.Calendar] = set
	}

	out := make(map[string]policy.HolidaySet)
	for code, loc := range s.locations {
		var merged policy.HolidaySet
		for _, cal := range loc.HolidayCalendars {
			mergeHolidaySet(&merged, byCalendar[cal])
		}
		if merged.Dates != nil || merged.Recurring != nil {
			out[code] = merged
		}
	}
	s.graph.Holidays = out
}

func mergeHolidaySet(dst *policy.HolidaySet, src policy.HolidaySet) {
	for d := range src.Dates {
		if dst.Dates == nil {
			dst.Dates = make(map[string]struct{})
		}
		dst.Dates[d] = struct{}{}
	}
	for r := range src.Recurring {
		if dst.Recurring == nil {
			dst.Recurring = make(map[string]struct{})
		}
		dst.Recurring[r] = struct{}{}
	}
}
