// file: internal/metrics/server.go

package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// NewServer builds an HTTP server exposing the registry at the given path.
// The caller owns ListenAndServe / Shutdown.
func (m *Metrics) NewServer(addr, path string) *http.Server {
	mux := http.NewServeMux()
	mux.Handle(path, promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{}))
	return &http.Server{Addr: addr, Handler: mux}
}
