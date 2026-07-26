// Package badgesweep is accessd's visitor-credential hygiene sweep: it marks an
// expired visitor credential `revoked` once its validity window has passed.
//
// # This is hygiene, not security
//
// Worth being blunt about, because it is easy to over-build. Expiry is ALREADY
// enforced where it matters: policy.Decide checks the credential's
// valid_from/valid_until on every presentation, at the edge, and fails closed on a
// value it cannot parse. A visitor's pass stops opening doors the moment it expires
// whether or not this sweep ever runs — including on a controller that has lost
// contact with accessd.
//
// What the sweep buys is truth in the control plane:
//
//   - The credentials list stops showing dozens of `active` passes that in fact open
//     nothing, so "active" means what an operator thinks it means.
//   - A stale value cannot be silently resurrected by someone extending valid_until
//     on a long-forgotten visitor row; re-issuing has to go through minting.
//
// # Deliberately NOT done here
//
// The sweep does not delete visitors. How long to keep a record of who visited is a
// data-retention decision that belongs to the install (and often to its lawyers), not
// to a background job inventing a policy. Keeping the record also means a returning
// visitor is recognised and refreshed by badgeapi's mint route rather than
// duplicated. Operators can delete a visitor outright; that path is unchanged.
//
// Scope is `visitor` badges only. An expired STAFF credential is left alone on
// purpose: an operator may well be about to extend it, and silently flipping it to
// revoked would turn a date edit into a re-enrollment.
package badgesweep

import (
	"context"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stone-age-io/access-control/internal/logger"
)

// sweepInterval is how often the sweep runs. Generous on purpose: nothing depends on
// it for enforcement (see the package doc), so a slow cadence costs nothing and keeps
// the query off a busy database. A var so tests can shorten it.
var sweepInterval = time.Hour

// badgeCollection is the collection whose `visitor` records this sweep covers — the
// cardholders themselves, since a visitor IS a cardholder with kind = visitor.
// Duplicated from badgeapi rather than imported: this package has no other reason to
// depend on the HTTP layer.
const badgeCollection = "cardholders"

// Sweeper periodically revokes expired visitor credentials. It owns its own lifetime
// (like armrelease.Releaser): Start launches the loop, Stop ends it.
type Sweeper struct {
	app core.App
	log *logger.Logger

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates a Sweeper.
func New(app core.App, log *logger.Logger) *Sweeper {
	return &Sweeper{app: app, log: log.With("component", "badge-sweep")}
}

// Start launches the sweep loop on its own context (cancelled by Stop), so it lives
// for the whole serve lifetime rather than the caller's boot context.
func (s *Sweeper) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.wg.Add(1)
	go s.loop(ctx)
	s.log.Info("visitor credential sweep started", "every", sweepInterval)
}

// Stop ends the sweep loop and waits for it to exit.
func (s *Sweeper) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

func (s *Sweeper) loop(ctx context.Context) {
	defer s.wg.Done()
	t := time.NewTicker(sweepInterval)
	defer t.Stop()
	// Run once at startup: an install that restarts daily would otherwise never
	// reach the first tick.
	s.Sweep(time.Now().UTC())
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Sweep(time.Now().UTC())
		}
	}
}

// Sweep revokes every active credential belonging to a `visitor` badge holder whose
// validity window has ended. Exported so it is directly testable and so a caller can
// force one.
//
// Returns the number of credentials revoked.
func (s *Sweeper) Sweep(now time.Time) int {
	holders, err := s.visitorCardholderIDs()
	if err != nil {
		s.log.Error("visitor badge query failed; skipping sweep", "error", err)
		return 0
	}
	if len(holders) == 0 {
		return 0
	}

	revoked := 0
	for _, cardholderID := range holders {
		creds, err := s.app.FindRecordsByFilter("credentials",
			"user = {:user} && status = 'active' && valid_until != ''",
			"", 0, 0, dbx.Params{"user": cardholderID})
		if err != nil {
			s.log.Error("credential query failed", "cardholder", cardholderID, "error", err)
			continue
		}
		for _, c := range creds {
			until := c.GetDateTime("valid_until")
			// An unparseable/zero bound is left alone: policy.Decide already fails
			// closed on it, and guessing here could revoke something still wanted.
			if until.IsZero() || !now.After(until.Time()) {
				continue
			}
			c.Set("status", "revoked")
			if err := s.app.Save(c); err != nil {
				s.log.Error("failed to revoke expired visitor credential",
					"credential", c.Id, "error", err)
				continue
			}
			revoked++
			s.log.Info("revoked expired visitor credential",
				"credential", c.Id, "cardholder", cardholderID,
				"expired", until.Time().UTC().Format(time.RFC3339))
		}
	}
	return revoked
}

// visitorCardholderIDs returns the ids of every `visitor` cardholder.
//
// One query, no relation to walk: before the badge login and the person were the same
// record, this had to enumerate logins and dereference each one's `cardholder`, and a
// login whose person had gone contributed nothing but a blank id to skip.
func (s *Sweeper) visitorCardholderIDs() ([]string, error) {
	// The filter value is a constant, not user input.
	visitors, err := s.app.FindRecordsByFilter(badgeCollection, "kind = 'visitor'", "", 0, 0)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(visitors))
	for _, v := range visitors {
		out = append(out, v.Id)
	}
	return out, nil
}
