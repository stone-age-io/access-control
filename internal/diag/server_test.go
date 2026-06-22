package diag

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stone-age-io/access-control/internal/controller"
	"github.com/stone-age-io/access-control/internal/logger"
)

type fakeSource struct{ r Report }

func (f fakeSource) Report() Report { return f.r }

func sampleReport() Report {
	return Report{
		GeneratedAt: time.Unix(0, 0).UTC(),
		Identity:    Identity{Controller: "ctrl-hq-1", Location: "hq", SubjectsApp: "acc", Driver: "mock", Reader: "nats", Uptime: "1m0s"},
		Build:       Build{GoVersion: "go1.26"},
		NATS:        NATSStatus{Connected: true, URL: "nats://localhost:4222"},
		Policy:      PolicyStatus{Synced: true, Counts: map[string]int{"portals": 1}},
		Portals: []PortalView{{
			Code: "lobby-main", Type: "door", Location: "hq", Armed: true,
			Posture: "secure", Source: "standing", Door: "closed",
		}},
		Decisions: []controller.DecisionRecord{{
			At: time.Unix(0, 0).UTC(), Portal: "lobby-main", Cred: "CARD-001",
			User: "alice", Allow: true, Reason: "allow_grant",
		}},
	}
}

func serve(t *testing.T, src ReportSource, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	srv := NewServer(":0", src, logger.NewNopLogger())
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestStatusJSON(t *testing.T) {
	rec := serve(t, fakeSource{sampleReport()}, http.MethodGet, "/status.json")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content-type = %q, want application/json", ct)
	}
	var got Report
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Identity.Controller != "ctrl-hq-1" || len(got.Portals) != 1 || len(got.Decisions) != 1 {
		t.Errorf("decoded report = %+v, want ctrl-hq-1 with 1 portal and 1 decision", got)
	}
}

func TestStatusHTML(t *testing.T) {
	rec := serve(t, fakeSource{sampleReport()}, http.MethodGet, "/status")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	for _, want := range []string{"ctrl-hq-1", "lobby-main", "CARD-001", "allow_grant", "ALLOW"} {
		if !strings.Contains(body, want) {
			t.Errorf("HTML missing %q", want)
		}
	}
}

func TestStatusHTMLEmptyPortalsBanner(t *testing.T) {
	r := sampleReport()
	r.Portals = nil
	rec := serve(t, fakeSource{r}, http.MethodGet, "/status")
	if !strings.Contains(rec.Body.String(), "No portals bound") {
		t.Errorf("expected the empty-portals banner; body did not contain it")
	}
}

func TestRootRedirect(t *testing.T) {
	rec := serve(t, fakeSource{sampleReport()}, http.MethodGet, "/")
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/status" {
		t.Errorf("redirect location = %q, want /status", loc)
	}
}

func TestUnknownPathNotFound(t *testing.T) {
	rec := serve(t, fakeSource{sampleReport()}, http.MethodGet, "/nope")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
