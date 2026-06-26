package simulateapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stone-age-io/access-control/internal/policy"
	"github.com/stone-age-io/access-control/internal/policykv"
)

var at = time.Date(2026, 6, 25, 14, 0, 0, 0, time.UTC)

func isoWD(t time.Time) int {
	if wd := int(t.Weekday()); wd != 0 {
		return wd
	}
	return 7
}

func mk(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// grantEntries is a minimal setup that grants cred C1 at portal door1 (open 24h).
func grantEntries(t *testing.T) map[string][]byte {
	open := policykv.Window{Days: []int{isoWD(at)}, Start: "00:00", End: "24:00"}
	return map[string][]byte{
		policykv.PrefixLocation + "hq":  mk(t, policykv.Location{Code: "hq", Timezone: "UTC"}),
		policykv.PrefixSched + "s1":     mk(t, policykv.Schedule{Code: "s1", Windows: []policykv.Window{open}}),
		policykv.PrefixPortal + "door1": mk(t, policykv.Portal{Code: "door1", Type: "door", Location: "hq", Posture: "secure", PulseSeconds: 5}),
		policykv.PrefixGroup + "g1":     mk(t, policykv.AccessGroup{Code: "g1", Portals: []string{"door1"}, Schedule: "s1"}),
		policykv.PrefixRole + "r1":      mk(t, policykv.Role{Code: "r1", Groups: []string{"g1"}}),
		policykv.PrefixUser + "u1":      mk(t, policykv.User{ID: "u1", Status: "active", Roles: []string{"r1"}}),
		policykv.PrefixCred + "C1":      mk(t, policykv.Credential{Value: "C1", User: "u1", Status: "active"}),
	}
}

func TestEvaluate_Grant(t *testing.T) {
	res, err := evaluate(grantEntries(t), request{Credential: "C1", Portal: "door1", At: at.Format(time.RFC3339)}, at)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Allow || res.Reason != policy.ReasonAllowGrant {
		t.Fatalf("want allow_grant, got allow=%v reason=%q", res.Allow, res.Reason)
	}
	if !res.PortalKnown || !res.CredKnown || res.Location != "hq" {
		t.Fatalf("context wrong: portalKnown=%v credKnown=%v loc=%q", res.PortalKnown, res.CredKnown, res.Location)
	}
}

func TestEvaluate_DefaultsToNow(t *testing.T) {
	// At omitted → uses the passed `now`; here `now`=at, so the open window grants.
	res, err := evaluate(grantEntries(t), request{Credential: "C1", Portal: "door1"}, at)
	if err != nil || !res.Allow {
		t.Fatalf("want grant using now fallback, got allow=%v err=%v", res.Allow, err)
	}
}

func TestEvaluate_BadInput(t *testing.T) {
	cases := []struct{ name string; req request }{
		{"missing portal", request{Credential: "C1"}},
		{"bad posture", request{Portal: "door1", Posture: "bogus"}},
		{"bad time", request{Portal: "door1", At: "not-a-time"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := evaluate(grantEntries(t), c.req, at); err == nil {
				t.Fatalf("expected a bad-input error, got nil")
			} else if _, ok := err.(badInput); !ok {
				t.Fatalf("expected badInput, got %T", err)
			}
		})
	}
}
