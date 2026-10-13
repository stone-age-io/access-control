package diag

import (
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/micro"
	"github.com/stone-age-io/access-control/internal/logger"
)

// fakeRequest records what a handler replied with. Only RespondJSON is used.
type fakeRequest struct {
	micro.Request
	reply []byte
}

func (f *fakeRequest) RespondJSON(v any, _ ...micro.RespondOpt) error {
	b, err := json.Marshal(v)
	f.reply = b
	return err
}

// The health reply is the status page's report without its decisions, so no
// presented credential value can leave the box through it.
func TestHealthReplyHasNoDecisions(t *testing.T) {
	req := &fakeRequest{}
	healthHandler(fakeSource{sampleReport()}, logger.NewNopLogger())(req)

	if strings.Contains(string(req.reply), "CARD-001") {
		t.Errorf("health reply carries a credential value: %s", req.reply)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(req.reply, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := got["decisions"]; ok {
		t.Errorf("health reply has a decisions field: %s", got["decisions"])
	}
	for _, key := range []string{"identity", "nats", "policy", "portals", "alarms"} {
		if _, ok := got[key]; !ok {
			t.Errorf("health reply is missing %q", key)
		}
	}
}

// The status page keeps its decisions: Health drops them from a copy.
func TestHealthLeavesTheReportAlone(t *testing.T) {
	r := sampleReport()
	_ = Health(r)
	if len(r.Decisions) != 1 {
		t.Errorf("Health changed the caller's report: %d decisions, want 1", len(r.Decisions))
	}
}

// noServer refuses every dial, so the connection never reaches a server.
type noServer struct{}

func (noServer) Dial(_, _ string) (net.Conn, error) { return nil, errors.New("no server") }

// The endpoint registers on a cold boot, before NATS is reachable, on the
// subject it is given and with no queue group.
func TestRegisterHealth(t *testing.T) {
	nc, err := nats.Connect("nats://127.0.0.1:4222", nats.SetCustomDialer(noServer{}), nats.RetryOnFailedConnect(true))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer nc.Close()

	svc, err := micro.AddService(nc, micro.Config{Name: "access-controller", Version: "0.0.0-dev"})
	if err != nil {
		t.Fatalf("add service: %v", err)
	}
	defer func() { _ = svc.Stop() }()

	const subject = "acc.hq.ctrl.ctrl-hq-1.health"
	if err := RegisterHealth(svc, subject, fakeSource{sampleReport()}, logger.NewNopLogger()); err != nil {
		t.Fatalf("register: %v", err)
	}

	eps := svc.Info().Endpoints
	if len(eps) != 1 {
		t.Fatalf("endpoints = %v, want one", eps)
	}
	if eps[0].Name != HealthEndpoint || eps[0].Subject != subject || eps[0].QueueGroup != "" {
		t.Errorf("endpoint = %+v, want name %q, subject %q and no queue group", eps[0], HealthEndpoint, subject)
	}
}
