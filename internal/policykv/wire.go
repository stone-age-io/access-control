// Package policykv defines the on-the-wire JSON shapes and key scheme for the
// policy graph mirrored into NATS KV (bucket ACC_POLICY), one key per record.
//
// It is the shared contract between the publisher (accessd's mirror, which
// writes these) and the consumer (the controller's PolicyStore, which parses
// them). Cross-references are stored as stable codes (or the credential value /
// cardholder id), never PocketBase ids, so keys and values stay human-readable
// and self-contained.
package policykv

// Key prefixes. One KV key per record: "<prefix><natural-key>", e.g.
// "cred.CARD-001", "user.<pbid>", "portal.lobby-main".
const (
	PrefixLocation   = "location."
	PrefixSched      = "sched."
	PrefixPortal     = "portal."
	PrefixGroup      = "group."
	PrefixRole       = "role."
	PrefixUser       = "user."
	PrefixCred       = "cred."
	PrefixController = "controller."
	PrefixHoliday    = "holiday."
	PrefixAuxInput   = "auxin."
	PrefixAuxOutput  = "auxout."
	PrefixArea       = "area."
)

// Location carries the timezone (the controller resolves it once per evaluation)
// and the fire-alarm-input alarm-suppression flag. HolidayCalendars are the codes
// of the holiday calendars this site observes; the controller unions their dates
// into the location's HolidaySet (see PolicyStore.rebuildHolidays).
type Location struct {
	Code             string   `json:"code"`
	Name             string   `json:"name"`
	Timezone         string   `json:"timezone"`
	FAISuppress      bool     `json:"faiSuppress"`
	HolidayCalendars []string `json:"holidayCalendars,omitempty"`
}

// Window is one recurring time window. Days are ISO weekdays (1=Mon..7=Sun);
// Start/End are "HH:MM" local wall-clock; End<=Start crosses midnight.
type Window struct {
	Days  []int  `json:"days"`
	Start string `json:"start"`
	End   string `json:"end"`
}

// Schedule is a reusable set of weekly windows. ObserveHolidays closes every
// window on a holiday of the evaluated portal's location (operator default true).
type Schedule struct {
	Code            string   `json:"code"`
	Windows         []Window `json:"windows"`
	ObserveHolidays bool     `json:"observeHolidays"`
}

// Holiday is one date on a holiday calendar (a location observes a set of these;
// it is no longer location-scoped). Calendar is the code of the owning calendar;
// Date is a local "YYYY-MM-DD" (the wall-clock day the site is closed); Recurring
// matches that month/day every year (for fixed-date holidays like Dec 25). One KV
// key per record, keyed by the PocketBase id (holidays carry no natural code).
type Holiday struct {
	Calendar  string `json:"calendar"`
	Date      string `json:"date"`
	Recurring bool   `json:"recurring"`
}

// Portal references its location by code; Type is the portal kind (also the
// {type} subject segment, a single NATS token); Posture is the standing default.
//
// Controller is the code of the edge box that drives this portal (empty if
// unassigned); LockRelay/DpsInput/RexInput are *logical* hardware indices on
// that box (the box's model template maps them to physical lines); HeldOpenSeconds
// is the door-open-too-long threshold. ReaderAddress is the OSDP PD address of
// this portal's reader on the controller's RS485 bus (used only when the
// controller's reader is "osdp"). These hardware fields are consumed only by the
// controller's PortalManager/runtime, never by the pure policy.Decide.
type Portal struct {
	Code            string `json:"code"`
	Type            string `json:"type"`
	Location        string `json:"location"`
	Posture         string `json:"posture"`
	PulseSeconds    int    `json:"pulseSeconds"`
	AutoPosture     string `json:"autoPosture,omitempty"`  // scheduled posture while AutoSchedule is open
	AutoSchedule    string `json:"autoSchedule,omitempty"` // schedule code gating AutoPosture
	Controller      string `json:"controller,omitempty"`
	LockRelay       int    `json:"lockRelay,omitempty"`
	DpsInput        int    `json:"dpsInput,omitempty"`
	RexInput        int    `json:"rexInput,omitempty"`
	HeldOpenSeconds int    `json:"heldOpenSeconds,omitempty"`
	ReaderAddress   int    `json:"readerAddress,omitempty"` // OSDP PD address (reader=="osdp")
	// Per-install wiring sense (consumed only by the controller's hardware arming,
	// never by policy.Decide). Empty = the common default for that input/lock.
	DpsContact string `json:"dpsContact,omitempty"` // "" / "nc" (default) / "no" door-position contact
	RexContact string `json:"rexContact,omitempty"` // "" / "no" (default) / "nc" request-to-exit contact
	LockType   string `json:"lockType,omitempty"`   // "" / "strike" (default) / "maglock"
	RexUnlock  bool   `json:"rexUnlock,omitempty"`  // REX press also pulses the lock, not just shunts the alarm
}

