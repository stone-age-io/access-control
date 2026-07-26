package badgeapi

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

// floorplanXY is what decides whether something appears on a badge's plan, and its
// failure mode matters: a malformed position must read as "not placed", never as a
// marker at the origin — that would stack every broken item in one corner and look like
// a placement bug rather than a data one.
func TestFloorplanXY(t *testing.T) {
	app := newApp(t)
	col, err := app.FindCollectionByNameOrId("portals")
	if err != nil {
		t.Fatalf("portals collection: %v", err)
	}

	tests := []struct {
		name     string
		position any
		wantOK   bool
		wantX    float64
		wantY    float64
	}{
		{"placed", map[string]any{"x": 120, "y": 340}, true, 120, 340},
		{"placed with floats", map[string]any{"x": 12.5, "y": 7.25}, true, 12.5, 7.25},
		{"at the origin is a real position", map[string]any{"x": 0, "y": 0}, true, 0, 0},
		{"absent", nil, false, 0, 0},
		{"empty object", map[string]any{}, false, 0, 0},
		{"x only", map[string]any{"x": 10}, false, 0, 0},
		{"non-numeric", map[string]any{"x": "10", "y": "20"}, false, 0, 0},
		{"wrong shape entirely", []any{1, 2}, false, 0, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := core.NewRecord(col)
			if tc.position != nil {
				rec.Set("floorplan_position", tc.position)
			}
			x, y, ok := floorplanXY(rec)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (position %v)", ok, tc.wantOK, tc.position)
			}
			if ok && (x != tc.wantX || y != tc.wantY) {
				t.Errorf("(x, y) = (%v, %v), want (%v, %v)", x, y, tc.wantX, tc.wantY)
			}
		})
	}
}

func TestPlacedPoint(t *testing.T) {
	app := newApp(t)
	col, err := app.FindCollectionByNameOrId("portals")
	if err != nil {
		t.Fatalf("portals collection: %v", err)
	}

	// A nameless portal falls back to its code rather than rendering a blank pin.
	rec := core.NewRecord(col)
	rec.Set("floorplan_position", map[string]any{"x": 5, "y": 6})
	rec.Set("allow_remote_unlock", true)
	p, ok := placedPoint(rec, "east-lobby", "allow_remote_unlock")
	if !ok {
		t.Fatal("placed portal reported as unplaced")
	}
	if p.Name != "east-lobby" {
		t.Errorf("name = %q, want the code as a fallback", p.Name)
	}
	if !p.Remote {
		t.Error("remote = false, want true")
	}

	// An unplaced one is skipped; the Access tab's list is where it still appears.
	if _, ok := placedPoint(core.NewRecord(col), "east-lobby", "allow_remote_unlock"); ok {
		t.Error("an unplaced portal was reported as placed")
	}
}

// The URL must match what the SDK's files.getURL produces, or the badge and the operator
// map would load different (and one of them broken) assets.
func TestFileURLShape(t *testing.T) {
	app := newApp(t)
	h := testHandler(app)

	loc, err := app.FindFirstRecordByData("locations", "code", "hq")
	if err != nil {
		t.Fatalf("fixture location: %v", err)
	}
	got := h.fileURL(loc, "plan.png")
	want := "/api/files/" + loc.Collection().Id + "/" + loc.Id + "/plan.png"
	if got != want {
		t.Errorf("fileURL = %q, want %q", got, want)
	}
}
