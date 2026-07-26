package badgeapi

import (
	"net/http"
	"sort"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// GET /api/badge/live — the holder's own doors and controls, placed on the floor plan of
// each site that opted in (migration 1750000041).
//
// # Why this is a server projection and not widened collection rules
//
// The operator's Live View is built entirely on direct collection reads and PocketBase
// realtime subscriptions — portals, areas, aux I/O, point_status, locations. A badge
// token can read none of that, and it must not: PocketBase rules are ROW-level with no
// field granularity, so any rule letting a holder see their own portal row would also
// hand them `lock_relay`, `dps_input`, `reader_address` and the portal's policy code. On
// top of that, "this portal is in a group in a role I hold" is a deep back-relation
// filter — fragile to write and worse to maintain than a projection that simply decides
// server-side what one person may see.
//
// So this route sends names, positions, and the two remote opt-ins. No codes, no wiring,
// no controllers, no other people's doors.
//
// # What it deliberately does not do
//
//   - No live state, and no realtime. The operator console's `point_status` stream is how
//     you watch a building; a badge is a thing you use to get into one. The client polls
//     /api/badge/me for an area's arm-state, which is the only state a holder can act on.
//   - No areas on the plan. Only portals and aux I/O carry a `floorplan_position`
//     (1750000011, 1750000026); an area is a set spanning several points, with no single
//     place to put a pin. Areas stay in the Access tab's list, where their two buttons fit.
//   - No unplaced items. Something with no position has nowhere to go on the plan; it is
//     still in the list, which is the complete surface.

// liveResponse is one entry per site the holder has something placed at, and nothing at
// all when no site opted in — which is the default, and which the client renders as
// simply not offering a plan.
type liveResponse struct {
	Locations []liveLocation `json:"locations"`
}

type liveLocation struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Floorplan is a ready-made URL. `locations.floorplan` is an ordinary public file
	// field (the operator map loads it without a token), so no file token is needed —
	// see 1750000041 for what the opt-in does and does not protect.
	Floorplan string      `json:"floorplan"`
	Portals   []livePoint `json:"portals"`
	Outputs   []livePoint `json:"outputs"`
}

// livePoint is one pinnable thing. X/Y are pixel coordinates on the floor-plan image, the
// same space the operator's editor writes; the client turns them into percentages once
// the image reports its natural size.
type livePoint struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Remote bool    `json:"remote"` // allow_remote_unlock / allow_remote
}

func (h *handler) registerLiveRoute(se *core.ServeEvent) {
	// Badge tier only. An operator has the real Live View, with live state and command
	// authority; giving them a second, narrower one here would be redundant.
	se.Router.GET("/api/badge/live", h.live).Bind(apis.RequireAuth(BadgeCollection))
}

