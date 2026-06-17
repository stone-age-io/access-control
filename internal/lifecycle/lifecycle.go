// file: internal/lifecycle/lifecycle.go

package lifecycle

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/stone-age-io/access-control/internal/logger"
)

// RunWithReload runs an application with automatic reload support on SIGHUP.
// It handles the complete lifecycle:
//   - Initial startup
//   - Signal handling (SIGTERM, SIGINT, SIGHUP)
//   - Graceful shutdown on SIGTERM/SIGINT
//   - Reload on SIGHUP (createApp is called again for a fresh instance)
//   - Error propagation
//
// If createApp returns an error, the process exits.
func RunWithReload(
	createApp func() (Application, error),
	log *logger.Logger,
) error {
	log = log.With("component", "lifecycle")
	reloadCount := 0

	for {
		if reloadCount > 0 {
			log.Info("initiating application reload", "reloadCount", reloadCount)
		}

		shutdownSig := make(chan os.Signal, 1)
		reloadSig := make(chan os.Signal, 1)

		signal.Notify(shutdownSig, os.Interrupt, syscall.SIGTERM)
		signal.Notify(reloadSig, syscall.SIGHUP)

		startTime := time.Now()
		application, err := createApp()
		if err != nil {
			signal.Stop(shutdownSig)
			signal.Stop(reloadSig)
			close(shutdownSig)
			close(reloadSig)

			if reloadCount > 0 {
				log.Error("FATAL: failed to reload application",
					"reloadCount", reloadCount, "error", err)
				log.Info("process will exit - fix the error and restart")
			}
			return fmt.Errorf("failed to create application: %w", err)
		}

		if reloadCount > 0 {
			log.Info("application reload completed successfully",
				"reloadCount", reloadCount, "duration", time.Since(startTime))
		}

		ctx, cancel := context.WithCancel(context.Background())
		errCh := make(chan error, 1)
		go func() {
			errCh <- application.Run(ctx)
		}()

		var shouldReload bool
		var runErr error

		select {
		case sig := <-shutdownSig:
			log.Info("shutdown signal received - initiating graceful shutdown", "signal", sig)
			shouldReload = false

		case <-reloadSig:
			log.Info("SIGHUP received - initiating reload")
			shouldReload = true
			reloadCount++

		case runErr = <-errCh:
			log.Error("application stopped with error", "error", runErr, "reloadCount", reloadCount)
			shouldReload = false
		}

		cancel()

		signal.Stop(shutdownSig)
		signal.Stop(reloadSig)
		close(shutdownSig)
		close(reloadSig)

		log.Info("closing application")
		closeStart := time.Now()
		if closeErr := application.Close(); closeErr != nil {
			log.Error("error during application close",
				"error", closeErr, "duration", time.Since(closeStart))
		} else {
			log.Info("application closed successfully", "duration", time.Since(closeStart))
		}

		if !shouldReload {
			log.Info("shutdown complete")
			return runErr
		}
		log.Info("reloading and re-establishing connections")
	}
}
