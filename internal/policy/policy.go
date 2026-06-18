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

// Posture is a portal's standing state. Effective posture is resolved by
// the controller (standing value from policy, optionally overridden by a command)
// and passed into Decide, so Decide stays pure.
const (
	PostureSecure   = "secure"   // credential required (default)
	PostureUnlocked = "unlocked" // free passage, credentials not consulted
	PostureLockdown = "lockdown" // deny all — beats a valid credential
	PostureDisabled = "disabled" // not enforcing / maintenance
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
	ReasonAllowGrant           = "allow_grant"
	ReasonAllowPostureUnlocked = "allow_posture_unlocked"

	ReasonDenyUnknownCredential = "deny_unknown_credential"
	ReasonDenyRevoked           = "deny_revoked"
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
}

// Schedule is a reusable set of weekly time windows. The owning site supplies
// the timezone at evaluation time; a schedule carries no timezone of its own.
type Schedule struct {
	Windows []Window
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
// Posture here is the standing default; the controller may hold a runtime override.
type Portal struct {
	Code         string
	Type         string
	Location     string
	Posture      string
	PulseSeconds int
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
type Credential struct {
	Value  string
	User   string
	Status string
}

// Decision is the result of evaluating a credential presentation.
type Decision struct {
	Allow  bool
	Reason string
	User   string // resolved user id when known, otherwise ""
	Pulse  int    // seconds to energize the strike when Allow
}
