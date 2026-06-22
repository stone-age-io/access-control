package controller

import (
	"sync"
	"time"

	"github.com/stone-age-io/access-control/internal/statuskv"
)

// recentRingSize is how many recent decisions and alarms the runtime retains for
// the diagnostics page. Small and fixed: this is a troubleshooting tail, not an
// audit log (JetStream is the system of record for events).
const recentRingSize = 50

// DecisionRecord is one recent credential decision, retained for the diagnostics
// page so a field tech can tap a card and see exactly how it resolved. The
// credential value is kept verbatim — confirming the decoded card number is the
// point of the view.
type DecisionRecord struct {
	At     time.Time `json:"at"`
	Portal string    `json:"portal"`
	Cred   string    `json:"cred"`
	User   string    `json:"user"`
	Allow  bool      `json:"allow"`
	Reason string    `json:"reason"`
}

// AlarmRecord is one recent emitted door alarm (forced/held/held_clear), retained
// for the diagnostics page so door wiring can be verified during install.
type AlarmRecord struct {
	At     time.Time `json:"at"`
	Portal string    `json:"portal"`
	Kind   string    `json:"kind"`
}

// PortalLive is the runtime's live view of one driven portal, as shown on the
// diagnostics page. It carries only values — never the door monitor's timer or a
// driver handle — so it is safe to read from the HTTP goroutine.
type PortalLive struct {
	Posture  string `json:"posture"`  // effective posture
	Source   string `json:"source"`   // statuskv.PostureSource* — provenance of Posture
	Door     string `json:"door"`     // statuskv.Door* (open/closed/unknown)
	Held     bool   `json:"held"`     // held-open alarm currently active
	Override string `json:"override"` // active runtime posture override ("" = none)
	AuthOpen bool   `json:"authOpen"` // a grant/REX authorized-open window is currently open
}

// AuxLive is the runtime's live view of one aux point. Energized applies to
// outputs, Active to inputs.
type AuxLive struct {
	Code      string `json:"code"`
	Location  string `json:"location"`
	Energized bool   `json:"energized"`
	Active    bool   `json:"active"`
}

// RuntimeSnapshot is a read-only copy of the runtime's live state at an instant,
// for the opt-in diagnostics page. It holds no pointers into live state.
type RuntimeSnapshot struct {
	Portals    map[string]PortalLive `json:"portals"` // keyed by driven portal code
	Fire       map[string]bool       `json:"fire"`    // location -> fire input active (suppresses alarms)
	AuxOutputs []AuxLive             `json:"auxOutputs"`
	AuxInputs  []AuxLive             `json:"auxInputs"`
	Decisions  []DecisionRecord      `json:"decisions"` // oldest first
	Alarms     []AlarmRecord         `json:"alarms"`    // oldest first
}

// Snapshot returns a read-only copy of the runtime's live state, resolved at the
// given instant. It is for the diagnostics page only and is not on any hot path.
//
// It reads raw state under r.mu (a brief copy), then resolves each portal's
// effective posture via the store WITHOUT holding r.mu — ResolvePosture takes the
// store lock, and r.mu is not reentrant (the same sequencing writeStatus uses).
func (r *Runtime) Snapshot(at time.Time) RuntimeSnapshot {
	// raw is the per-portal state copied under the lock; posture is resolved after.
	type raw struct {
		override   string
		open       bool
		dpsSeen    bool
		held       bool
		grantUntil time.Time
		rexUntil   time.Time
	}

	r.mu.RLock()
	portals := make(map[string]raw, len(r.locks))
	for code := range r.locks {
		rw := raw{override: r.overrides[code]}
		if m := r.monitors[code]; m != nil {
			rw.open = m.open
			rw.dpsSeen = m.dpsSeen
			rw.held = m.held
			rw.grantUntil = m.grantUntil
			rw.rexUntil = m.rexUntil
		}
		portals[code] = rw
	}
	fire := make(map[string]bool, len(r.fire))
	for loc, on := range r.fire {
		fire[loc] = on
	}
	auxOut := make([]AuxLive, 0, len(r.auxOutputs))
	for code, st := range r.auxOutputs {
		auxOut = append(auxOut, AuxLive{Code: code, Location: st.location, Energized: st.energized})
	}
	auxIn := make([]AuxLive, 0, len(r.auxInputs))
	for code, st := range r.auxInputs {
		auxIn = append(auxIn, AuxLive{Code: code, Location: st.location, Active: st.active})
	}
	hasInput := r.input != nil
	r.mu.RUnlock()

	live := make(map[string]PortalLive, len(portals))
	for code, rw := range portals {
		posture, source, _ := r.store.ResolvePosture(code, rw.override, at)
		door := statuskv.DoorUnknown
		if hasInput && rw.dpsSeen {
			if rw.open {
				door = statuskv.DoorOpen
			} else {
				door = statuskv.DoorClosed
			}
		}
		live[code] = PortalLive{
			Posture:  posture,
			Source:   source,
			Door:     door,
			Held:     rw.held,
			Override: rw.override,
			AuthOpen: at.Before(rw.grantUntil) || at.Before(rw.rexUntil),
		}
	}

	return RuntimeSnapshot{
		Portals:    live,
		Fire:       fire,
		AuxOutputs: auxOut,
		AuxInputs:  auxIn,
		Decisions:  r.decisions.snapshot(),
		Alarms:     r.alarms.snapshot(),
	}
}

// ring is a fixed-capacity, mutex-guarded ring buffer of recent records. The
// runtime appends from the tap loop and the alarm path; the diagnostics page
// reads a copy from the HTTP goroutine. Its own mutex keeps these appends off the
// decision RWMutex.
type ring[T any] struct {
	mu   sync.Mutex
	buf  []T
	next int
	full bool
}

func newRing[T any](size int) *ring[T] {
	return &ring[T]{buf: make([]T, size)}
}

// add records v, overwriting the oldest entry once the buffer is full.
func (r *ring[T]) add(v T) {
	r.mu.Lock()
	r.buf[r.next] = v
	r.next = (r.next + 1) % len(r.buf)
	if r.next == 0 {
		r.full = true
	}
	r.mu.Unlock()
}

// snapshot returns the retained records oldest-first.
func (r *ring[T]) snapshot() []T {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.full {
		out := make([]T, r.next)
		copy(out, r.buf[:r.next])
		return out
	}
	out := make([]T, 0, len(r.buf))
	out = append(out, r.buf[r.next:]...)
	out = append(out, r.buf[:r.next]...)
	return out
}
