package demoseed

// The fixtures. Everything here is fictional.
//
// THE SITE CODES ARE NOT OURS. `KC-DC1`, `KC-OFFICE` and `SGF-XD2` are the
// location codes the Stone Age platform's own `demo-seed` writes for its
// Northwind Traders organization, and they are reproduced here verbatim so the
// two demos describe ONE company rather than two that happen to share a name.
//
// That direction is deliberate. The platform is the inventory system of record
// for sites, so a location code is minted there and mirrored here — the same rule
// ADR 0002 states for the ecosystem at large: ids for storage, codes for
// addressing. Renaming a site here without renaming it there does not break
// anything at runtime (this app resolves nothing over the network by that code
// except its own subjects) — it breaks the demo's whole point, which is that a
// door in this app and a Thing in the platform name the same building.
//
// Access HARDWARE codes go the other way. A controller and a portal appear in
// NATS subjects (`acc.{location}.{type}.{thing}`), so they are minted here in
// subject-token form — lowercase, hyphenated, no dots — and the platform's demo
// seeds Things carrying these exact strings as their codes. Whichever app first
// puts an object on the wire names it; the other one follows.

// ------------------------------------------------------------------ locations

type locationFixture struct {
	Code, Name, Description, Timezone string
	Lat, Lon                          float64

	// FAISuppress makes a fire-alarm input suppress door alarms at this site —
	// correct for a warehouse where fire drops every door, wrong for an office
	// suite where it does not.
	FAISuppress bool

	// NotifyFire opts the site into the notification sink's fire emails. Inert
	// unless an operator also sets users.notify.
	NotifyFire bool

	// BadgeFloorplan lets a cardholder see the site's floor plan on their own
	// badge screen. Nothing renders until someone uploads a floorplan image, which
	// this seed deliberately does not do — see the note in seed.go.
	BadgeFloorplan bool

	// Calendars are holiday_calendars codes this site observes.
	Calendars []string
}

var locations = []locationFixture{
	{
		Code: "KC-DC1", Name: "Kansas City Distribution Center",
		Description: "Primary frozen and chilled DC. 42 dock doors.",
		Timezone:    "America/Chicago", Lat: 39.1141, Lon: -94.6275,
		FAISuppress: true, NotifyFire: true, Calendars: []string{"us-federal"},
	},
	{
		Code: "KC-OFFICE", Name: "Kansas City Office",
		Description: "Front office suite attached to the DC.",
		Timezone:    "America/Chicago", Lat: 39.1148, Lon: -94.6261,
		// Deliberately NOT fai_suppress: an office suite's doors are not dropped
		// by the fire panel, so a forced-open during an alarm is still real.
		NotifyFire: true, BadgeFloorplan: true, Calendars: []string{"us-federal"},
	},
	{
		Code: "SGF-XD2", Name: "Springfield Cross-Dock",
		Description: "Ambient cross-dock. No long-term storage.",
		Timezone:    "America/Chicago", Lat: 37.2153, Lon: -93.2982,
		FAISuppress: true, NotifyFire: true, Calendars: []string{"us-federal"},
	},
}

// ------------------------------------------------------------------ schedules

type windowFixture struct {
	Days       []int // ISO weekdays, 1=Mon .. 7=Sun
	Start, End string
}

type scheduleFixture struct {
	Code, Name string
	Windows    []windowFixture

	// IgnoreHolidays keeps every window open on a holiday. Wrong for staff
	// access, right for the comms cabinet and for the overnight arm window — a
	// building still has to lock itself on Christmas Day.
	IgnoreHolidays bool
}

