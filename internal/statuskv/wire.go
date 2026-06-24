// Package statuskv defines the on-the-wire JSON shapes and key scheme for the
// upward "device shadow" channel: live edge state mirrored into NATS KV (bucket
// ACC_STATUS), one key per point.
//
// It is the reverse of internal/policykv. Policy flows DOWN (accessd writes /
// controller reads); status flows UP (controller writes / accessd reads). The
// controller publishes the current state of each point it drives; accessd watches
// the bucket and projects it into the rebuildable point_status collection behind
// the UI. Keys use the same stable-code scheme as policykv so the bucket stays
// human-readable and self-contained.
//
// Semantics are latest-wins per key (KV history 1): status is "what is true now,"
// not a log of transitions — the history of record is the ACC_EVENTS stream. A
// missed intermediate value is harmless; the newest value is always correct.
package statuskv

import "strings"

// Key prefixes. One KV key per point: "<prefix><code>", e.g. "portal.lobby-main".
// Aux prefixes are added in the aux-I/O phase.
const (
	PrefixPortal = "portal." // portal.<code>
	PrefixAuxIn  = "auxin."  // auxin.<code>  (aux-I/O phase)
	PrefixAuxOut = "auxout." // auxout.<code> (aux-I/O phase)
	// PrefixArea keys an area's arm shadow PER CONTROLLER: "area.<controller>.<code>".
	// Unlike the others this is a compound key (one shadow per participating
	// controller for the same area), so the bare remainder after the prefix is NOT a
	// usable code — the projector reads code/controller from the AreaStatus value.
	PrefixArea = "area." // area.<controller>.<code>
)

// Kind names used in the point_status projection (and the kind select field).
const (
	KindPortal    = "portal"
	KindAuxInput  = "aux_input"
	KindAuxOutput = "aux_output"
	KindArea      = "area"
)

// Area arm states carried in AreaStatus.Arm and the area point_status row's state.
const (
	AreaArmed    = "armed"
	AreaDisarmed = "disarmed"
)

// Parse splits a status KV key into its projection kind and bare code. ok is
// false for an unrecognized prefix (a foreign key the projector should skip). For
// the area kind the returned code is the compound remainder ("<controller>.<code>")
// and is unused — the projector takes code/controller from the AreaStatus value.
func Parse(key string) (kind, code string, ok bool) {
	switch {
	case strings.HasPrefix(key, PrefixArea):
		return KindArea, strings.TrimPrefix(key, PrefixArea), true
	case strings.HasPrefix(key, PrefixPortal):
		return KindPortal, strings.TrimPrefix(key, PrefixPortal), true
	case strings.HasPrefix(key, PrefixAuxIn):
		return KindAuxInput, strings.TrimPrefix(key, PrefixAuxIn), true
	case strings.HasPrefix(key, PrefixAuxOut):
		return KindAuxOutput, strings.TrimPrefix(key, PrefixAuxOut), true
	default:
		return "", "", false
	}
}

// Door contact states carried in PortalStatus.Door. Stable strings (they flow
// verbatim into the point_status projection and the UI).
const (
	DoorOpen    = "open"
	DoorClosed  = "closed"
	DoorUnknown = "unknown" // no door input wired, or before the first DPS edge
)

// Posture provenance carried in PortalStatus.Source — *why* the effective posture
// is what it is, so the UI can mark a manual override (or an active scheduled
// posture) distinctly from the door's normal standing state. Stable strings (they
// flow verbatim into the point_status projection and the UI). An empty Source (a
// shadow written by an older controller) is read as standing.
const (
	PostureSourceStanding  = "standing"  // the portal's configured standing posture
	PostureSourceScheduled = "scheduled" // auto_posture, while the auto_schedule window is open
	PostureSourceOverride  = "override"  // an operator's runtime command override
)

// PortalStatus is the live shadow of one portal the controller drives. Posture is
// the current EFFECTIVE posture (command override / scheduled / standing, resolved
// by the controller) and Source records which of those three produced it; Held
// reports an active held-open (DOTL) alarm. Location and Controller are carried so
// the point_status projection is self-contained (no lookup back into the policy
// graph), per the policykv convention.
type PortalStatus struct {
	Code       string `json:"code"`
	Location   string `json:"location"`
	Controller string `json:"controller"`
	Door       string `json:"door"`      // DoorOpen | DoorClosed | DoorUnknown
	Posture    string `json:"posture"`   // current effective posture
	Source     string `json:"source"`    // PostureSource* — provenance of Posture
	Held       bool   `json:"held"`      // held-open alarm active
	UpdatedAt  string `json:"updatedAt"` // RFC3339 UTC
}

// AuxInputStatus is the live shadow of a named auxiliary input (observe-only).
type AuxInputStatus struct {
	Code       string `json:"code"`
	Location   string `json:"location"`
	Controller string `json:"controller"`
	Active     bool   `json:"active"` // input line asserted
	UpdatedAt  string `json:"updatedAt"`
}

// AuxOutputStatus is the live shadow of a named auxiliary output relay.
type AuxOutputStatus struct {
	Code       string `json:"code"`
	Location   string `json:"location"`
	Controller string `json:"controller"`
	Energized  bool   `json:"energized"` // standing held state (on/off)
	UpdatedAt  string `json:"updatedAt"`
}

// AreaStatus is one controller's view of an area's effective arm-state. Because
// an area can span several controllers, each participating controller writes its
// OWN shadow (key area.<controller>.<code>), and the console aggregates them.
//
// Peers carries the FULL participant set (every controller code with a member
// input in this area) so the console has a denominator: "armed" is true only when
// every peer has reported armed — a peer that was offline at arm time and never
// wrote a shadow is detectable as missing, rather than silently ignored. Every
// participant computes the same Peers from the shared policy graph, so any one
// shadow row carries the authoritative set.
type AreaStatus struct {
	Code       string   `json:"code"`
	Location   string   `json:"location"`
	Controller string   `json:"controller"`
	Arm        string   `json:"arm"`            // AreaArmed | AreaDisarmed (this controller's effective state)
	Source     string   `json:"source"`         // standing | scheduled | override (provenance, PostureSource*)
	Peers      []string `json:"peers"`          // all participating controller codes (the denominator)
	UpdatedAt  string   `json:"updatedAt"`
}
