// Package osdp is a pure-Go OSDP Control Panel (CP/ACU) engine: it owns one
// RS485 bus, polls the peripheral devices (PDs — card readers) armed on it, and
// turns card presentations into CardRead events. It builds on the byte-level
// codec in ./wire and mirrors the proven CP architecture of osdp-dev/libosdp
// (a per-PD state machine — INIT → CAPDET → ONLINE → OFFLINE — round-robined
// across the bus, with libosdp's sequence/retry/offline rules), without linking
// any C.
//
// Go simplification of libosdp's design: because each bus is owned by a single
// goroutine that can block on a deadlined serial read, the engine collapses
// libosdp's tick-sliced PHY micro-FSM (IDLE/SEND/REPLY_WAIT/WAIT/RETRY) into one
// synchronous send → read-with-timeout → retry per exchange, keeping libosdp's
// state semantics and constants but not its byte-at-a-time slicing.
//
// v1 is clear-text only: a secure-channel reply parses to wire.ErrSecureUnsupported
// and is treated as a failed exchange (fail closed). Secure Channel is a planned
// fast-follow (see docs/protocol.md / the plan's Appendix B).
package osdp

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/stone-age-io/access-control/internal/drivers/osdp/wire"
	"github.com/stone-age-io/access-control/internal/logger"
)

// Tunables. These are package vars (not consts) only so tests can shrink them;
// runtime never mutates them. Defaults follow libosdp where it makes sense, with
// a shorter offline window than libosdp's 300s since a door reader should recover
// from a transient glitch quickly.
var (
	respTimeout  = 200 * time.Millisecond // wait for a reply before counting a failure
	pollInterval = 50 * time.Millisecond  // min spacing between polls to one online PD
	maxRetries   = 8                      // consecutive failed exchanges before a PD goes offline
	offlineWait  = 10 * time.Second       // how long a PD stays offline before re-INIT
	idleNap      = 5 * time.Millisecond   // loop nap when no PD is due for service
)

// Transceiver is the byte pipe to one RS485 bus. Send writes a full command
// frame (blocking until it is on the wire, so half-duplex direction can turn
// around); Receive reads whatever bytes are available, blocking up to the
// transport's own short read timeout and returning (0, nil) on timeout.
type Transceiver interface {
	Send(frame []byte) error
	Receive(buf []byte) (int, error)
	Close() error
}

// CardRead is one card presentation decoded from a PD's REPLY_RAW. Credential is
// the opaque string fed to policy (wire.CardRead.Credential); Addr ties it back
// to a PD so the controller adapter can resolve the portal.
type CardRead struct {
	Addr       byte
	Credential string
	Format     byte
	BitCount   int
	At         time.Time
}

type cpState int

const (
	stateInit    cpState = iota // send CMD_ID, establish the link (seq starts at 0)
	stateCapDet                 // send CMD_CAP
	stateOnline                 // steady-state CMD_POLL + queued commands
	stateOffline                // unresponsive; wait, then re-INIT
)

func (s cpState) String() string {
	switch s {
	case stateInit:
		return "INIT"
	case stateCapDet:
		return "CAPDET"
	case stateOnline:
		return "ONLINE"
	case stateOffline:
		return "OFFLINE"
	default:
		return "?"
	}
}

// pd is the per-address state machine. All fields are touched only by the bus
// goroutine, so they need no locking; the map that holds pds is the only shared
// structure (Arm/Disarm mutate it from the reconciler goroutine).
type pd struct {
	addr         byte
	state        cpState
	seq          int // -1 before INIT; first build sends 0, then 1→2→3→1…
	retry        int
	offlineUntil time.Time
	lastPoll     time.Time
	rx           []byte // reusable receive accumulator
}