var schedules = []scheduleFixture{
	{Code: "nw-business-hours", Name: "Business Hours (M-F 07:00-18:00)",
		Windows: []windowFixture{{Days: []int{1, 2, 3, 4, 5}, Start: "07:00", End: "18:00"}}},

	{Code: "nw-warehouse-shift", Name: "Warehouse Shift (M-Sat 05:00-22:00)",
		Windows: []windowFixture{{Days: []int{1, 2, 3, 4, 5, 6}, Start: "05:00", End: "22:00"}}},

	{Code: "nw-cleaning", Name: "Cleaning Crew (M-F 18:00-23:00)",
		Windows: []windowFixture{{Days: []int{1, 2, 3, 4, 5}, Start: "18:00", End: "23:00"}}},

	// End <= Start is the crosses-midnight form: the tail belongs to today's
	// weekday and the head to yesterday's (policy.windowOpen). 22:00-05:00 every
	// day is the window the warehouse arms itself in.
	{Code: "nw-night-arm", Name: "Overnight Arm (22:00-05:00 daily)",
		IgnoreHolidays: true,
		Windows:        []windowFixture{{Days: []int{1, 2, 3, 4, 5, 6, 7}, Start: "22:00", End: "05:00"}}},

	// 00:00-00:00 is End == Start, so it takes the same crosses-midnight branch
	// and is open for the whole day. Written this way rather than 00:00-23:59
	// because it is genuinely 24/7, with no dead minute before midnight.
	{Code: "nw-always", Name: "Always (24/7)",
		IgnoreHolidays: true,
		Windows:        []windowFixture{{Days: []int{1, 2, 3, 4, 5, 6, 7}, Start: "00:00", End: "00:00"}}},
}

// ----------------------------------------------------------------- holidays

type calendarFixture struct {
	Code, Name string
	Dates      []holidayFixture
}

type holidayFixture struct {
	Name string
	Date string // "YYYY-MM-DD" local
	// Recurring matches that month/day every year — right for a fixed-date
	// holiday, wrong for Thanksgiving.
	Recurring bool
}

var calendars = []calendarFixture{
	{Code: "us-federal", Name: "US Federal Holidays", Dates: []holidayFixture{
		{Name: "New Year's Day", Date: "2026-01-01", Recurring: true},
		{Name: "Independence Day", Date: "2026-07-04", Recurring: true},
		{Name: "Thanksgiving", Date: "2026-11-26"},
		{Name: "Christmas Day", Date: "2026-12-25", Recurring: true},
	}},
}

// ---------------------------------------------------------------- controllers

type controllerFixture struct {
	Code, Name, Location, Model string

	// NotifyOffline opts this controller into the offline-notification path.
	NotifyOffline bool
}

// Two models are represented because they take different physical transports —
// the Server-Mini drives GPIO directly, the Pi5R8 goes through an MCP23017 over
// I2C — and a demo with one model hides that the profile, not the binary, is what
// changes per board.
var controllers = []controllerFixture{
	{Code: "ctrl-kc-dc1-1", Name: "KC DC1 - Dock Panel", Location: "KC-DC1",
		Model: "kincony-server-mini", NotifyOffline: true},
	{Code: "ctrl-kc-dc1-2", Name: "KC DC1 - Interior Panel", Location: "KC-DC1",
		Model: "kincony-pi5r8", NotifyOffline: true},
	{Code: "ctrl-kc-office-1", Name: "KC Office Panel", Location: "KC-OFFICE",
		Model: "kincony-server-mini"},
	{Code: "ctrl-sgf-xd2-1", Name: "Springfield Panel", Location: "SGF-XD2",
		Model: "kincony-pi5r8", NotifyOffline: true},
}

// --------------------------------------------------------------------- areas

type areaFixture struct {
	Code, Name, Location string

	// Arm is the standing state. AutoArm + AutoSchedule is the scheduled one; the
	// controller resolves override > scheduled > standing.
	Arm          string
	AutoArm      string
	AutoSchedule string

	NotifyOnAlarm  bool
	AllowRemoteArm bool
}

var areas = []areaFixture{
	{Code: "kc-dc1-warehouse", Name: "KC Warehouse Floor", Location: "KC-DC1",
		Arm: "disarmed", AutoArm: "armed", AutoSchedule: "nw-night-arm",
		NotifyOnAlarm: true, AllowRemoteArm: true},
	{Code: "kc-dc1-comms", Name: "KC Comms Cabinet", Location: "KC-DC1",
		Arm: "armed", NotifyOnAlarm: true},
	{Code: "kc-office-suite", Name: "KC Office Suite", Location: "KC-OFFICE",
		Arm: "disarmed", AutoArm: "armed", AutoSchedule: "nw-night-arm"},
	{Code: "sgf-xd2-dock", Name: "Springfield Dock Floor", Location: "SGF-XD2",
		Arm: "armed", NotifyOnAlarm: true, AllowRemoteArm: true},
}

// ------------------------------------------------------------------- portals

