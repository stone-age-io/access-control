// Package diag serves the access-controller's opt-in, read-only local
// diagnostics page (HTML at /status, JSON at /status.json) for field install and
// troubleshooting. It renders the controller's live in-memory state — identity,
// NATS/policy-sync health, the portals it bound and their door/posture state, and
// recent decisions — by reading existing accessors on the PolicyStore and
// Runtime. It is strictly read-only: it never mutates state and exposes no control
// path (all control stays on the NATS command plane).
package diag

import (
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stone-age-io/access-control/config"
	"github.com/stone-age-io/access-control/internal/controller"
)

// Report is the full diagnostics snapshot, serialized to /status.json and
// rendered by the /status HTML template.
type Report struct {
	GeneratedAt time.Time                   `json:"generatedAt"`
	Identity    Identity                    `json:"identity"`
	Build       Build                       `json:"build"`
	NATS        NATSStatus                  `json:"nats"`
	Policy      PolicyStatus                `json:"policy"`
	Portals     []PortalView                `json:"portals"`
	Fire        []FireStatus                `json:"fire"`
	AuxOutputs  []controller.AuxLive        `json:"auxOutputs"`
	AuxInputs   []controller.AuxLive        `json:"auxInputs"`
	Decisions   []controller.DecisionRecord `json:"decisions"` // most-recent first
	Alarms      []controller.AlarmRecord    `json:"alarms"`    // most-recent first
}

// Identity is the controller's configured identity and hardware selection.
type Identity struct {
	Controller  string    `json:"controller"`
	Location    string    `json:"location"`
	SubjectsApp string    `json:"subjectsApp"`
	Driver      string    `json:"driver"`
	Model       string    `json:"model"`
	Reader      string    `json:"reader"`
	StartedAt   time.Time `json:"startedAt"`
	Uptime      string    `json:"uptime"`
}

// Build is the binary's build provenance, read from the embedded VCS info.
type Build struct {
	GoVersion string `json:"goVersion"`
	Revision  string `json:"revision"`
	Time      string `json:"time"`
	Modified  bool   `json:"modified"`
}

// NATSStatus is the controller's NATS connection health.
type NATSStatus struct {
	Connected  bool   `json:"connected"`
	URL        string `json:"url"`
	Reconnects uint64 `json:"reconnects"`
}

// PolicyStatus reports whether policy has synced and what loaded.
type PolicyStatus struct {
	Synced bool           `json:"synced"`
	Counts map[string]int `json:"counts"`
}

// PortalView joins a portal bound to this controller (the authoritative "what
// this box should drive" set) with its live runtime state and hardware binding.
// Armed is false when a bound portal has no live state yet — the "bound, not
// armed" case worth surfacing during install.
type PortalView struct {
	Code     string `json:"code"`
	Type     string `json:"type"`
	Location string `json:"location"`
	Armed    bool   `json:"armed"`

	Posture  string `json:"posture"`
	Source   string `json:"source"`
	Door     string `json:"door"`
	Held     bool   `json:"held"`
	Override string `json:"override"`
	AuthOpen bool   `json:"authOpen"`

	LockRelay       int `json:"lockRelay"`
	DpsInput        int `json:"dpsInput"`
	RexInput        int `json:"rexInput"`
	HeldOpenSeconds int `json:"heldOpenSeconds"`
	ReaderAddress   int `json:"readerAddress"`

	// Logical wiring sense the box armed each line with (vs. the board's electrical
	// polarity). Maglock = fail-safe energize-to-lock; DpsInvert/RexInvert = the
	// contact reads normally-open (DPS) / normally-closed (REX); RexUnlock = a REX
	// press also pulses the strike.
	Maglock   bool `json:"maglock"`
	DpsInvert bool `json:"dpsInvert"`
	RexInvert bool `json:"rexInvert"`
	RexUnlock bool `json:"rexUnlock"`
}

// FireStatus reports a location's fire-input state (active = alarms suppressed).
type FireStatus struct {
	Location string `json:"location"`
	Active   bool   `json:"active"`
}

// ReportSource produces a fresh Report on demand. The HTTP handlers depend on
// this interface so they can be tested with a fake.
type ReportSource interface {
	Report() Report
}

