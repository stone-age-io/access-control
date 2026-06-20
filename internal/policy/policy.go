// Package policy holds the access-control policy graph and the pure decision
// function.
//
// It has no I/O and no concurrency control of its own: the caller (the edge
// controller) owns storage and locking and invokes Decide under a read lock.
// Keeping Decide a pure function over plain maps makes it run identically on the
// central app and the edge, and makes it trivially table-testable.
//
// The graph mirrors the operator's mental model 1:1 — user → roles → access
// groups → (portals + one schedule). A grant is the union over a user's
// access groups; a deny (revoked/suspended credential or user) overrides. There
// is deliberately no compiled/denormalized form: the data is small (low
// single-digit MB for a whole org) and the join is a cheap nested-map walk
// bounded by one user's role/group fan-out.
package policy

import "time"

// Posture is a portal's standing state. Effective posture is resolved by
// the controller (standing value from policy, optionally overridden by a command
// or a scheduled-posture window) and passed into Decide, so Decide stays pure.
//
// secure, free_access, and unlocked differ in two independent dimensions —
// whether the credential is consulted, and whether the strike is physically
// held open. Decide only owns the first; the controller owns the second
// (the reconciler holds the strike only for unlocked). See the table:
//
//	posture      credential consulted?   strike physically held?
//	secure       yes                     no (pulses on grant)
//	free_access  no (any tap opens)      no (pulses on tap)
//	unlocked     no (any tap opens)      yes (held open; no tap needed)
const (
	PostureSecure     = "secure"      // credential required (default)
	PostureFreeAccess = "free_access" // any tap opens (credential not consulted); strike still pulses, door stays closed
	PostureUnlocked   = "unlocked"    // physical hold — strike held open, no tap needed
	PostureLockdown   = "lockdown"    // deny all — beats a valid credential
	PostureDisabled   = "disabled"    // not enforcing / maintenance
)

// Status values shared by users and credentials. Anything other than
// StatusActive is treated as a deny.
const (
	StatusActive    = "active"
	StatusSuspended = "suspended"
	StatusRevoked   = "revoked"
)

// Decision reason codes. Stable strings — they flow verbatim into tap events
// and the events collection, so downstream consumers and dashboards can rely on
// them.
const (
	ReasonAllowGrant             = "allow_grant"
	ReasonAllowPostureUnlocked   = "allow_posture_unlocked"    // posture unlocked (B): credential not consulted
	ReasonAllowPostureFreeAccess = "allow_posture_free_access" // posture free_access (A): any tap opens, no validation

	ReasonDenyUnknownCredential = "deny_unknown_credential"
	ReasonDenyRevoked           = "deny_revoked"
	ReasonDenyNotYetValid       = "deny_not_yet_valid" // credential presented before its valid_from
	ReasonDenyExpired           = "deny_expired"       // credential presented after its valid_until
	ReasonDenyNoAccess          = "deny_no_access"
	ReasonDenyScheduleClosed    = "deny_schedule_closed"
	ReasonDenyLockdown          = "deny_lockdown"
	ReasonDenyPointDisabled     = "deny_point_disabled"
	ReasonDenyUnknownPoint      = "deny_unknown_point"
)

// Policy is the in-memory access-control graph. All cross-references are by code
// (or credential value / user id), never by storage id, so the graph is
// self-contained and human-readable. Reads of nil maps are safe (zero value,
// not found), so a zero Policy{} default-denies everything.
type Policy struct {
	Schedules map[string]Schedule    // schedule code -> schedule
	Portals   map[string]Portal      // portal code -> portal
	Users     map[string]User        // user id -> user
	Roles     map[string]Role        // role code -> role
	Groups    map[string]AccessGroup // access-group code -> group
	Creds     map[string]Credential  // credential value -> credential (the tap lookup)
	Holidays  map[string]HolidaySet  // location code -> that site's holiday calendar
}

// Schedule is a reusable set of weekly time windows. The owning site supplies
// the timezone at evaluation time; a schedule carries no timezone of its own.
// ObserveHolidays (the operator-facing default is true) closes every window on a
// holiday of the evaluated portal's location.
type Schedule struct {
	Windows         []Window
	ObserveHolidays bool
}

// HolidaySet is one location's holiday calendar: explicit "YYYY-MM-DD" local
// dates plus "MM-DD" dates that recur every year. The zero value (nil maps)
// contains nothing, so a location with no holidays never closes a schedule.
type HolidaySet struct {
	Dates     map[string]struct{} // "2006-01-02"
	Recurring map[string]struct{} // "01-02"
}

// Contains reports whether the given local date falls on a holiday.
func (h HolidaySet) Contains(local time.Time) bool {
	if _, ok := h.Dates[local.Format("2006-01-02")]; ok {
		return true
	}
	_, ok := h.Recurring[local.Format("01-02")]
	return ok
}

// Window is one recurring time window. Days are ISO weekdays (1=Mon..7=Sun).
// Start/End are "HH:MM" local wall-clock; "24:00" is accepted as end-of-day.
// If End <= Start the window crosses midnight (e.g. 22:00–06:00).
type Window struct {
	Days  []int
	Start string
	End   string
}

// Portal is a controllable opening (door/gate/turnstile/elevator) or a logical
// access target. Type is the portal kind (also the {type} subject segment).
// Posture here is the standing default; the controller may override it with a
// runtime command or, while AutoSchedule's window is open, with AutoPosture
// (scheduled posture — both empty means no automation). Effective posture is
// resolved by the controller, never by the pure Decide.
type Portal struct {
	Code         string
	Type         string
	Location     string
	Posture      string
	PulseSeconds int
	AutoPosture  string // posture to adopt while AutoSchedule is open ("" = none)
	AutoSchedule string // schedule code gating AutoPosture ("" = none)
}

// User is an identity that holds credentials and roles.
type User struct {
	ID     string
	Status string
	Roles  []string // role codes
}

// Role is a named bundle of access groups assigned to users.
type Role struct {
	Code   string
	Groups []string // access-group codes
}

// AccessGroup ("access level") grants a set of portals under exactly one
// schedule. Portals is a set for O(1) membership at decision time.
type AccessGroup struct {
	Code     string
	Portals  map[string]struct{}
	Schedule string // schedule code
}

// Credential is an opaque string presented at a reader, mapping to one user.
// ValidFrom/ValidUntil are optional activation/expiry bounds (zero = unbounded);
// the controller parses them once on apply so Decide does no parsing on the hot
// path. A presentation outside the bounds denies.
type Credential struct {
	Value      string
	User       string
	Status     string
	ValidFrom  time.Time
	ValidUntil time.Time
}

// Decision is the result of evaluating a credential presentation.
type Decision struct {
	Allow  bool
	Reason string
	User   string // resolved user id when known, otherwise ""
	Pulse  int    // seconds to energize the strike when Allow
}
