package natsx

import (
	"errors"
	"net"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/stone-age-io/access-control/internal/logger"
)

// noServer refuses every dial, so the connection never reaches a server.
type noServer struct{}

func (noServer) Dial(_, _ string) (net.Conn, error) { return nil, errors.New("no server") }

func retryingConn(t *testing.T) *Conn {
	t.Helper()
	nc, err := nats.Connect("nats://127.0.0.1:4222", nats.SetCustomDialer(noServer{}), nats.RetryOnFailedConnect(true))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(nc.Close)
	if nc.IsConnected() {
		t.Fatal("expected a connection that is still retrying")
	}
	return &Conn{NC: nc, log: logger.NewNopLogger()}
}

// The controller's cold boot: NATS has never been reachable and the client is
// retrying in the background. Registration must still succeed, without blocking,
// and carry identity only: the Thing code, the location, the version, and no
// endpoints.
func TestRegisterServiceBeforeNATSIsReachable(t *testing.T) {
	c := retryingConn(t)
	svc := c.RegisterService(ServiceController, "test", "ctrl-1", map[string]string{"location": "hq"})
	if svc == nil {
		t.Fatal("RegisterService returned nil while NATS is unreachable")
	}

	info := svc.Info()
	if info.Name != ServiceController {
		t.Errorf("name = %q, want %q", info.Name, ServiceController)
	}
	if info.Metadata["code"] != "ctrl-1" || info.Metadata["location"] != "hq" || info.Metadata["version"] == "" || len(info.Metadata) != 3 {
		t.Errorf("metadata = %v, want exactly code=ctrl-1, location=hq and a version", info.Metadata)
	}
	if len(info.Endpoints) != 0 {
		t.Errorf("endpoints = %v, want none", info.Endpoints)
	}
	if err := svc.Stop(); err != nil {
		t.Errorf("stop: %v", err)
	}
}

// Without a Thing code there is nothing for the console to join a registration
// to, so it is skipped (ADR 0008 rule 4), and never fatal.
func TestRegisterServiceWithoutCodeIsSkipped(t *testing.T) {
	if svc := retryingConn(t).RegisterService(ServiceAccessd, "test", "", nil); svc != nil {
		t.Errorf("registered without a Thing code: %+v", svc.Info())
	}
}
