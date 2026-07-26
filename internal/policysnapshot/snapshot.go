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
// contract and by this package's own tests). It builds the maps the access decision
// and posture resolution need, plus areas — so accessd can resolve an area's
// scheduled arm-state centrally (ShouldReleaseDisarm), the same way the controller
// does, without importing the edge runtime. Aux inputs/outputs and controllers are
// irrelevant to both and are skipped.
package policysnapshot

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
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
	areas     map[string]policykv.Area    // area code -> area (for BaseArmState)
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
			Areas:     make(map[string]policy.Area),
			Outputs:   make(map[string]policy.Output),
		},
		locs:      make(map[string]*time.Location),
		tzName:    make(map[string]string),
		locations: make(map[string]policykv.Location),
		holidays:  make(map[string]policykv.Holiday),
		areas:     make(map[string]policykv.Area),
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
			canArm, canDisarm := policy.ArmRights(w.AreaRights)
			s.graph.Groups[w.Code] = policy.AccessGroup{
				Code: w.Code, Portals: toSet(w.Portals), Schedule: w.Schedule,
				Areas: toSet(w.Areas), Outputs: toSet(w.AuxOutputs),
				CanArm: canArm, CanDisarm: canDisarm,
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

		case strings.HasPrefix(key, policykv.PrefixArea):
			var w policykv.Area
			if json.Unmarshal(value, &w) != nil {
				continue
			}
			s.areas[w.Code] = w
			// Two copies with different jobs: s.areas keeps the arm-state fields
			// ShouldReleaseDisarm needs, while the graph copy carries only what an
			// AUTHORIZATION decision may see (existence + location).
			s.graph.Areas[w.Code] = policy.Area{Code: w.Code, Location: w.Location}

		case strings.HasPrefix(key, policykv.PrefixAuxOutput):
			// Now relevant: an access group can grant an aux output (1750000037), so
			// DecideOutput needs to know the output exists and where it is.
			var w policykv.AuxOutput
			if json.Unmarshal(value, &w) != nil {
				continue
			}
			s.graph.Outputs[w.Code] = policy.Output{Code: w.Code, Location: w.Location}

		default:
			// controller / auxin: irrelevant to decision, arm-state and grants.
		}
	}

	s.rebuildHolidays()
	return s
}

// SnapshotKV reads the current ACC_POLICY keyspace into the key→value map Build
// consumes. It drains a WatchAll: the watcher re-delivers each key's latest value
// and then a nil sentinel marking "all current keys delivered" — exactly the
// snapshot the controller's PolicyStore syncs on boot, without leaving a watch
// running.
//
// The cost is proportional to the whole keyspace, so a caller on a user-facing path
// (as opposed to an operator pressing "simulate") should cache the result for a few
// seconds rather than draining per request.
func SnapshotKV(ctx context.Context, kv jetstream.KeyValue) (map[string][]byte, error) {
	w, err := kv.WatchAll(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = w.Stop() }()

	out := make(map[string][]byte)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case entry, ok := <-w.Updates():
			if !ok {
				return out, nil
			}
			if entry == nil {
				return out, nil // initial sync complete
			}
			switch entry.Operation() {
			case jetstream.KeyValuePut:
				out[entry.Key()] = entry.Value()
			case jetstream.KeyValueDelete, jetstream.KeyValuePurge:
				delete(out, entry.Key())
			}
		}
	}
}

// PortalsFor returns the portal codes the given cardholder id can be granted at,
// walking the same user → roles → groups → portals path policy.Decide walks — but
// WITHOUT the schedule/posture/credential checks, so it answers "which doors are on
// this person's badge at all", not "which would open right now".
//
// It exists for the badge view's door list. Ordering is not defined (map iteration),
// so callers that display it should sort.
func (s *Snapshot) PortalsFor(userID string) []string {
	seen := make(map[string]struct{})
	s.walkGroups(userID, func(g policy.AccessGroup) {
		for portalCode := range g.Portals {
			if _, known := s.graph.Portals[portalCode]; known {
				seen[portalCode] = struct{}{}
			}
		}
	})
	out := make([]string, 0, len(seen))
	for code := range seen {
		out = append(out, code)
	}
	return out
}

