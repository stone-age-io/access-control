package diag

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/stone-age-io/access-control/internal/logger"
)

// NewServer builds the read-only diagnostics HTTP server: /status (HTML, auto
// -refreshing), /status.json (the report as JSON), and / which redirects to
// /status. It mirrors metrics.NewServer — it owns its own mux; the caller runs
// ListenAndServe and Shutdown. Only GET is served; everything is read-only.
func NewServer(addr string, src ReportSource, log *logger.Logger) *http.Server {
	log = log.With("component", "diagnostics")
	mux := http.NewServeMux()

	mux.HandleFunc("/status.json", func(w http.ResponseWriter, _ *http.Request) {
		rep := src.Report()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			log.Error("status json encode failed", "error", err)
		}
	})

	mux.HandleFunc("/status", func(w http.ResponseWriter, _ *http.Request) {
		rep := src.Report()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := statusTemplate.Execute(w, rep); err != nil {
			log.Error("status template execute failed", "error", err)
		}
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/status", http.StatusFound)
	})

	return &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
}