type portalFixture struct {
	Code, Name, Location, Controller string
	Type                             string // door | turnstile | elevator | gate | logical
	Posture                          string
	PulseSeconds                     int

	LockRelay, DPSInput, RexInput int
	HeldOpenSeconds               int
	LockType                      string // strike | maglock
	DPSContact, RexContact        string // nc | no
	RexUnlock                     bool

	// ReaderAddress is the OSDP PD address on the controller's RS485 bus. -1 means
	// NATS-only, which is what a simulated demo tap uses.
	ReaderAddress int

	// AutoPosture + AutoSchedule: a scheduled posture, resolved by the controller
	// under any command override and above the standing Posture.
	AutoPosture, AutoSchedule string

	Area              string
	DisarmOnGrant     bool
	AllowRemoteUnlock bool
	NotifyOnAlarm     bool
}

var portals = []portalFixture{
	// ---- KC-DC1, dock panel
	{Code: "kc-dc1-main", Name: "DC Main Entrance", Location: "KC-DC1", Controller: "ctrl-kc-dc1-1",
		Type: "door", Posture: "secure", PulseSeconds: 5,
		LockRelay: 1, DPSInput: 1, RexInput: 2, HeldOpenSeconds: 30,
		LockType: "strike", DPSContact: "nc", RexContact: "no", RexUnlock: true,
		ReaderAddress: 0, AllowRemoteUnlock: true, NotifyOnAlarm: true},

	{Code: "kc-dc1-dock-a", Name: "Dock A Personnel Door", Location: "KC-DC1", Controller: "ctrl-kc-dc1-1",
		Type: "door", Posture: "secure", PulseSeconds: 5,
		LockRelay: 2, DPSInput: 3, RexInput: 4, HeldOpenSeconds: 45,
		LockType: "strike", DPSContact: "nc", RexContact: "no", RexUnlock: true,
		ReaderAddress: 1, Area: "kc-dc1-warehouse", NotifyOnAlarm: true},

	{Code: "kc-dc1-freezer-1", Name: "Freezer Zone 1 Door", Location: "KC-DC1", Controller: "ctrl-kc-dc1-1",
		Type: "door", Posture: "secure", PulseSeconds: 8,
		LockRelay: 3, DPSInput: 5, RexInput: 6, HeldOpenSeconds: 60,
		// A maglock, not a strike: a freezer door is held shut by the seal and
		// wants a fail-safe lock that drops on fire.
		LockType: "maglock", DPSContact: "nc", RexContact: "no", RexUnlock: true,
		ReaderAddress: 2, Area: "kc-dc1-warehouse"},

	// ---- KC-DC1, interior panel
	{Code: "kc-dc1-mdf", Name: "Comms Cabinet", Location: "KC-DC1", Controller: "ctrl-kc-dc1-2",
		Type: "door", Posture: "secure", PulseSeconds: 5,
		LockRelay: 1, DPSInput: 1, RexInput: 2, HeldOpenSeconds: 15,
		LockType: "strike", DPSContact: "nc", RexContact: "no",
		ReaderAddress: 0, Area: "kc-dc1-comms", NotifyOnAlarm: true},

	{Code: "kc-dc1-yard", Name: "Yard Gate", Location: "KC-DC1", Controller: "ctrl-kc-dc1-2",
		Type: "gate", Posture: "secure", PulseSeconds: 10,
		LockRelay: 2, DPSInput: 3, RexInput: 4, HeldOpenSeconds: 180,
		LockType: "strike", DPSContact: "nc", RexContact: "no",
		ReaderAddress: 1, AllowRemoteUnlock: true},

	// ---- KC-OFFICE
	{Code: "kc-office-lobby", Name: "Office Lobby", Location: "KC-OFFICE", Controller: "ctrl-kc-office-1",
		Type: "door", Posture: "secure", PulseSeconds: 5,
		LockRelay: 1, DPSInput: 1, RexInput: 2, HeldOpenSeconds: 30,
		LockType: "strike", DPSContact: "nc", RexContact: "no", RexUnlock: true,
		ReaderAddress: 0,
		// Unlocked during business hours: free passage, no tap needed. Distinct
		// from free_access, which pulses the strike on every tap and logs each one.
		AutoPosture: "unlocked", AutoSchedule: "nw-business-hours",
		AllowRemoteUnlock: true},

	{Code: "kc-office-server", Name: "Office Server Room", Location: "KC-OFFICE", Controller: "ctrl-kc-office-1",
		Type: "door", Posture: "secure", PulseSeconds: 5,
		LockRelay: 2, DPSInput: 3, RexInput: 4, HeldOpenSeconds: 15,
		LockType: "strike", DPSContact: "nc", RexContact: "no",
		ReaderAddress: 1, Area: "kc-office-suite", NotifyOnAlarm: true},

	// ---- SGF-XD2
	{Code: "sgf-xd2-main", Name: "Cross-Dock Main Entrance", Location: "SGF-XD2", Controller: "ctrl-sgf-xd2-1",
		Type: "door", Posture: "secure", PulseSeconds: 5,
		LockRelay: 1, DPSInput: 1, RexInput: 2, HeldOpenSeconds: 30,
		LockType: "strike", DPSContact: "nc", RexContact: "no", RexUnlock: true,
		ReaderAddress: 0, Area: "sgf-xd2-dock",
		// The first person in disarms the dock by badging. Pairs with the area's
		// scheduled arm: the building locks itself overnight and the opener's
		// valid grant unlocks it, with no operator clearing anything.
		DisarmOnGrant: true, AllowRemoteUnlock: true, NotifyOnAlarm: true},

	{Code: "sgf-xd2-dock-b", Name: "Dock B Personnel Door", Location: "SGF-XD2", Controller: "ctrl-sgf-xd2-1",
		Type: "door", Posture: "secure", PulseSeconds: 5,
		LockRelay: 2, DPSInput: 3, RexInput: 4, HeldOpenSeconds: 45,
		LockType: "strike", DPSContact: "nc", RexContact: "no", RexUnlock: true,
		ReaderAddress: 1, Area: "sgf-xd2-dock"},

	{Code: "sgf-xd2-mdf", Name: "Springfield Comms Cabinet", Location: "SGF-XD2", Controller: "ctrl-sgf-xd2-1",
		Type: "door", Posture: "secure", PulseSeconds: 5,
		LockRelay: 3, DPSInput: 5, RexInput: 6, HeldOpenSeconds: 15,
		LockType: "strike", DPSContact: "nc", RexContact: "no",
		ReaderAddress: 2, NotifyOnAlarm: true},
}