// AreasFor returns the area codes the given cardholder id holds any arm right over,
// paired with which rights those are — the area equivalent of PortalsFor, and like it
// WITHOUT the schedule/credential checks, so it answers "which areas are on this
// person's badge at all", not "which could they arm right now".
//
// An area reachable through a group that carries no arm right is omitted entirely:
// showing it with both buttons disabled would tell a holder about an area they have
// no authority over, and the operator's fix for that misconfiguration is on the group,
// not the badge. Ordering is undefined (map iteration); callers that display it sort.
func (s *Snapshot) AreasFor(userID string) map[string]ArmRights {
	out := make(map[string]ArmRights)
	s.walkGroups(userID, func(g policy.AccessGroup) {
		if !g.CanArm && !g.CanDisarm {
			return
		}
		for areaCode := range g.Areas {
			if _, known := s.graph.Areas[areaCode]; !known {
				continue // KV ahead of the DB, or mid-rename
			}
			// Union across groups: two groups granting different rights on one area
			// give the holder both, exactly as two groups granting one portal give
			// the union of their schedules.
			r := out[areaCode]
			r.CanArm = r.CanArm || g.CanArm
			r.CanDisarm = r.CanDisarm || g.CanDisarm
			out[areaCode] = r
		}
	})
	return out
}

// ArmRights is which arm actions a badge holds over one area, unioned across every
// group that grants it.
type ArmRights struct {
	CanArm    bool `json:"canArm"`
	CanDisarm bool `json:"canDisarm"`
}

// OutputsFor returns the aux-output codes on the given cardholder's badge — the
// output equivalent of PortalsFor, with the same caveats.
func (s *Snapshot) OutputsFor(userID string) []string {
	seen := make(map[string]struct{})
	s.walkGroups(userID, func(g policy.AccessGroup) {
		for code := range g.Outputs {
			if _, known := s.graph.Outputs[code]; known {
				seen[code] = struct{}{}
			}
		}
	})
	out := make([]string, 0, len(seen))
	for code := range seen {
		out = append(out, code)
	}
	return out
}

// walkGroups calls fn once per group reachable from the given cardholder id, skipping
// dangling role/group references (fail-safe: they contribute nothing). A group reached
// through two roles is visited twice; every caller unions, so that is harmless.
func (s *Snapshot) walkGroups(userID string, fn func(policy.AccessGroup)) {
	u, ok := s.graph.Users[userID]
	if !ok {
		return
	}
	for _, roleCode := range u.Roles {
		role, ok := s.graph.Roles[roleCode]
		if !ok {
			continue
		}
		for _, groupCode := range role.Groups {
			if group, ok := s.graph.Groups[groupCode]; ok {
				fn(group)
			}
		}
	}
}

// SimulateArea runs the real policy.DecideArea for a credential value, area code and
// arm action, resolving the area's timezone the way Simulate resolves a portal's.
// Returns the pure Decision — there is no posture to report and no strike to pulse.
func (s *Snapshot) SimulateArea(cred, area, action string, atUTC time.Time) policy.Decision {
	return policy.DecideArea(&s.graph, s.tzFor(s.areaLocation(area)), cred, area, action, atUTC)
}

// SimulateOutput runs the real policy.DecideOutput for a credential value and
// aux-output code.
func (s *Snapshot) SimulateOutput(cred, output string, atUTC time.Time) policy.Decision {
	loc := ""
	if o, ok := s.graph.Outputs[output]; ok {
		loc = o.Location
	}
	return policy.DecideOutput(&s.graph, s.tzFor(loc), cred, output, atUTC)
}

// AreaLocation returns an area's location code and whether the area is known —
// needed to address a command subject without re-reading PocketBase.
func (s *Snapshot) AreaLocation(areaCode string) (string, bool) {
	a, ok := s.graph.Areas[areaCode]
	if !ok {
		return "", false
	}
	return a.Location, true
}

// OutputLocation returns an aux output's location code and whether it is known.
func (s *Snapshot) OutputLocation(code string) (string, bool) {
	o, ok := s.graph.Outputs[code]
	if !ok {
		return "", false
	}
	return o.Location, true
}

func (s *Snapshot) areaLocation(areaCode string) string {
	if a, ok := s.graph.Areas[areaCode]; ok {
		return a.Location
	}
	return ""
}

// tzFor resolves a location code's timezone, falling back to UTC for an unknown or
// unresolvable one — the same fallback Build and PolicyStore apply, so a decision
// never fails for want of a timezone.
func (s *Snapshot) tzFor(locCode string) *time.Location {
	if l, ok := s.locs[locCode]; ok && l != nil {
		return l
	}
	return time.UTC
}

