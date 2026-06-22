package authz

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

// recordWith builds an in-memory users-like record carrying the given
// permissions, without needing a running app.
func recordWith(perms ...string) *core.Record {
	col := core.NewBaseCollection("users")
	col.Fields.Add(&core.SelectField{
		Name:      "permissions",
		Values:    []string{CapEnroll, CapPolicy, CapTopology, CapCommand, CapOperators},
		MaxSelect: 5,
	})
	rec := core.NewRecord(col)
	rec.Set("permissions", perms)
	return rec
}

func TestHasCapability(t *testing.T) {
	cases := []struct {
		name string
		rec  *core.Record
		cap  string
		want bool
	}{
		{"present", recordWith(CapEnroll, CapCommand), CapEnroll, true},
		{"present second", recordWith(CapEnroll, CapCommand), CapCommand, true},
		{"absent", recordWith(CapEnroll), CapTopology, false},
		{"empty perms", recordWith(), CapEnroll, false},
		{"nil record", nil, CapEnroll, false},
		// substring-free names: holding "operators" must not satisfy a different cap.
		{"no substring bleed", recordWith(CapOperators), CapPolicy, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasCapability(tc.rec, tc.cap); got != tc.want {
				t.Errorf("HasCapability(%v, %q) = %v, want %v", tc.rec.GetStringSlice("permissions"), tc.cap, got, tc.want)
			}
		})
	}
}