// Bus owns one RS485 bus and the PDs armed on it.
type Bus struct {
	t     Transceiver
	log   *logger.Logger
	cards chan CardRead

	mu    sync.Mutex
	pds   map[byte]*pd
	order []byte

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewBus wires a CP engine to a transceiver. Nothing is polled until Arm adds a
// PD and Start launches the bus loop.
func NewBus(t Transceiver, log *logger.Logger) *Bus {
	return &Bus{
		t:     t,
		log:   log.With("component", "osdp-bus"),
		cards: make(chan CardRead, 64),
		pds:   make(map[byte]*pd),
	}
}

// Cards is the stream of decoded card presentations. Closed after Stop.
func (b *Bus) Cards() <-chan CardRead { return b.cards }

// Arm starts polling a PD address. Idempotent.
func (b *Bus) Arm(addr byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.pds[addr]; ok {
		return
	}
	b.pds[addr] = &pd{addr: addr, state: stateInit, seq: -1}
	b.order = append(b.order, addr)
	b.log.Info("PD armed", "addr", addr)
}

// Disarm stops polling a PD address. Unknown addresses are a no-op.
func (b *Bus) Disarm(addr byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.pds[addr]; !ok {
		return
	}
	delete(b.pds, addr)
	for i, a := range b.order {
		if a == addr {
			b.order = append(b.order[:i], b.order[i+1:]...)
			break
		}
	}
	b.log.Info("PD disarmed", "addr", addr)
}

// Start launches the bus loop until the context is cancelled or Stop is called.
func (b *Bus) Start(ctx context.Context) {
	rctx, cancel := context.WithCancel(ctx)
	b.cancel = cancel
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		b.run(rctx)
	}()
}

// Stop ends the bus loop, waits for it to exit, closes the card stream, and
// releases the transceiver.
func (b *Bus) Stop() {
	if b.cancel != nil {
		b.cancel()
	}
	b.wg.Wait()
	close(b.cards)
	_ = b.t.Close()
}

func (b *Bus) run(ctx context.Context) {
	b.log.Info("bus loop started")
	for {
		if ctx.Err() != nil {
			return
		}
		worked := b.serviceRound(ctx)
		if !worked {
			select {
			case <-ctx.Done():
				return
			case <-time.After(idleNap):
			}
		}
	}
}

// serviceRound services each armed PD at most once. Returns true if any PD was
// actually exchanged with (so the loop knows whether to nap).
func (b *Bus) serviceRound(ctx context.Context) bool {
	b.mu.Lock()
	addrs := append([]byte(nil), b.order...)
	b.mu.Unlock()

	worked := false
	for _, a := range addrs {
		if ctx.Err() != nil {
			return worked
		}
		b.mu.Lock()
		p := b.pds[a]
		b.mu.Unlock()
		if p == nil {
			continue // disarmed mid-round
		}
		if b.serviceOne(p) {
			worked = true
		}
	}
	return worked
}

// serviceOne runs one command→reply exchange with a PD if it is due, advancing
// its state machine. Returns whether an exchange was attempted.
func (b *Bus) serviceOne(p *pd) bool {
	now := time.Now()

	switch p.state {
	case stateOffline:
		if now.Before(p.offlineUntil) {
			return false // still in the offline cool-down
		}
		b.toState(p, stateInit) // cool-down elapsed; re-establish the link
	case stateOnline:
		if now.Sub(p.lastPoll) < pollInterval {
			return false // not yet time to poll this PD again
		}
	}

	cmd := commandFor(p.state)
	txSeq := nextSeq(p.seq) // recomputed each attempt; p.seq only advances on success
	frame := wire.BuildCommand(p.addr, byte(txSeq), cmd, nil)

	reply, err := b.exchange(p, frame)
	if p.state == stateOnline {
		p.lastPoll = now
	}
	if err != nil {
		b.onFailure(p, cmd, err)
		return true
	}

	// A clean exchange: commit the sequence, clear the retry counter, dispatch.
	p.seq = txSeq
	p.retry = 0
	b.handleReply(p, reply)
	return true
}

