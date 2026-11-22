package natsx

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stone-age-io/kit/credskeeper"

	"github.com/stone-age-io/access-control/config"
)

// StartKeeper starts the kit's credskeeper for cfg.Platform (ADR 0009): it
// keeps cfg.NATS.CredsFile current from the platform until ctx ends. The caller
// waits with WaitForCreds before Connect, since nats.go reads the creds file
// before it dials, and decides how long a first start may wait.
//
// It makes the files' directory first, 0700, as the process that owns them:
// the keeper writes only into a directory that exists.
func StartKeeper(ctx context.Context, cfg *config.Config, log *slog.Logger, reg prometheus.Registerer) (*credskeeper.Keeper, error) {
	for _, f := range []string{cfg.NATS.CredsFile, cfg.Platform.SessionFile} {
		if f == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(f), 0o700); err != nil {
			return nil, fmt.Errorf("platform: %w", err)
		}
	}
	k, err := credskeeper.New(credskeeper.Config{
		URL:           cfg.Platform.URL,
		Identity:      cfg.Platform.Identity,
		Password:      cfg.Platform.Password,
		CredsFile:     cfg.NATS.CredsFile,
		SessionFile:   cfg.Platform.SessionFile,
		SyncInterval:  cfg.Platform.SyncInterval,
		AllowInsecure: cfg.Platform.AllowInsecure,
	}, log.With("component", "credskeeper"), credskeeper.NewMetrics(reg))
	if err != nil {
		return nil, fmt.Errorf("platform: %w", err)
	}
	go k.Run(ctx)
	return k, nil
}
