package hardware

import "testing"

func TestServerMiniRelayMapping(t *testing.T) {
	p, ok := ProfileFor("kincony-server-mini")
	if !ok {
		t.Fatal("kincony-server-mini profile not registered")
	}
	// Spot-check the documented BCM pins and that relays are GPIO, active-high.
	want := map[int]int{1: 5, 4: 4, 8: 26}
	for idx, off := range want {
		s, ok := p.Relay(idx)
		if !ok {
			t.Errorf("relay %d missing", idx)
			continue
		}
		if s.Backend != BackendGPIO || s.Chip != "gpiochip0" || s.Offset != off || s.ActiveLow {
			t.Errorf("relay %d = %+v, want gpiochip0 offset %d active-high", idx, s, off)
		}
	}
}

func TestServerMiniInputMapping(t *testing.T) {
	p, _ := ProfileFor("kincony-server-mini")
	want := map[int]int{1: 18, 5: 12, 8: 21}
	for idx, off := range want {
		s, ok := p.Input(idx)
		if !ok {
			t.Errorf("input %d missing", idx)
			continue
		}
		if s.Backend != BackendGPIO || s.Offset != off || !s.ActiveLow {
			t.Errorf("input %d = %+v, want gpiochip0 offset %d active-low", idx, s, off)
		}
	}
}

// Out-of-range and unset (0) indices resolve to "not found", so the GPIO driver
// fails closed rather than driving a wrong line.
func TestUnknownIndex(t *testing.T) {
	p, _ := ProfileFor("kincony-server-mini")
	if _, ok := p.Relay(0); ok {
		t.Error("relay index 0 (unset) resolved")
	}
	if _, ok := p.Relay(99); ok {
		t.Error("relay index 99 resolved")
	}
	if _, ok := p.Input(0); ok {
		t.Error("input index 0 (unset) resolved")
	}
}

func TestUnknownModel(t *testing.T) {
	if _, ok := ProfileFor("acme-9000"); ok {
		t.Error("unknown model resolved a profile")
	}
}

// The Pi5R8 stub is registered with I2C-backed descriptors (defined, not driven).
func TestPi5R8StubIsI2C(t *testing.T) {
	p, ok := ProfileFor("kincony-pi5r8")
	if !ok {
		t.Fatal("kincony-pi5r8 stub not registered")
	}
	s, ok := p.Relay(1)
	if !ok || s.Backend != BackendI2C {
		t.Errorf("pi5r8 relay 1 = %+v (ok=%v), want an I2C descriptor", s, ok)
	}
}
