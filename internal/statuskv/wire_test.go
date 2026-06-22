package statuskv

import (
	"encoding/json"
	"testing"
)

func TestPortalStatusRoundTrip(t *testing.T) {
	in := PortalStatus{
		Code:       "lobby-main",
		Location:   "hq",
		Controller: "ctrl-hq-1",
		Door:       DoorOpen,
		Posture:    "secure",
		Held:       true,
		UpdatedAt:  "2026-01-05T09:00:00Z",
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out PortalStatus
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out != in {
		t.Errorf("round-trip = %+v, want %+v", out, in)
	}
}

// The JSON field names are a wire contract shared with accessd's projector and
// the UI; pin them.
func TestPortalStatusJSONShape(t *testing.T) {
	b, _ := json.Marshal(PortalStatus{Code: "c", Door: DoorClosed, Posture: "secure", Source: PostureSourceStanding})
	const want = `{"code":"c","location":"","controller":"","door":"closed","posture":"secure","source":"standing","held":false,"updatedAt":""}`
	if string(b) != want {
		t.Errorf("json = %s, want %s", b, want)
	}
}