// --------------------------------------------------------------- aux points

type auxInputFixture struct {
	Code, Name, Location, Controller string
	InputIndex                       int
	Kind                             string // monitor | intrusion | tamper_24h
	Contact                          string // no | nc
	Area                             string
}

// Inputs 1-6 on each panel are taken by the door pairs above, so aux points start
// at 7 — and on the two panels with three doors there is exactly nothing left.
// That constraint is real: an 8-in/8-out board runs out, and a demo that ignored
// it would teach someone to over-subscribe a panel.
var auxInputs = []auxInputFixture{
	{Code: "kc-dc1-motion-1", Name: "Warehouse Motion (Dock A)", Location: "KC-DC1",
		Controller: "ctrl-kc-dc1-1", InputIndex: 7, Kind: "intrusion",
		Contact: "nc", Area: "kc-dc1-warehouse"},
	{Code: "kc-dc1-glass-1", Name: "Warehouse Glassbreak", Location: "KC-DC1",
		Controller: "ctrl-kc-dc1-1", InputIndex: 8, Kind: "tamper_24h",
		Contact: "nc", Area: "kc-dc1-warehouse"},
	{Code: "kc-dc1-mdf-tamper", Name: "Comms Cabinet Tamper", Location: "KC-DC1",
		Controller: "ctrl-kc-dc1-2", InputIndex: 5, Kind: "tamper_24h",
		Contact: "nc", Area: "kc-dc1-comms"},
	{Code: "kc-dc1-yard-loop", Name: "Yard Exit Loop", Location: "KC-DC1",
		Controller: "ctrl-kc-dc1-2", InputIndex: 6, Kind: "monitor", Contact: "no"},
	{Code: "kc-office-motion", Name: "Office Suite Motion", Location: "KC-OFFICE",
		Controller: "ctrl-kc-office-1", InputIndex: 5, Kind: "intrusion",
		Contact: "nc", Area: "kc-office-suite"},
	{Code: "kc-office-tamper", Name: "Office Panel Tamper", Location: "KC-OFFICE",
		Controller: "ctrl-kc-office-1", InputIndex: 6, Kind: "tamper_24h", Contact: "nc"},
	{Code: "sgf-xd2-motion", Name: "Dock Floor Motion", Location: "SGF-XD2",
		Controller: "ctrl-sgf-xd2-1", InputIndex: 7, Kind: "intrusion",
		Contact: "nc", Area: "sgf-xd2-dock"},
	{Code: "sgf-xd2-tamper", Name: "Springfield Panel Tamper", Location: "SGF-XD2",
		Controller: "ctrl-sgf-xd2-1", InputIndex: 8, Kind: "tamper_24h", Contact: "nc"},
}