// PortalLocation returns a portal's location code and whether the portal is known.
// Needed to address a command subject without re-reading PocketBase.
func (s *Snapshot) PortalLocation(portalCode string) (string, bool) {
	p, ok := s.graph.Portals[portalCode]
	if !ok {
		return "", false
	}
	return p.Location, true
}

// PortalType returns a portal's type token (the {type} subject segment) and whether
// the portal is known.
func (s *Snapshot) PortalType(portalCode string) (string, bool) {
	p, ok := s.graph.Portals[portalCode]
	if !ok {
		return "", false
	}
	return p.Type, true
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

// ShouldReleaseDisarm reports whether a disarm override on the given area should be
// released now — the policy half of accessd's one-shot disarm (see internal/armrelease).
// It is true only when the area is SCHEDULED (has an auto_schedule) and its base
// arm-state (schedule+standing, override excluded) currently resolves to DISARMED.
//
//   - An area with no auto_schedule is never released here: with no scheduled arm to
//     revert to, its disarm override is sticky until an operator clears it. (The
//     snapshot reflects the mirror's both-or-neither rule, so an auto_schedule set
//     without an auto_arm reads as no schedule here — correctly not released.)
//   - false on unknown/unresolved (KV lag, schedule/location not yet loaded) — fail-safe
//     keep the override, the same "keep previous" ResolveArmState applies.
//
// It never inspects the override itself: the caller (which holds the authoritative
// PocketBase record) has already selected areas whose durable arm_override is disarmed.
func (s *Snapshot) ShouldReleaseDisarm(areaCode string, atUTC time.Time) bool {
	if a, ok := s.areas[areaCode]; !ok || a.AutoSchedule == "" {
		return false
	}
	armed, resolved := s.baseArmState(areaCode, atUTC)
	return resolved && !armed
}

// BaseArmState is baseArmState exported: an area's scheduled/standing arm-state at atUTC
// with the durable arm_override EXCLUDED.
//
// Callers that want the effective state apply the override themselves, and accessd's badge
// routes do that from the authoritative PocketBase record rather than from this snapshot's
// copy. That is not pedantry — a holder who has just disarmed an area re-reads their badge
// immediately, and the mirror plus this package's few-second snapshot cache would still be
// reporting "armed", which reads as "it didn't work". The override is the one field a badge
// holder can change, so it is the one where staleness is visible; the scheduled/standing
// tiers below change only when an operator edits them, where a few seconds of lag is
// invisible.
//
// What this resolves to is POLICY INTENT, not a report from the hardware. The authoritative
// live state is the per-controller arm shadow in ACC_STATUS, which the operator console
// reads and which can say "a box never reported" — not something a badge holder has any use
// for. So callers rendering this to a holder should avoid wording that asserts the hardware
// agrees.
//
// resolved is false when the area is unknown here or a configured auto_schedule has not
// loaded yet; a caller must treat that as "don't know" rather than "disarmed".
func (s *Snapshot) BaseArmState(areaCode string, atUTC time.Time) (armed, resolved bool) {
	return s.baseArmState(areaCode, atUTC)
}

// baseArmState resolves an area's BASE arm-state at atUTC — the scheduled/standing
// arm-state with the durable arm_override EXCLUDED. It mirrors the scheduled→standing
// tiers of the controller's PolicyStore.ResolveArmState (via the same
// policy.ScheduleOpen), deliberately dropping the override tier so ShouldReleaseDisarm
// can decide whether the base is still holding the area armed.
//
// resolved is false when the base can't be trusted — the area is unknown to this
// snapshot (KV lag), or an auto_schedule is configured but its schedule/location isn't
// loaded yet.
func (s *Snapshot) baseArmState(areaCode string, atUTC time.Time) (armed, resolved bool) {
	a, ok := s.areas[areaCode]
	if !ok {
		return false, false // unknown here: don't act on missing data
	}
	standing := a.Arm == "armed"
	if a.AutoSchedule != "" {
		sched, schedOK := s.graph.Schedules[a.AutoSchedule]
		loc, locOK := s.locs[a.Location]
		if !schedOK || !locOK || loc == nil {
			return standing, false // configured but unresolved: keep the override
		}
		if policy.ScheduleOpen(sched, loc, atUTC, s.graph.Holidays[a.Location]) {
			return a.AutoArm == "armed", true
		}
	}
	return standing, true
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
