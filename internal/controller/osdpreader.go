package controller

import (
	"context"
	"fmt"
	"sync"

	"github.com/stone-age-io/access-control/internal/drivers"
	"github.com/stone-age-io/access-control/internal/drivers/osdp"
	"github.com/stone-age-io/access-control/internal/drivers/osdp/wire"
	"github.com/stone-age-io/access-control/internal/logger"
	"github.com/stone-age-io/access-control/internal/metrics"
)

// Reader is the controller's pluggable reader surface: it streams taps and is
// armed/disarmed per portal by the PortalManager. *NATSReader (the v1 simulated
// reader) and *OSDPReader (real RS485) both implement it; main selects one by
// config (controller.reader).
type Reader interface {
	drivers.ReaderDriver
	Arm(p Portal) error
	Disarm(code string)
	Stop()
}

// osdpBus is the slice of *osdp.Bus the reader adapter drives. It is an interface
// only so the adapter can be unit-tested against a fake bus that emits canned
// card reads, without a serial port.
type osdpBus interface {
	Cards() <-chan osdp.CardRead
	Arm(addr byte)
	Disarm(addr byte)
	Start(ctx context.Context)
	Stop()
}

// OSDPReader is the real reader: it owns one RS485 bus (an osdp.Bus) and turns the
// engine's CardRead events into drivers.Tap. Unlike the NATSReader, all armed
// portals share a single bus — Arm/Disarm register a portal's OSDP PD address into
// the engine's polled set and maintain the address↔portal-code map used to resolve
// a card read back to a portal. It implements drivers.ReaderDriver and the
// reconciler's portalArmer, so it is a drop-in replacement for NATSReader.
type OSDPReader struct {
	bus osdpBus
	log *logger.Logger
	m   *metrics.Metrics
	ch  chan drivers.Tap

	mu     sync.Mutex
	byAddr map[byte]string // PD address -> portal code
	byCode map[string]byte // portal code -> PD address
	closed bool

	wg sync.WaitGroup
}

// Compile-time guarantees: both readers are interchangeable, and the real bus
// satisfies the adapter's bus interface.
var (
	_ Reader  = (*NATSReader)(nil)
	_ Reader  = (*OSDPReader)(nil)
	_ osdpBus = (*osdp.Bus)(nil)
)

// NewOSDPReader builds the adapter over an OSDP bus. Call Start to begin polling.
// The metrics argument may be nil.
func NewOSDPReader(bus osdpBus, log *logger.Logger, m *metrics.Metrics) *OSDPReader {
	return &OSDPReader{
		bus:    bus,
		log:    log.With("component", "osdp-reader"),
		m:      m,
		ch:     make(chan drivers.Tap, 64),
		byAddr: make(map[byte]string),
		byCode: make(map[string]byte),
	}
}

// Start launches the bus loop and the goroutine that translates card reads into
// taps. The context bounds the bus loop; Stop also tears it down.
func (r *OSDPReader) Start(ctx context.Context) {
	r.bus.Start(ctx)
	r.wg.Add(1)
	go r.translate()
}

func (r *OSDPReader) translate() {
	defer r.wg.Done()
	for card := range r.bus.Cards() {
		r.mu.Lock()
		code, ok := r.byAddr[card.Addr]
		r.mu.Unlock()
		if !ok {
			// A card from an address we don't drive (stale arming, or a reader
			// at an unconfigured address). Fail safe: drop it.
			r.log.Warn("card read from unmapped PD address; dropping", "addr", card.Addr)
			continue
		}
		tap := drivers.Tap{Portal: code, Credential: card.Credential, At: card.At}
		// Non-blocking, like NATSReader: a full queue means we're saturated; drop
		// and count rather than wedge the bus goroutine (a dropped tap denies).
		select {
		case r.ch <- tap:
		default:
			r.m.IncTapDropped()
			r.log.Warn("tap queue full; dropping tap", "portal", code)
		}
	}
}

// Arm registers a portal's reader (its OSDP PD address) on the bus. It is
// idempotent for an unchanged address, re-points cleanly if the address changed,
// and rejects a second portal claiming an address already in use on this bus.
func (r *OSDPReader) Arm(p Portal) error {
	if p.Address < 0 || p.Address > int(wire.MaxAddr) {
		return fmt.Errorf("invalid OSDP address %d for portal %q (want 0..%d)", p.Address, p.Code, wire.MaxAddr)
	}
	addr := byte(p.Address)

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return fmt.Errorf("reader closed")
	}
	if cur, ok := r.byCode[p.Code]; ok {
		if cur == addr {
			r.mu.Unlock()
			return nil // already armed at this address
		}
		delete(r.byAddr, cur) // address changed: drop the old mapping
		r.bus.Disarm(cur)
	}
	if other, ok := r.byAddr[addr]; ok && other != p.Code {
		r.mu.Unlock()
		return fmt.Errorf("OSDP address %d already armed for portal %q", addr, other)
	}
	r.byCode[p.Code] = addr
	r.byAddr[addr] = p.Code
	r.mu.Unlock()

	r.bus.Arm(addr)
	r.log.Info("reader armed", "portal", p.Code, "addr", addr)
	return nil
}

// Disarm removes a portal's reader from the bus. Unknown portals are a no-op.
func (r *OSDPReader) Disarm(code string) {
	r.mu.Lock()
	addr, ok := r.byCode[code]
	if ok {
		delete(r.byCode, code)
		delete(r.byAddr, addr)
	}
	r.mu.Unlock()
	if ok {
		r.bus.Disarm(addr)
		r.log.Info("reader disarmed", "portal", code, "addr", addr)
	}
}

// Taps implements drivers.ReaderDriver.
func (r *OSDPReader) Taps() <-chan drivers.Tap { return r.ch }

// Stop tears down the bus, drains the translate goroutine, and closes the taps
// channel.
func (r *OSDPReader) Stop() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	r.mu.Unlock()

	r.bus.Stop() // closes the bus card stream, unblocking translate()
	r.wg.Wait()  // translate() returns once the card stream is closed
	close(r.ch)
}