type auxOutputFixture struct {
	Code, Name, Location, Controller string
	RelayIndex                       int
	PulseSeconds                     int
	AllowRemote                      bool
}

var auxOutputs = []auxOutputFixture{
	{Code: "kc-dc1-dock-horn", Name: "Dock A Horn", Location: "KC-DC1",
		Controller: "ctrl-kc-dc1-1", RelayIndex: 5, PulseSeconds: 3},
	{Code: "kc-dc1-yard-lights", Name: "Yard Lighting", Location: "KC-DC1",
		Controller: "ctrl-kc-dc1-2", RelayIndex: 5, PulseSeconds: 0, AllowRemote: true},
	{Code: "kc-office-gate", Name: "Office Gate Strike", Location: "KC-OFFICE",
		Controller: "ctrl-kc-office-1", RelayIndex: 5, PulseSeconds: 5, AllowRemote: true},
	{Code: "sgf-xd2-siren", Name: "Dock Siren", Location: "SGF-XD2",
		Controller: "ctrl-sgf-xd2-1", RelayIndex: 5, PulseSeconds: 10},
}

// ------------------------------------------------------- groups and roles

type groupFixture struct {
	Code, Name, Schedule string
	Portals              []string
	Areas                []string
	AuxOutputs           []string

	// AreaRights is what the Areas above are granted FOR. Empty grants neither
	// arm nor disarm and reports deny_no_area_right — diagnosable rather than
	// silent, which is why it is a separate list from Areas.
	AreaRights []string
}

var groups = []groupFixture{
	{Code: "ag-nw-entrances", Name: "Site Entrances (business hours)", Schedule: "nw-business-hours",
		Portals: []string{"kc-dc1-main", "kc-office-lobby", "sgf-xd2-main"}},

	{Code: "ag-nw-warehouse", Name: "Warehouse Floor", Schedule: "nw-warehouse-shift",
		Portals:    []string{"kc-dc1-main", "kc-dc1-dock-a", "kc-dc1-freezer-1", "sgf-xd2-main", "sgf-xd2-dock-b"},
		Areas:      []string{"kc-dc1-warehouse", "sgf-xd2-dock"},
		AreaRights: []string{"arm", "disarm"}},

	// The asymmetry worth looking at: the cleaning crew can arm the warehouse
	// when they finish and cannot disarm it. That is why arm and disarm are two
	// rights rather than one checkbox.
	{Code: "ag-nw-cleaning", Name: "Cleaning Crew (evenings, arm only)", Schedule: "nw-cleaning",
		Portals:    []string{"kc-dc1-main", "kc-office-lobby"},
		Areas:      []string{"kc-dc1-warehouse", "kc-office-suite"},
		AreaRights: []string{"arm"}},

	{Code: "ag-nw-comms", Name: "Comms Cabinets (24/7)", Schedule: "nw-always",
		Portals:    []string{"kc-dc1-mdf", "sgf-xd2-mdf", "kc-office-server"},
		Areas:      []string{"kc-dc1-comms"},
		AreaRights: []string{"arm", "disarm"}},

	{Code: "ag-nw-yard", Name: "Yard and Gates", Schedule: "nw-warehouse-shift",
		Portals:    []string{"kc-dc1-yard"},
		AuxOutputs: []string{"kc-dc1-yard-lights"}},

	// An access group whose only targets are aux outputs. A badge that opens no
	// door and still authorizes something is the case that shows why targets are
	// three independent relations rather than a widened portal list.
	{Code: "ag-nw-relays", Name: "Facility Relays", Schedule: "nw-always",
		AuxOutputs: []string{"kc-dc1-dock-horn", "kc-office-gate", "sgf-xd2-siren"}},
}

