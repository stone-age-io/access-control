// Package modelsapi exposes accessd's read-only hardware-model catalogue: for each
// supported controller model, the count and physical labels of its logical relay
// and input lines. The management UI uses it to render the controller I/O map and
// to bound/annotate the relay/input index pickers on the portal and aux-I/O forms,
// so the capacity (8 relays + 8 inputs, the logical→physical mapping) lives once in
// internal/drivers/hardware rather than being duplicated in the frontend.
//
// The catalogue is static — derived from the compiled-in profiles — so the route is
// a plain GET with no NATS or PocketBase reads. Any authenticated operator may read
// it (every role, including viewer, needs it to render the controller I/O map).
package modelsapi

import (
	"net/http"
	"sort"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stone-age-io/access-control/internal/drivers/hardware"
)

// line is one logical relay or input: its 1-based index and a physical description.
type line struct {
	Index int    `json:"index"`
	Label string `json:"label"`
}

// model is one hardware profile's capacity as the UI needs it.
type model struct {
	Model     string `json:"model"`
	Transport string `json:"transport"`
	Relays    []line `json:"relays"`
	Inputs    []line `json:"inputs"`
}

// Register wires GET /api/models onto the serve event's router.
func Register(se *core.ServeEvent) {
	se.Router.GET("/api/models", handle).Bind(apis.RequireAuth())
}

func handle(e *core.RequestEvent) error {
	return e.JSON(http.StatusOK, map[string]any{"models": catalogue()})
}

// catalogue builds the model list from the compiled-in hardware profiles, in stable
// (sorted) order. Pure — separated from the HTTP handler so it can be unit-tested.
func catalogue() []model {
	names := hardware.Models()
	sort.Strings(names)
	out := make([]model, 0, len(names))
	for _, name := range names {
		p, ok := hardware.ProfileFor(name)
		if !ok {
			continue
		}
		out = append(out, model{
			Model:     name,
			Transport: string(p.Transport()),
			Relays:    lines(p.RelayCount(), p.Relay),
			Inputs:    lines(p.InputCount(), p.Input),
		})
	}
	return out
}

// lines turns a profile's 1..n logical lines into the wire shape, skipping any gap
// (a defined model has no gaps, but the resolver is fail-safe).
func lines(n int, at func(int) (hardware.LineSpec, bool)) []line {
	out := make([]line, 0, n)
	for i := 1; i <= n; i++ {
		spec, ok := at(i)
		if !ok {
			continue
		}
		out = append(out, line{Index: i, Label: spec.Label()})
	}
	return out
}
