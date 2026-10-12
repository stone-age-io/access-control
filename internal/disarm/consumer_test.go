package disarm

import (
	"errors"
	"testing"

	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/subjects"
)

type disarmCall struct{ portal, cred string }

// newDisarmer builds a Disarmer whose DisarmFunc records calls and returns the
// given (disarmed, err) result.
func newDisarmer(t *testing.T, result bool, ferr error) (*Disarmer, *[]disarmCall) {
	t.Helper()
	var calls []disarmCall
	fn := func(portal, cred string) (bool, error) {
		calls = append(calls, disarmCall{portal, cred})
		return result, ferr
	}
	return New(nil, "ACC_EVENTS", subjects.Default(), fn, logger.NewNopLogger(), nil), &calls
}

// A valid credential grant disarms the portal's area, passing the portal + cred.
func TestProcessGrantDisarms(t *testing.T) {
	d, calls := newDisarmer(t, true, nil)
	status, err := d.process("acc.hq.door.lobby-main.evt.tap",
		[]byte(`{"cred":"CARD-001","allow":true,"reason":"allow_grant","ts":"2026-01-05T14:00:00Z"}`))
	if err != nil || status != "disarmed" {
		t.Fatalf("process = (%q,%v), want (disarmed,nil)", status, err)
	}
	if len(*calls) != 1 || (*calls)[0].portal != "lobby-main" || (*calls)[0].cred != "CARD-001" {
		t.Errorf("disarm calls = %+v, want one (lobby-main,CARD-001)", *calls)
	}
}

// A denied tap never disarms.
func TestProcessDenyNoDisarm(t *testing.T) {
	d, calls := newDisarmer(t, true, nil)
	status, err := d.process("acc.hq.door.lobby-main.evt.tap",
		[]byte(`{"cred":"CARD-001","allow":false,"reason":"deny_no_access","ts":"2026-01-05T14:00:00Z"}`))
	if err != nil || status != "skip" {
		t.Fatalf("process = (%q,%v), want (skip,nil)", status, err)
	}
	if len(*calls) != 0 {
		t.Errorf("disarm called %d times on a deny, want 0", len(*calls))
	}
}

// An operator remote grant (cmd.grant) carries no credential — it must not
// silently disarm the building.
func TestProcessCommandGrantNoDisarm(t *testing.T) {
	d, calls := newDisarmer(t, true, nil)
	status, err := d.process("acc.hq.door.lobby-main.evt.tap",
		[]byte(`{"allow":true,"reason":"allow_command_grant","ts":"2026-01-05T14:00:00Z"}`)) // no cred
	if err != nil || status != "skip" {
		t.Fatalf("process = (%q,%v), want (skip,nil)", status, err)
	}
	if len(*calls) != 0 {
		t.Errorf("disarm called %d times on a credential-less grant, want 0", len(*calls))
	}
}

// Under free_access or unlocked posture policy.Decide allows without reading the
// credential, so the cred on the event is whatever was presented: a card nobody
// issued, a revoked one, a stranger's. Only allow_grant, the reason Decide returns
// after actually checking the credential, may disarm. So may nothing the sink does
// not recognise, including a body with no reason at all.
func TestProcessPostureAllowNoDisarm(t *testing.T) {
	for _, body := range []string{
		`{"cred":"NOT-A-CARD","allow":true,"reason":"allow_posture_free_access","ts":"2026-01-05T14:00:00Z"}`,
		`{"cred":"NOT-A-CARD","allow":true,"reason":"allow_posture_unlocked","ts":"2026-01-05T14:00:00Z"}`,
		`{"cred":"NOT-A-CARD","allow":true,"ts":"2026-01-05T14:00:00Z"}`,
		`{"cred":"NOT-A-CARD","allow":true,"reason":"allow_area_grant","ts":"2026-01-05T14:00:00Z"}`,
	} {
		d, calls := newDisarmer(t, true, nil)
		status, err := d.process("acc.hq.door.lobby-main.evt.tap", []byte(body))
		if err != nil || status != "skip" {
			t.Errorf("process(%s) = (%q,%v), want (skip,nil)", body, status, err)
		}
		if len(*calls) != 0 {
			t.Errorf("process(%s): disarm called %d times, want 0", body, len(*calls))
		}
	}
}

// Non-tap events (alarm/state/fire) and malformed subjects are skipped without
// consulting the DisarmFunc.
func TestProcessSkipsNonTap(t *testing.T) {
	d, calls := newDisarmer(t, true, nil)
	for _, subj := range []string{
		"acc.hq.door.lobby-main.evt.alarm",
		"acc.hq.door.lobby-main.evt.state",
		"acc.hq.evt.fire",
		"acc.hq.evt",
	} {
		status, err := d.process(subj, []byte(`{"allow":true,"cred":"CARD-001","reason":"allow_grant"}`))
		if err != nil || status != "skip" {
			t.Errorf("process(%q) = (%q,%v), want (skip,nil)", subj, status, err)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("disarm called %d times on non-tap events, want 0", len(*calls))
	}
}

// A grant at a non-entry portal (DisarmFunc returns disarmed=false) acks as a skip.
func TestProcessNonEntryPortalSkips(t *testing.T) {
	d, _ := newDisarmer(t, false, nil) // no-op: not an entry door
	status, err := d.process("acc.hq.door.lobby-main.evt.tap",
		[]byte(`{"cred":"CARD-001","allow":true,"reason":"allow_grant","ts":"2026-01-05T14:00:00Z"}`))
	if err != nil || status != "skip" {
		t.Errorf("process = (%q,%v), want (skip,nil)", status, err)
	}
}

// A write failure surfaces as an error (→ Nak / redelivery), not a swallowed skip.
func TestProcessDisarmErrorNaks(t *testing.T) {
	d, _ := newDisarmer(t, false, errors.New("db down"))
	if _, err := d.process("acc.hq.door.lobby-main.evt.tap",
		[]byte(`{"cred":"CARD-001","allow":true,"reason":"allow_grant","ts":"2026-01-05T14:00:00Z"}`)); err == nil {
		t.Fatal("want error (Nak) on disarm failure, got nil")
	}
}