// exchange sends a command and reads until a reply addressed to this PD arrives
// or respTimeout elapses. Packets that are not this PD's replies — our own TX
// echo on a half-duplex bus, or a foreign device — are skipped, not failed.
func (b *Bus) exchange(p *pd, frame []byte) (*wire.Reply, error) {
	if err := b.t.Send(frame); err != nil {
		return nil, err
	}
	p.rx = p.rx[:0]
	tmp := make([]byte, 256)
	deadline := time.Now().Add(respTimeout)
	for time.Now().Before(deadline) {
		n, err := b.t.Receive(tmp)
		if err != nil {
			return nil, err
		}
		if n == 0 {
			continue // read slice timed out with no data; check the deadline
		}
		p.rx = append(p.rx, tmp[:n]...)
		for len(p.rx) > 0 {
			reply, consumed, perr := wire.ParseReply(p.rx)
			if consumed > 0 {
				p.rx = p.rx[consumed:]
			}
			switch {
			case errors.Is(perr, wire.ErrNeedMore):
				// keep reading from the bus
			case perr != nil:
				// hard framing error (CRC/length/secure); consumed advanced past
				// it — keep scanning what's left.
				if consumed == 0 {
					p.rx = p.rx[:0] // safety: avoid a spin on a zero-consume error
				}
				continue
			case reply.IsReply && reply.Address == p.addr:
				return reply, nil
			default:
				continue // TX echo or another device's traffic; skip it
			}
			break // ErrNeedMore: break the scan loop and read more bytes
		}
	}
	return nil, errTimeout
}

var errTimeout = errors.New("osdp: no reply before timeout")

// handleReply advances the state machine and emits card reads. It is lenient
// about the reply code in INIT/CAPDET (any valid reply moves the link forward,
// since v1 does not use PD id/capabilities), but honours a sequence-number NAK
// (re-init the link) per libosdp.
func (b *Bus) handleReply(p *pd, r *wire.Reply) {
	if r.Code == wire.ReplyNAK {
		if len(r.Data) > 0 && r.Data[0] == wire.NAKSeqNum {
			b.log.Warn("PD reported sequence-number NAK; re-initialising link", "addr", p.addr)
			b.toState(p, stateInit)
			return
		}
		b.log.Debug("PD NAK", "addr", p.addr, "state", p.state.String())
		return
	}

	switch p.state {
	case stateInit:
		b.toState(p, stateCapDet)
	case stateCapDet:
		b.toState(p, stateOnline)
	case stateOnline:
		if r.Code == wire.ReplyRAW {
			b.emitCard(p, r.Data)
		}
	}
}

func (b *Bus) emitCard(p *pd, data []byte) {
	cr, err := wire.DecodeCardRead(data)
	if err != nil {
		b.log.Warn("malformed REPLY_RAW; dropping", "addr", p.addr, "error", err)
		return
	}
	out := CardRead{
		Addr:       p.addr,
		Credential: cr.Credential(),
		Format:     cr.Format,
		BitCount:   cr.BitCount,
		At:         time.Now().UTC(),
	}
	select {
	case b.cards <- out:
		b.log.Info("card read", "addr", p.addr, "bits", cr.BitCount, "cred", out.Credential)
	default:
		b.log.Warn("card queue full; dropping card read", "addr", p.addr)
	}
}

func (b *Bus) onFailure(p *pd, cmd wire.Code, err error) {
	p.retry++
	b.log.Debug("exchange failed", "addr", p.addr, "cmd", cmd.String(),
		"state", p.state.String(), "retry", p.retry, "error", err)
	if p.retry > maxRetries {
		b.log.Warn("PD unresponsive; going offline", "addr", p.addr, "afterRetries", p.retry-1)
		b.toState(p, stateOffline)
		p.offlineUntil = time.Now().Add(offlineWait)
	}
	// Below the retry ceiling we stay in the same state and keep p.seq, so the
	// next attempt resends the same sequence number (libosdp's rollback rule).
}

// toState applies a state transition and the resets it implies. Entering INIT or
// OFFLINE resets the sequence number to -1 so the link restarts at seq 0.
func (b *Bus) toState(p *pd, next cpState) {
	if p.state == next {
		return
	}
	b.log.Info("PD state change", "addr", p.addr, "from", p.state.String(), "to", next.String())
	p.state = next
	p.retry = 0
	if next == stateInit || next == stateOffline {
		p.seq = -1
	}
}

func commandFor(s cpState) wire.Code {
	switch s {
	case stateInit:
		return wire.CmdID
	case stateCapDet:
		return wire.CmdCap
	default:
		return wire.CmdPoll
	}
}

// nextSeq advances the OSDP sequence number: -1 → 0 (the link-restart value, sent
// once after INIT), then 1 → 2 → 3 → 1… (0 is never reused mid-session).
func nextSeq(cur int) int {
	n := cur + 1
	if n > 3 {
		n = 1
	}
	return n
}