type roleFixture struct {
	Code, Name string
	Groups     []string
}

var roles = []roleFixture{
	{Code: "nw-staff", Name: "Office Staff", Groups: []string{"ag-nw-entrances"}},
	{Code: "nw-warehouse", Name: "Warehouse Associate", Groups: []string{"ag-nw-warehouse"}},
	{Code: "nw-driver", Name: "Driver", Groups: []string{"ag-nw-entrances", "ag-nw-yard"}},
	{Code: "nw-facilities", Name: "Facilities", Groups: []string{"ag-nw-entrances", "ag-nw-yard", "ag-nw-relays", "ag-nw-comms"}},
	{Code: "nw-security", Name: "Security", Groups: []string{"ag-nw-entrances", "ag-nw-warehouse", "ag-nw-yard", "ag-nw-relays"}},
	{Code: "nw-cleaning", Name: "Cleaning Crew", Groups: []string{"ag-nw-cleaning"}},
	{Code: "nw-it", Name: "IT", Groups: []string{"ag-nw-entrances", "ag-nw-comms"}},
	// A role with no groups at all. Someone enrolled but not yet granted anything
	// is a normal state, and it is what deny_no_access looks like.
	{Code: "nw-contractor", Name: "Contractor (ungranted)", Groups: nil},
}

// ---------------------------------------------------------------- people

// DemoPassword is shared by every seeded badge login. It is printed by the
// command and is the reason --confirm exists.
const DemoPassword = "demo1234"

type cardholderFixture struct {
	ExternalID, Name, Email string
	Roles                   []string
	Status                  string // active | suspended
	Kind                    string // holder | visitor

	// BadgeLogin lets this person sign in at /login?as=badge and see their own
	// badge. A cardholder with no email cannot sign in by any method, which is a
	// state worth having in the data — see the two below.
	BadgeLogin bool

	// ValidFrom / ValidUntil, when set, bound the person's credential rather than
	// the person. Dates are day offsets from the seed run.
	ValidFromDays, ValidUntilDays int
	Bounded                       bool

	// Credential is the card value. Visitors and holders alike carry one.
	Credential, CredentialType, CredentialLabel string
	CredentialStatus                            string // active | revoked | suspended
}