// DPSInvert / REXInvert / IsMaglock translate the operator's contact-type / lock-type
// labels into the boolean line-sense inversion the hardware backend applies. The
// per-input default lives here, in one place: a door-position contact is normally
// CLOSED (closed when the door is shut), a REX contact is normally OPEN (closed
// when pressed) — so only the non-default value inverts.
func (p Portal) DPSInvert() bool { return p.DpsContact == "no" }
func (p Portal) REXInvert() bool { return p.RexContact == "nc" }
func (p Portal) IsMaglock() bool { return p.LockType == "maglock" }

// Controller is an edge box. It references its location by code; Model selects
// the hardware template. last_seen/status are not mirrored (accessd writes them
// from heartbeats), so they are absent here.
type Controller struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Location string `json:"location"`
	Model    string `json:"model"`
}

// AuxInput is a named auxiliary digital input bound to a controller — like a
// portal's DPS/REX but standalone (no door semantics). Location and Controller
// are codes; InputIndex is a logical input index on the box (the model template
// maps it to a physical line). Area is the code of the area this input belongs to
// (empty = none); PointType ("" / "monitor" / "intrusion" / "tamper_24h") decides
// whether an active edge raises an intrusion alarm (see the controller runtime).
// Consumed only by the controller's AuxManager/runtime, never by policy.Decide.
type AuxInput struct {
	Code       string `json:"code"`
	Location   string `json:"location"`
	Controller string `json:"controller,omitempty"`
	InputIndex int    `json:"inputIndex,omitempty"`
	Contact    string `json:"contact,omitempty"`   // "" / "no" (default) / "nc" contact sense
	Area       string `json:"area,omitempty"`      // area code this input belongs to
	PointType  string `json:"pointType,omitempty"` // "" / monitor (default) / intrusion / tamper_24h
}

// Area is an arm-state grouping (logical, single-location, possibly spanning
// several controllers). All cross-refs are codes. Arm is the standing floor;
// ArmOverride is the durable operator override (empty = none); AutoArm/AutoSchedule
// are the scheduled arm-state (both-or-neither). The controller resolves the
// effective arm-state from these exactly as it resolves scheduled posture, and
// arming is purely derived from KV so a reboot re-derives it. Consumed only by the
// controller's AreaManager/runtime, never by policy.Decide.
type Area struct {
	Code         string `json:"code"`
	Name         string `json:"name,omitempty"`
	Location     string `json:"location"`
	Arm          string `json:"arm,omitempty"`          // standing: "" (=disarmed) / disarmed / armed
	ArmOverride  string `json:"armOverride,omitempty"`  // durable override: "" (none) / armed / disarmed
	AutoArm      string `json:"autoArm,omitempty"`      // scheduled arm-state while AutoSchedule open
	AutoSchedule string `json:"autoSchedule,omitempty"` // schedule code gating AutoArm
}

// Invert reports whether the aux input's contact sense is inverted from the
// default (normally-open: closed when asserted). A normally-closed contact inverts.
func (a AuxInput) Invert() bool { return a.Contact == "nc" }

// AuxOutput is a named auxiliary relay bound to a controller — driven by the
// cmd.output command (on/off/pulse). RelayIndex is a logical relay index on the
// box; PulseSeconds is the default momentary-pulse duration.
type AuxOutput struct {
	Code         string `json:"code"`
	Location     string `json:"location"`
	Controller   string `json:"controller,omitempty"`
	RelayIndex   int    `json:"relayIndex,omitempty"`
	PulseSeconds int    `json:"pulseSeconds,omitempty"`
}

// AccessGroup grants a set of portals (by code) under one schedule (by code).
type AccessGroup struct {
	Code     string   `json:"code"`
	Portals  []string `json:"portals"`
	Schedule string   `json:"schedule"`
}

// Role bundles access groups (by code).
type Role struct {
	Code   string   `json:"code"`
	Groups []string `json:"groups"`
}

// User (cardholder) references roles by code. Keyed in KV by PocketBase id.
type User struct {
	ID     string   `json:"id"`
	Status string   `json:"status"`
	Roles  []string `json:"roles"`
}

// Credential maps an opaque value to a cardholder (by id). ValidFrom/ValidUntil
// are optional RFC 3339 activation/expiry bounds (empty = unbounded); the
// controller parses them once on apply. A present-but-unparseable bound fails
// closed (the credential denies).
type Credential struct {
	Value      string `json:"value"`
	User       string `json:"user"`
	Status     string `json:"status"`
	ValidFrom  string `json:"validFrom,omitempty"`
	ValidUntil string `json:"validUntil,omitempty"`
}
