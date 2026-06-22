package controller

import (
	"fmt"
	"sync"

	"github.com/stone-age-io/access-control/internal/drivers"
	"github.com/stone-age-io/access-control/internal/logger"
)

// multiReader runs both readers at once for the "both" controller mode: every
// portal is reachable over NATS, and the portals that have a physical reader
// (reader_address >= 0) are additionally polled over OSDP. It composes a
// *NATSReader and a *OSDPReader behind the same Reader interface, dispatching
// Arm by the portal's address and fanning both taps streams into one channel so
// the runtime stays oblivious to which transport produced a tap (drivers.Tap
// already carries the portal code and source).
type multiReader struct {
	nats Reader // arms every portal
	osdp Reader // arms only portals with a physical reader (Address >= 0)
	log  *logger.Logger
	ch   chan drivers.Tap
	wg   sync.WaitGroup
}

var _ Reader = (*multiReader)(nil)

// NewMultiReader composes the two readers and starts the fan-in. The OSDP
// reader should already be Start-ed (its bus loop is running); the NATS reader
// produces taps as subscriptions deliver. Stop tears both down. Both are taken
// as the Reader interface so the composite is unit-testable with fakes.
func NewMultiReader(nats, osdp Reader, log *logger.Logger) Reader {
	r := &multiReader{
		nats: nats,
		osdp: osdp,
		log:  log.With("component", "multi-reader"),
		ch:   make(chan drivers.Tap, 64),
	}
	r.wg.Add(2)
	go r.forward(nats.Taps())
	go r.forward(osdp.Taps())
	return r
}

// forward relays one sub-reader's taps into the merged channel until that
// sub-reader closes its channel (on Stop).
func (r *multiReader) forward(src <-chan drivers.Tap) {
	defer r.wg.Done()
	for tap := range src {
		r.ch <- tap
	}
}

// Arm arms NATS for every portal and, when the portal has a physical reader
// (Address >= 0), OSDP too. A NATS-only portal (Address < 0) never touches the
// OSDP bus, so it can't collide on a PD address or be phantom-polled. On a
// partial failure the NATS arm is rolled back so the portal stays fully
// unarmed and the reconciler retries cleanly on the next policy change.
func (r *multiReader) Arm(p Portal) error {
	if err := r.nats.Arm(p); err != nil {
		return err
	}
	if p.Address >= 0 {
		if err := r.osdp.Arm(p); err != nil {
			r.nats.Disarm(p.Code)
			return fmt.Errorf("osdp arm: %w", err)
		}
	}
	return nil
}

// Disarm removes a portal from both readers. Both are no-ops for a portal they
// don't hold, so disarming a NATS-only portal's OSDP side is harmless.
func (r *multiReader) Disarm(code string) {
	r.nats.Disarm(code)
	r.osdp.Disarm(code)
}

// Taps implements drivers.ReaderDriver: the merged stream from both readers.
func (r *multiReader) Taps() <-chan drivers.Tap { return r.ch }

// Stop tears down both sub-readers (closing their taps channels), waits for the
// fan-in goroutines to drain, then closes the merged channel.
func (r *multiReader) Stop() {
	r.nats.Stop()
	r.osdp.Stop()
	r.wg.Wait()
	close(r.ch)
}