// Three of these share a name and email with the platform demo's Northwind
// members (dana, raj, elena). One company, two apps.
var cardholders = []cardholderFixture{
	{ExternalID: "nw-dana", Name: "Dana Whitfield", Email: "dana@northwind.example",
		Roles: []string{"nw-staff", "nw-it"}, Status: "active", Kind: "holder", BadgeLogin: true,
		Credential: "NW-CARD-0001", CredentialType: "wiegand", CredentialLabel: "Dana badge", CredentialStatus: "active"},

	{ExternalID: "nw-raj", Name: "Raj Malhotra", Email: "raj@northwind.example",
		Roles: []string{"nw-facilities"}, Status: "active", Kind: "holder", BadgeLogin: true,
		Credential: "NW-CARD-0002", CredentialType: "wiegand", CredentialLabel: "Raj badge", CredentialStatus: "active"},

	{ExternalID: "nw-elena", Name: "Elena Sokolova", Email: "elena@northwind.example",
		Roles: []string{"nw-warehouse"}, Status: "active", Kind: "holder", BadgeLogin: true,
		Credential: "NW-CARD-0003", CredentialType: "wiegand", CredentialLabel: "Elena badge", CredentialStatus: "active"},

	{ExternalID: "nw-marco", Name: "Marco Ferreira", Email: "marco@northwind.example",
		Roles: []string{"nw-warehouse"}, Status: "active", Kind: "holder", BadgeLogin: true,
		Credential: "NW-CARD-0004", CredentialType: "wiegand", CredentialLabel: "Marco badge", CredentialStatus: "active"},

	{ExternalID: "nw-tasha", Name: "Tasha Bell", Email: "tasha@northwind.example",
		Roles: []string{"nw-security"}, Status: "active", Kind: "holder", BadgeLogin: true,
		Credential: "NW-CARD-0005", CredentialType: "wiegand", CredentialLabel: "Tasha badge", CredentialStatus: "active"},

	{ExternalID: "nw-owen", Name: "Owen Pryce", Email: "owen@northwind.example",
		Roles: []string{"nw-driver"}, Status: "active", Kind: "holder", BadgeLogin: true,
		Credential: "NW-CARD-0006", CredentialType: "wiegand", CredentialLabel: "Owen badge", CredentialStatus: "active"},

	{ExternalID: "nw-priya", Name: "Priya Raman", Email: "priya@northwind.example",
		Roles: []string{"nw-cleaning"}, Status: "active", Kind: "holder", BadgeLogin: true,
		Credential: "NW-CARD-0007", CredentialType: "wiegand", CredentialLabel: "Priya badge", CredentialStatus: "active"},

	// Suspended person, active card. The person's status denies before the
	// credential is ever consulted, which is the ladder subjectFor implements —
	// and it reads differently on the badge screen from a revoked card.
	{ExternalID: "nw-glen", Name: "Glen Hargrove", Email: "glen@northwind.example",
		Roles: []string{"nw-warehouse"}, Status: "suspended", Kind: "holder", BadgeLogin: true,
		Credential: "NW-CARD-0008", CredentialType: "wiegand", CredentialLabel: "Glen badge", CredentialStatus: "active"},

	// Active person, revoked card. The mirror image of Glen, and the pair is the
	// point: two identical-looking denials with different causes.
	{ExternalID: "nw-brett", Name: "Brett Nolan", Email: "brett@northwind.example",
		Roles: []string{"nw-warehouse"}, Status: "active", Kind: "holder", BadgeLogin: true,
		Credential: "NW-CARD-0009", CredentialType: "wiegand", CredentialLabel: "Brett badge (lost)", CredentialStatus: "revoked"},

	// Enrolled, granted nothing. deny_no_access, not deny_no_credential.
	{ExternalID: "nw-casey", Name: "Casey Nakamura", Email: "casey@msp.example",
		Roles: []string{"nw-contractor"}, Status: "active", Kind: "holder", BadgeLogin: true,
		Credential: "NW-CARD-0010", CredentialType: "wiegand", CredentialLabel: "Contractor badge", CredentialStatus: "active"},

	// No email: cannot sign in by any method, and that is the intended state for
	// a card that lives in a drawer rather than a pocket.
	{ExternalID: "nw-spare-dock", Name: "Loading Dock Spare Card",
		Roles: []string{"nw-warehouse"}, Status: "active", Kind: "holder",
		Credential: "NW-CARD-0100", CredentialType: "wiegand", CredentialLabel: "Dock spare", CredentialStatus: "active"},

	{ExternalID: "nw-fire-lockbox", Name: "Fire Dept Lockbox",
		Roles: []string{"nw-facilities"}, Status: "active", Kind: "holder",
		Credential: "NW-CARD-0101", CredentialType: "wiegand", CredentialLabel: "Fire lockbox", CredentialStatus: "active"},

	// ---- visitors, in the three states a pass can be in
	{ExternalID: "nw-visit-live", Name: "Sandra Okonkwo (Auditor)", Email: "sandra@auditco.example",
		Roles: []string{"nw-staff"}, Status: "active", Kind: "visitor", BadgeLogin: true,
		Bounded: true, ValidFromDays: -1, ValidUntilDays: 3,
		Credential: "NW-VISIT-001", CredentialType: "wiegand", CredentialLabel: "Audit visit", CredentialStatus: "active"},

	{ExternalID: "nw-visit-expired", Name: "Dale Whitmore (Vendor)", Email: "dale@vendorco.example",
		Roles: []string{"nw-staff"}, Status: "active", Kind: "visitor", BadgeLogin: true,
		Bounded: true, ValidFromDays: -30, ValidUntilDays: -14,
		Credential: "NW-VISIT-002", CredentialType: "wiegand", CredentialLabel: "Expired vendor visit", CredentialStatus: "active"},

	{ExternalID: "nw-visit-revoked", Name: "Kim Alvarado (Vendor)", Email: "kim@vendorco.example",
		Roles: []string{"nw-staff"}, Status: "active", Kind: "visitor", BadgeLogin: true,
		Bounded: true, ValidFromDays: -7, ValidUntilDays: 7,
		Credential: "NW-VISIT-003", CredentialType: "wiegand", CredentialLabel: "Revoked vendor visit", CredentialStatus: "revoked"},
}