// Source is the production ReportSource: it aggregates a Report from the
// controller's PolicyStore, Runtime, and NATS connection.
type Source struct {
	ident     Identity // static identity captured at startup (uptime filled per call)
	ctrlCode  string
	store     *controller.PolicyStore
	rt        *controller.Runtime
	nc        *nats.Conn
	build     Build
	startedAt time.Time
}

// NewSource captures the static identity/build and the live components to read.
// startedAt is the process start, for the uptime display.
func NewSource(cfg *config.Config, store *controller.PolicyStore, rt *controller.Runtime, nc *nats.Conn, startedAt time.Time) *Source {
	return &Source{
		ident: Identity{
			Controller:  cfg.Controller.Code,
			Location:    cfg.Controller.Location,
			SubjectsApp: cfg.Subjects.App,
			Driver:      cfg.Controller.Driver,
			Model:       cfg.Controller.Model,
			Reader:      cfg.Controller.Reader,
		},
		ctrlCode:  cfg.Controller.Code,
		store:     store,
		rt:        rt,
		nc:        nc,
		build:     readBuild(),
		startedAt: startedAt,
	}
}

// Report builds a fresh diagnostics report from live state.
func (s *Source) Report() Report {
	now := time.Now().UTC()

	ident := s.ident
	ident.StartedAt = s.startedAt
	ident.Uptime = now.Sub(s.startedAt).Round(time.Second).String()

	snap := s.rt.Snapshot(now)

	// Bound portals (authoritative set this box should drive) joined with live
	// state and the hardware binding. A bound portal absent from the live set is
	// "bound, not armed".
	var portals []PortalView
	for _, p := range s.store.PortalsForController(s.ctrlCode) {
		pv := PortalView{Code: p.Code, Type: p.Type, Location: p.Location}
		if live, ok := snap.Portals[p.Code]; ok {
			pv.Armed = true
			pv.Posture = live.Posture
			pv.Source = live.Source
			pv.Door = live.Door
			pv.Held = live.Held
			pv.Override = live.Override
			pv.AuthOpen = live.AuthOpen
		}
		if b, ok := s.store.Binding(p.Code); ok {
			pv.LockRelay = b.LockRelay
			pv.DpsInput = b.DpsInput
			pv.RexInput = b.RexInput
			pv.HeldOpenSeconds = b.HeldOpenSeconds
			pv.ReaderAddress = b.ReaderAddress
			pv.Maglock = b.Maglock
			pv.DpsInvert = b.DpsInvert
			pv.RexInvert = b.RexInvert
			pv.RexUnlock = b.RexUnlock
		}
		portals = append(portals, pv)
	}
	slices.SortFunc(portals, func(a, b PortalView) int { return strings.Compare(a.Code, b.Code) })

	var fire []FireStatus
	for loc, on := range snap.Fire {
		fire = append(fire, FireStatus{Location: loc, Active: on})
	}
	slices.SortFunc(fire, func(a, b FireStatus) int { return strings.Compare(a.Location, b.Location) })

	ns := NATSStatus{}
	if s.nc != nil {
		ns.Connected = s.nc.IsConnected()
		ns.URL = s.nc.ConnectedUrl()
		ns.Reconnects = s.nc.Stats().Reconnects
	}

	// Show newest first.
	decisions := snap.Decisions
	slices.Reverse(decisions)
	alarms := snap.Alarms
	slices.Reverse(alarms)

	return Report{
		GeneratedAt: now,
		Identity:    ident,
		Build:       s.build,
		NATS:        ns,
		Policy:      PolicyStatus{Synced: s.store.Ready(), Counts: s.store.Counts()},
		Portals:     portals,
		Fire:        fire,
		AuxOutputs:  snap.AuxOutputs,
		AuxInputs:   snap.AuxInputs,
		Decisions:   decisions,
		Alarms:      alarms,
	}
}

// readBuild reads the binary's Go version and embedded VCS provenance. Go stamps
// vcs.* settings automatically when building from a checkout, so this needs no
// ldflags or build-time wiring.
func readBuild() Build {
	b := Build{GoVersion: runtime.Version()}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return b
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			b.Revision = setting.Value
		case "vcs.time":
			b.Time = setting.Value
		case "vcs.modified":
			b.Modified = setting.Value == "true"
		}
	}
	return b
}
