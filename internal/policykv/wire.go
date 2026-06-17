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
// "cred.CARD-001", "user.<pbid>", "point.lobby-main".
const (
	PrefixSite  = "site."
	PrefixSched = "sched."
	PrefixPoint = "point."
	PrefixGroup = "group."
	PrefixRole  = "role."
	PrefixUser  = "user."
	PrefixCred  = "cred."
)

// Site carries the timezone (the controller resolves it once per evaluation)
// and the fire-alarm-input alarm-suppression flag.
type Site struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Timezone    string `json:"timezone"`
	FAISuppress bool   `json:"faiSuppress"`
}

// Window is one recurring time window. Days are ISO weekdays (1=Mon..7=Sun);
// Start/End are "HH:MM" local wall-clock; End<=Start crosses midnight.
type Window struct {
	Days  []int  `json:"days"`
	Start string `json:"start"`
	End   string `json:"end"`
}

// Schedule is a reusable set of weekly windows.
type Schedule struct {
	Code    string   `json:"code"`
	Windows []Window `json:"windows"`
}

// AccessPoint references its site by code; Posture is the standing default.
type AccessPoint struct {
	Code         string `json:"code"`
	Site         string `json:"site"`
	Posture      string `json:"posture"`
	PulseSeconds int    `json:"pulseSeconds"`
}

// AccessGroup grants a set of access points (by code) under one schedule (by code).
type AccessGroup struct {
	Code     string   `json:"code"`
	Points   []string `json:"points"`
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

// Credential maps an opaque value to a cardholder (by id).
type Credential struct {
	Value  string `json:"value"`
	User   string `json:"user"`
	Status string `json:"status"`
}