func (h *handler) live(e *core.RequestEvent) error {
	cardholder := e.Auth

	snap, err := h.snapshot(e.Request.Context())
	if err != nil {
		// Fail soft with an empty list: the Access tab's own list still works, so a NATS
		// hiccup costs the plan rather than the page.
		h.log.Error("badge live: policy snapshot unavailable", "cardholder", cardholder.Id, "error", err)
		return e.JSON(http.StatusOK, liveResponse{Locations: []liveLocation{}})
	}

	// Group the holder's placed things by location record id.
	byLocation := map[string]*liveLocation{}
	// ensure returns the entry for a location, or nil when that location has no plan to
	// show — either it has not opted in or it has no image. Cached per location so a
	// holder with twenty doors at one site reads the record once.
	checked := map[string]bool{}
	ensure := func(locationID string) *liveLocation {
		if locationID == "" {
			return nil
		}
		if entry, ok := byLocation[locationID]; ok {
			return entry
		}
		if checked[locationID] {
			return nil // already looked at and rejected
		}
		checked[locationID] = true

		loc, err := h.app.FindRecordById("locations", locationID)
		if err != nil {
			return nil
		}
		if !loc.GetBool("badge_floorplan") {
			return nil
		}
		file := loc.GetString("floorplan")
		if file == "" {
			return nil // opted in but nothing uploaded: nothing to draw
		}
		name := loc.GetString("name")
		if name == "" {
			name = loc.GetString("code")
		}
		entry := &liveLocation{
			ID:        loc.Id,
			Name:      name,
			Floorplan: h.fileURL(loc, file),
			Portals:   []livePoint{},
			Outputs:   []livePoint{},
		}
		byLocation[locationID] = entry
		return entry
	}

	portalCodes := snap.PortalsFor(cardholder.Id)
	sort.Strings(portalCodes)
	for _, code := range portalCodes {
		rec, err := h.app.FindFirstRecordByData("portals", "code", code)
		if err != nil {
			continue
		}
		entry := ensure(rec.GetString("location"))
		if entry == nil {
			continue
		}
		if p, ok := placedPoint(rec, code, "allow_remote_unlock"); ok {
			entry.Portals = append(entry.Portals, p)
		}
	}

	outputCodes := snap.OutputsFor(cardholder.Id)
	sort.Strings(outputCodes)
	for _, code := range outputCodes {
		rec, err := h.app.FindFirstRecordByData("aux_output", "code", code)
		if err != nil {
			continue
		}
		entry := ensure(rec.GetString("location"))
		if entry == nil {
			continue
		}
		if p, ok := placedPoint(rec, code, "allow_remote"); ok {
			entry.Outputs = append(entry.Outputs, p)
		}
	}

	// A location whose every granted item turned out to be unplaced has an image and
	// nothing to pin on it. Drop it: a plan with no markers tells the holder nothing and
	// shows them the layout for free.
	out := make([]liveLocation, 0, len(byLocation))
	for _, entry := range byLocation {
		if len(entry.Portals) == 0 && len(entry.Outputs) == 0 {
			continue
		}
		out = append(out, *entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return e.JSON(http.StatusOK, liveResponse{Locations: out})
}

// placedPoint reads a record's floorplan_position, reporting false when it is absent or
// malformed. A thing with no position is not an error — it simply is not on the plan.
func placedPoint(rec *core.Record, fallbackName, remoteField string) (livePoint, bool) {
	x, y, ok := floorplanXY(rec)
	if !ok {
		return livePoint{}, false
	}
	name := rec.GetString("name")
	if name == "" {
		name = fallbackName
	}
	return livePoint{
		ID:     rec.Id,
		Name:   name,
		X:      x,
		Y:      y,
		Remote: rec.GetBool(remoteField),
	}, true
}

// floorplanXY decodes the {x, y} JSON the operator's floor-plan editor writes. Anything
// unexpected reads as "not placed" rather than as a marker at the origin, which would put
// every malformed item in one corner of the plan.
func floorplanXY(rec *core.Record) (x, y float64, ok bool) {
	raw, isMap := rec.Get("floorplan_position").(map[string]any)
	if !isMap {
		// PocketBase hands a JSONField back as types.JSONRaw when it has not been
		// unmarshaled; fall back to the record's own typed accessor.
		var pos struct {
			X *float64 `json:"x"`
			Y *float64 `json:"y"`
		}
		if err := rec.UnmarshalJSONField("floorplan_position", &pos); err != nil {
			return 0, 0, false
		}
		if pos.X == nil || pos.Y == nil {
			return 0, 0, false
		}
		return *pos.X, *pos.Y, true
	}
	fx, okX := toFloat(raw["x"])
	fy, okY := toFloat(raw["y"])
	return fx, fy, okX && okY
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

// fileURL builds the public URL for a record's file field, matching what the SDK's
// files.getURL produces so the operator UI and the badge load the same asset.
func (h *handler) fileURL(rec *core.Record, filename string) string {
	return "/api/files/" + rec.Collection().Id + "/" + rec.Id + "/" + filename
}
