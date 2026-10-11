package natsx

import (
	"errors"
	"net"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/stone-age-io/access-control/internal/logger"
)

func TestServiceVersion(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"1.2.3", "1.2.3"},
		{"v1.2.3", "1.2.3"},
		{"dev", "0.0.0-dev"},
		{"1.2", "0.0.0-dev"},
		{"1.2.3-rc.1", "1.2.3-rc.1"},
		// What Go stamps: a pseudo-version from an untagged checkout, and "(devel)"
		// (or nothing) when it has no VCS info.
		{"v0.0.0-20260819230703-003bc8d88324+dirty", "0.0.0-20260819230703-003bc8d88324+dirty"},
		{"(devel)", "0.0.0-dev"},
		{"", "0.0.0-dev"},
	}
	for _, tc := range cases {
		if got := serviceVersion(tc.in); got != tc.want {
			t.Errorf("serviceVersion(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// noServer refuses every dial, so the connection never reaches a server.
type noServer struct{}

func (noServer) Dial(_, _ string) (net.Conn, error) { return nil, errors.New("no server") }

// The controller's cold boot: NATS has never been reachable and the client is
// retrying in the background. Registration must still succeed, without blocking,
// and carry identity only: the controller's code/location and no endpoints.
func TestRegisterServiceBeforeNATSIsReachable(t *testing.T) {
	nc, err := nats.Connect("nats://127.0.0.1:4222", nats.SetCustomDialer(noServer{}), nats.RetryOnFailedConnect(true))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer nc.Close()
	if nc.IsConnected() {
		t.Fatal("expected a connection that is still retrying")
	}

	c := &Conn{NC: nc, log: logger.NewNopLogger()}
	svc := c.RegisterService(ServiceController, "test", map[string]string{"code": "ctrl-1", "location": "hq"})
	if svc == nil {
		t.Fatal("RegisterService returned nil while NATS is unreachable")
	}

	info := svc.Info()
	if info.Name != ServiceController {
		t.Errorf("name = %q, want %q", info.Name, ServiceController)
	}
	if info.Metadata["code"] != "ctrl-1" || info.Metadata["location"] != "hq" || len(info.Metadata) != 2 {
		t.Errorf("metadata = %v, want exactly code=ctrl-1 location=hq", info.Metadata)
	}
	if len(info.Endpoints) != 0 {
		t.Errorf("endpoints = %v, want none", info.Endpoints)
	}
	if err := svc.Stop(); err != nil {
		t.Errorf("stop: %v", err)
	}
}
