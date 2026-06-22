package controller

import (
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/policy"
	"github.com/stone-age-io/access-control/internal/statuskv"
	"github.com/stone-age-io/access-control/internal/subjects"
)

func handlerFor(t *testing.T) (*CommandHandler, *Runtime, *fakeEmitterRefs) {
	t.Helper()
	rt, reader, lock, emit := runtimeFor(t)
	h := NewCommandHandler("hq", rt, subjects.Default(), logger.NewNopLogger())
	return h, rt, &fakeEmitterRefs{reader: reader, lock: lock, emit: emit}
}

type fakeEmitterRefs struct {
	reader interface{ Close() }
	lock   interface{ Pulses() []int }
	emit   *fakeEmitter
}

func TestCommandPostureOverrideAndClear(t *testing.T) {
	h, rt, _ := handlerFor(t)

	at := ny(t, 2026, 1, 5, 9, 0)

	h.onPosture(&nats.Msg{
		Subject: "acc.hq.door.lobby-main.cmd.posture",
		Data:    []byte(`{"posture":"lockdown","actor":"guard"}`),
	})
	if got, src, _ := rt.effectivePosture("lobby-main", at); got != policy.PostureLockdown || src != statuskv.PostureSourceOverride {
		t.Errorf("posture = %q/%q, want lockdown/override", got, src)
	}

	h.onPosture(&nats.Msg{
		Subject: "acc.hq.door.lobby-main.cmd.posture",
		Data:    []byte(`{"posture":"clear"}`),
	})
	if got, src, _ := rt.effectivePosture("lobby-main", at); got != policy.PostureSecure || src != statuskv.PostureSourceStanding {
		t.Errorf("posture after clear = %q/%q, want secure/standing", got, src)
	}
}

func TestCommandPostureInvalidIgnored(t *testing.T) {
	h, rt, _ := handlerFor(t)
	h.onPosture(&nats.Msg{
		Subject: "acc.hq.door.lobby-main.cmd.posture",
		Data:    []byte(`{"posture":"bogus"}`),
	})
	if got, _, _ := rt.effectivePosture("lobby-main", ny(t, 2026, 1, 5, 9, 0)); got != policy.PostureSecure {
		t.Errorf("posture = %q, want unchanged secure", got)
	}
}

func TestCommandGrantExplicitSeconds(t *testing.T) {
	h, _, refs := handlerFor(t)
	h.onGrant(&nats.Msg{
		Subject: "acc.hq.door.lobby-main.cmd.grant",
		Data:    []byte(`{"seconds":7,"actor":"guard"}`),
	})
	if got := refs.lock.Pulses(); len(got) != 1 || got[0] != 7 {
		t.Errorf("pulses = %v, want [7]", got)
	}
}

func TestCommandGrantDefaultsToPortalPulse(t *testing.T) {
	h, _, refs := handlerFor(t)
	h.onGrant(&nats.Msg{
		Subject: "acc.hq.door.lobby-main.cmd.grant",
		Data:    []byte(`{}`),
	})
	if got := refs.lock.Pulses(); len(got) != 1 || got[0] != 5 {
		t.Errorf("pulses = %v, want [5] (portal's configured pulse)", got)
	}
}

// Fire suppresses alarms for the location while active, and resumes when cleared.
func TestFireSuppressesAlarm(t *testing.T) {
	h, rt, refs := handlerFor(t)
	at := ny(t, 2026, 1, 5, 9, 0)
	const alarmSubj = "acc.hq.door.lobby-main.evt.alarm"

	rt.EmitAlarm("lobby-main", "forced", at)
	if n := refs.emit.countSubject(alarmSubj); n != 1 {
		t.Fatalf("alarms before fire = %d, want 1", n)
	}

	h.onFire(&nats.Msg{Subject: "acc.hq.evt.fire", Data: []byte(`{"active":true}`)})
	rt.EmitAlarm("lobby-main", "forced", at)
	if n := refs.emit.countSubject(alarmSubj); n != 1 {
		t.Errorf("alarms during fire = %d, want 1 (suppressed)", n)
	}

	h.onFire(&nats.Msg{Subject: "acc.hq.evt.fire", Data: []byte(`{"active":false}`)})
	rt.EmitAlarm("lobby-main", "forced", at)
	if n := refs.emit.countSubject(alarmSubj); n != 2 {
		t.Errorf("alarms after fire clears = %d, want 2 (resumed)", n)
	}
}
