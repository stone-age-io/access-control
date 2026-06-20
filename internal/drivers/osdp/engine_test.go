package osdp

import (
	"bytes"
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stone-age-io/access-control/internal/drivers/osdp/wire"
	"github.com/stone-age-io/access-control/internal/logger"
)

// TestMain shrinks the engine's timing tunables so the state-machine tests run in
// milliseconds rather than seconds. Tests in this package never run in parallel,
// so mutating these package vars here is safe.
func TestMain(m *testing.M) {
	respTimeout = 60 * time.Millisecond
	pollInterval = 2 * time.Millisecond
	maxRetries = 2
	offlineWait = 40 * time.Millisecond
	idleNap = time.Millisecond
	os.Exit(m.Run())
}

type recvCmd struct {
	code wire.Code
	seq  byte
}

// fakePD is an in-memory Transceiver that simulates one OSDP reader: it answers
// CMD_ID/CMD_CAP, ACKs polls, and returns a REPLY_RAW once a card is staged. It
// records every command it receives so tests can assert engine behaviour without
// reaching into the (goroutine-confined) PD state.
type fakePD struct {
	addr byte

	mu       sync.Mutex
	pending  []byte
	cardData []byte
	muted    bool
	recv     []recvCmd
}

func (f *fakePD) Send(frame []byte) error {
	r, _, err := wire.ParseReply(frame)
	if err != nil || r.IsReply {
		return nil
	}
	if r.Address != f.addr && r.Address != wire.AddrBroadcast {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recv = append(f.recv, recvCmd{r.Code, r.Seq})
	if f.muted {
		return nil
	}
	switch r.Code {
	case wire.CmdID:
		f.pending = wire.BuildReply(f.addr, r.Seq, wire.ReplyPDID, make([]byte, 12))
	case wire.CmdCap:
		f.pending = wire.BuildReply(f.addr, r.Seq, wire.ReplyPDCAP, nil)
	case wire.CmdPoll:
		if f.cardData != nil {
			f.pending = wire.BuildReply(f.addr, r.Seq, wire.ReplyRAW, f.cardData)
			f.cardData = nil
		} else {
			f.pending = wire.BuildReply(f.addr, r.Seq, wire.ReplyACK, nil)
		}
	default:
		f.pending = wire.BuildReply(f.addr, r.Seq, wire.ReplyACK, nil)
	}
	return nil
}

func (f *fakePD) Receive(buf []byte) (int, error) {
	f.mu.Lock()
	if len(f.pending) == 0 {
		f.mu.Unlock()
		time.Sleep(time.Millisecond) // mimic the termios VTIME read slice
		return 0, nil
	}
	n := copy(buf, f.pending)
	f.pending = f.pending[n:]
	f.mu.Unlock()
	return n, nil
}

func (f *fakePD) Close() error { return nil }

func (f *fakePD) stageCard(data []byte) {
	f.mu.Lock()
	f.cardData = data
	f.mu.Unlock()
}

func (f *fakePD) setMuted(m bool) {
	f.mu.Lock()
	f.muted = m
	if m {
		f.pending = nil
	}
	f.mu.Unlock()
}

func (f *fakePD) countCode(c wire.Code) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, rc := range f.recv {
		if rc.code == c {
			n++
		}
	}
	return n
}

func (f *fakePD) sawCode(c wire.Code) bool { return f.countCode(c) > 0 }

func (f *fakePD) seqList() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]byte, len(f.recv))
	for i, rc := range f.recv {
		out[i] = rc.seq
	}
	return out
}

func waitFor(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", d)
}

func newTestBus(t *testing.T, f *fakePD) *Bus {
	t.Helper()
	b := NewBus(f, logger.NewBootstrapLogger())
	b.Arm(f.addr)
	b.Start(context.Background())
	t.Cleanup(b.Stop)
	return b
}

func TestBusReachesOnlineAndPolls(t *testing.T) {
	f := &fakePD{addr: 5}
	newTestBus(t, f)
	// Reaching ONLINE is the only way a CMD_POLL is ever sent.
	waitFor(t, 2*time.Second, func() bool { return f.sawCode(wire.CmdPoll) })
}

func TestBusEmitsCardRead(t *testing.T) {
	f := &fakePD{addr: 5}
	b := newTestBus(t, f)
	waitFor(t, 2*time.Second, func() bool { return f.sawCode(wire.CmdPoll) })

	// reader 0, Wiegand, 26 bits, 4 packed bytes.
	f.stageCard([]byte{0x00, wire.CardFmtRawWiegand, 26, 0x00, 0xDE, 0xAD, 0xBE, 0x01})

	select {
	case cr := <-b.Cards():
		if cr.Addr != 5 {
			t.Errorf("addr = %d, want 5", cr.Addr)
		}
		if cr.Credential != "deadbe01" {
			t.Errorf("credential = %q, want %q", cr.Credential, "deadbe01")
		}
		if cr.BitCount != 26 {
			t.Errorf("bitCount = %d, want 26", cr.BitCount)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no card read emitted")
	}
}

func TestBusSequenceProgression(t *testing.T) {
	f := &fakePD{addr: 9}
	newTestBus(t, f)
	waitFor(t, 2*time.Second, func() bool { return len(f.seqList()) >= 5 })
	got := f.seqList()[:5]
	want := []byte{0, 1, 2, 3, 1} // INIT(0) → CAPDET(1) → poll 2,3,then wraps to 1
	if !bytes.Equal(got, want) {
		t.Errorf("sequence = %v, want %v", got, want)
	}
}

func TestBusOfflineAndRecovery(t *testing.T) {
	f := &fakePD{addr: 3}
	newTestBus(t, f)
	waitFor(t, 2*time.Second, func() bool { return f.sawCode(wire.CmdPoll) })

	initIDs := f.countCode(wire.CmdID) // 1 from the first INIT
	f.setMuted(true)
	// Silence drives the PD offline after maxRetries, then it re-INITs (a fresh
	// CMD_ID) once the offline cool-down elapses — even while still muted.
	waitFor(t, 3*time.Second, func() bool { return f.countCode(wire.CmdID) > initIDs })

	// Reviving the PD must bring it back to ONLINE (polls resume).
	f.setMuted(false)
	pollsBefore := f.countCode(wire.CmdPoll)
	waitFor(t, 2*time.Second, func() bool { return f.countCode(wire.CmdPoll) > pollsBefore })
}
