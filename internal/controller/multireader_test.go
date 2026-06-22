package controller

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stone-age-io/access-control/internal/drivers"
	"github.com/stone-age-io/access-control/internal/logger"
)

// fakeReader is a Reader that records arming and lets a test push taps onto its
// stream — enough to exercise the multiReader's dispatch and fan-in without a
// NATS connection or a serial bus.
type fakeReader struct {
	mu       sync.Mutex
	armed    map[string]int // code -> address last armed with
	disarmed []string
	armErr   error // when set, Arm fails (to exercise rollback)
	ch       chan drivers.Tap
}

func newFakeReader() *fakeReader {
	return &fakeReader{armed: map[string]int{}, ch: make(chan drivers.Tap, 16)}
}

func (f *fakeReader) Taps() <-chan drivers.Tap { return f.ch }

func (f *fakeReader) Arm(p Portal) error {
	if f.armErr != nil {
		return f.armErr
	}
	f.mu.Lock()
	f.armed[p.Code] = p.Address
	f.mu.Unlock()
	return nil
}

func (f *fakeReader) Disarm(code string) {
	f.mu.Lock()
	delete(f.armed, code)
	f.disarmed = append(f.disarmed, code)
	f.mu.Unlock()
}

func (f *fakeReader) Stop() { close(f.ch) }

func (f *fakeReader) isArmed(code string) (int, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	addr, ok := f.armed[code]
	return addr, ok
}

func newTestMultiReader(t *testing.T) (Reader, *fakeReader, *fakeReader) {
	t.Helper()
	nats, osdp := newFakeReader(), newFakeReader()
	mr := NewMultiReader(nats, osdp, logger.NewBootstrapLogger())
	t.Cleanup(mr.Stop)
	return mr, nats, osdp
}

// A NATS-only portal (Address < 0) arms NATS only; a portal with a reader
// (Address >= 0) arms both.
func TestMultiReaderArmDispatch(t *testing.T) {
	mr, nats, osdp := newTestMultiReader(t)

	if err := mr.Arm(Portal{Code: "n", Type: "door", Address: -1}); err != nil {
		t.Fatalf("arm nats-only: %v", err)
	}
	if _, ok := nats.isArmed("n"); !ok {
		t.Fatal("nats-only portal not armed on NATS")
	}
	if _, ok := osdp.isArmed("n"); ok {
		t.Fatal("nats-only portal must not arm on OSDP")
	}

	if err := mr.Arm(Portal{Code: "o", Type: "door", Address: 5}); err != nil {
		t.Fatalf("arm osdp: %v", err)
	}
	if _, ok := nats.isArmed("o"); !ok {
		t.Fatal("osdp portal not armed on NATS (every portal is NATS-reachable)")
	}
	if addr, ok := osdp.isArmed("o"); !ok || addr != 5 {
		t.Fatalf("osdp portal armed at %d,%v, want 5,true", addr, ok)
	}
}

// Taps from either sub-reader surface on the one merged stream, source intact.
func TestMultiReaderFanIn(t *testing.T) {
	mr, nats, osdp := newTestMultiReader(t)

	nats.ch <- drivers.Tap{Portal: "n", Credential: "AAA", Source: drivers.SourceNATS}
	expectMergedTap(t, mr, "n", "AAA", drivers.SourceNATS)

	osdp.ch <- drivers.Tap{Portal: "o", Credential: "BBB", Source: drivers.SourceOSDP}
	expectMergedTap(t, mr, "o", "BBB", drivers.SourceOSDP)
}

// A failed OSDP arm rolls back the NATS arm so the portal stays fully unarmed.
func TestMultiReaderArmRollback(t *testing.T) {
	mr, nats, osdp := newTestMultiReader(t)
	osdp.armErr = errors.New("bus busy")

	if err := mr.Arm(Portal{Code: "x", Type: "door", Address: 5}); err == nil {
		t.Fatal("arm should fail when the OSDP side fails")
	}
	if _, ok := nats.isArmed("x"); ok {
		t.Fatal("NATS arm not rolled back after OSDP failure")
	}
}

// Disarm removes the portal from both readers.
func TestMultiReaderDisarmBoth(t *testing.T) {
	mr, nats, osdp := newTestMultiReader(t)
	_ = mr.Arm(Portal{Code: "o", Type: "door", Address: 5})

	mr.Disarm("o")
	if _, ok := nats.isArmed("o"); ok {
		t.Fatal("still armed on NATS after disarm")
	}
	if _, ok := osdp.isArmed("o"); ok {
		t.Fatal("still armed on OSDP after disarm")
	}
}

func expectMergedTap(t *testing.T, r Reader, wantPortal, wantCred, wantSource string) {
	t.Helper()
	select {
	case tap := <-r.Taps():
		if tap.Portal != wantPortal || tap.Credential != wantCred || tap.Source != wantSource {
			t.Fatalf("tap = %+v, want {portal:%q cred:%q source:%q}", tap, wantPortal, wantCred, wantSource)
		}
	case <-time.After(time.Second):
		t.Fatal("no tap surfaced on the merged stream")
	}
}
