package controller

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stone-age-io/access-control/internal/drivers/osdp"
	"github.com/stone-age-io/access-control/internal/logger"
)

// fakeOSDPBus is an osdpBus that records arming and lets a test push card reads.
type fakeOSDPBus struct {
	cards chan osdp.CardRead
	mu    sync.Mutex
	armed map[byte]bool
}

func newFakeOSDPBus() *fakeOSDPBus {
	return &fakeOSDPBus{cards: make(chan osdp.CardRead, 16), armed: map[byte]bool{}}
}

func (f *fakeOSDPBus) Cards() <-chan osdp.CardRead { return f.cards }
func (f *fakeOSDPBus) Start(context.Context)       {}
func (f *fakeOSDPBus) Stop()                       { close(f.cards) }

func (f *fakeOSDPBus) Arm(addr byte) {
	f.mu.Lock()
	f.armed[addr] = true
	f.mu.Unlock()
}

func (f *fakeOSDPBus) Disarm(addr byte) {
	f.mu.Lock()
	delete(f.armed, addr)
	f.mu.Unlock()
}

func (f *fakeOSDPBus) isArmed(addr byte) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.armed[addr]
}

func (f *fakeOSDPBus) emit(c osdp.CardRead) { f.cards <- c }

func newTestOSDPReader(t *testing.T) (*OSDPReader, *fakeOSDPBus) {
	t.Helper()
	bus := newFakeOSDPBus()
	r := NewOSDPReader(bus, logger.NewBootstrapLogger(), nil)
	r.Start(context.Background())
	t.Cleanup(r.Stop)
	return r, bus
}

func expectTap(t *testing.T, r *OSDPReader, wantPortal, wantCred string) {
	t.Helper()
	select {
	case tap := <-r.Taps():
		if tap.Portal != wantPortal || tap.Credential != wantCred {
			t.Fatalf("tap = {portal:%q cred:%q}, want {%q %q}", tap.Portal, tap.Credential, wantPortal, wantCred)
		}
	case <-time.After(time.Second):
		t.Fatal("no tap emitted")
	}
}

func expectNoTap(t *testing.T, r *OSDPReader) {
	t.Helper()
	select {
	case tap := <-r.Taps():
		t.Fatalf("unexpected tap %+v", tap)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestOSDPReaderMapsCardToTap(t *testing.T) {
	r, bus := newTestOSDPReader(t)
	if err := r.Arm(Portal{Code: "lobby", Address: 7}); err != nil {
		t.Fatalf("Arm: %v", err)
	}
	if !bus.isArmed(7) {
		t.Fatal("bus not armed for address 7")
	}
	bus.emit(osdp.CardRead{Addr: 7, Credential: "deadbe01", At: time.Now()})
	expectTap(t, r, "lobby", "deadbe01")
}

func TestOSDPReaderDropsUnmappedAddress(t *testing.T) {
	r, bus := newTestOSDPReader(t)
	_ = r.Arm(Portal{Code: "lobby", Address: 7})
	bus.emit(osdp.CardRead{Addr: 9, Credential: "ffff", At: time.Now()}) // no portal at 9
	bus.emit(osdp.CardRead{Addr: 7, Credential: "aa55", At: time.Now()})
	// Only the mapped read should surface, in order.
	expectTap(t, r, "lobby", "aa55")
}

func TestOSDPReaderAddressConflict(t *testing.T) {
	r, _ := newTestOSDPReader(t)
	if err := r.Arm(Portal{Code: "a", Address: 5}); err != nil {
		t.Fatalf("Arm a: %v", err)
	}
	if err := r.Arm(Portal{Code: "b", Address: 5}); err == nil {
		t.Fatal("arming a second portal at address 5 should fail")
	}
}

func TestOSDPReaderArmRejectsBadAddress(t *testing.T) {
	r, _ := newTestOSDPReader(t)
	if err := r.Arm(Portal{Code: "a", Address: 200}); err == nil {
		t.Fatal("arming at out-of-range address 200 should fail")
	}
}

func TestOSDPReaderDisarm(t *testing.T) {
	r, bus := newTestOSDPReader(t)
	_ = r.Arm(Portal{Code: "lobby", Address: 5})
	r.Disarm("lobby")
	if bus.isArmed(5) {
		t.Fatal("bus still armed for 5 after disarm")
	}
	bus.emit(osdp.CardRead{Addr: 5, Credential: "dead", At: time.Now()})
	expectNoTap(t, r) // mapping gone; the read is dropped
}

func TestOSDPReaderReArmNewAddress(t *testing.T) {
	r, bus := newTestOSDPReader(t)
	_ = r.Arm(Portal{Code: "lobby", Address: 5})
	if err := r.Arm(Portal{Code: "lobby", Address: 6}); err != nil {
		t.Fatalf("re-arm: %v", err)
	}
	if bus.isArmed(5) || !bus.isArmed(6) {
		t.Fatalf("after re-arm: armed5=%v armed6=%v, want false/true", bus.isArmed(5), bus.isArmed(6))
	}
	bus.emit(osdp.CardRead{Addr: 6, Credential: "cafe", At: time.Now()})
	expectTap(t, r, "lobby", "cafe")
}
