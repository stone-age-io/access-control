package modelsapi

import "testing"

// catalogue exposes every registered model with its line counts and physical
// labels, in stable sorted order — the shape the UI's I/O map and index pickers
// consume.
func TestCatalogue(t *testing.T) {
	cat := catalogue()
	by := map[string]model{}
	for _, m := range cat {
		by[m.Model] = m
	}

	sm, ok := by["kincony-server-mini"]
	if !ok {
		t.Fatal("kincony-server-mini missing from catalogue")
	}
	if sm.Transport != "gpio" {
		t.Errorf("server-mini transport = %q, want gpio", sm.Transport)
	}
	if len(sm.Relays) != 8 || len(sm.Inputs) != 8 {
		t.Fatalf("server-mini = %d relays / %d inputs, want 8/8", len(sm.Relays), len(sm.Inputs))
	}
	if sm.Relays[0].Index != 1 || sm.Relays[0].Label != "BCM 5" {
		t.Errorf("server-mini relay[0] = %+v, want index 1 label \"BCM 5\"", sm.Relays[0])
	}

	pi, ok := by["kincony-pi5r8"]
	if !ok {
		t.Fatal("kincony-pi5r8 missing from catalogue")
	}
	if pi.Transport != "i2c" {
		t.Errorf("pi5r8 transport = %q, want i2c", pi.Transport)
	}
	if pi.Inputs[0].Label != "MCP 0x20 pin 0 (port A)" {
		t.Errorf("pi5r8 input[0] label = %q, want %q", pi.Inputs[0].Label, "MCP 0x20 pin 0 (port A)")
	}
}
