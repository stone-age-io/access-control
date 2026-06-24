package main

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

// areaRecord builds an in-memory areas record with the given field values (no DB).
func areaRecord(t *testing.T, fields map[string]string) *core.Record {
	t.Helper()
	col := core.NewBaseCollection("areas")
	col.Fields.Add(&core.TextField{Name: "arm"})
	col.Fields.Add(&core.TextField{Name: "arm_override"})
	col.Fields.Add(&core.TextField{Name: "auto_arm"})
	rec := core.NewRecord(col)
	for k, v := range fields {
		rec.Set(k, v)
	}
	return rec
}

// shouldDisarm disarms an area that could be armed and isn't already explicitly
// overridden-disarmed; it skips permanently-disarmed areas to avoid churn.
func TestShouldDisarm(t *testing.T) {
	cases := []struct {
		name   string
		fields map[string]string
		want   bool
	}{
		{"standing armed", map[string]string{"arm": "armed"}, true},
		{"override armed", map[string]string{"arm_override": "armed"}, true},
		{"scheduled armable", map[string]string{"arm": "disarmed", "auto_arm": "armed"}, true},
		{"override disarmed wins over standing armed", map[string]string{"arm": "armed", "arm_override": "disarmed"}, false},
		{"never armable (standing disarmed)", map[string]string{"arm": "disarmed"}, false},
		{"scheduled to disarm only", map[string]string{"arm": "disarmed", "auto_arm": "disarmed"}, false},
		{"empty", map[string]string{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldDisarm(areaRecord(t, tc.fields)); got != tc.want {
				t.Errorf("shouldDisarm(%v) = %v, want %v", tc.fields, got, tc.want)
			}
		})
	}
}
