package policykv

import "testing"

// The contact-/lock-type labels translate to line-sense inversion with the
// per-input default living here: a DPS contact defaults normally-closed, a
// REX/aux contact defaults normally-open, so only the non-default value inverts.
func TestPortalSenseDerivation(t *testing.T) {
	cases := []struct {
		name                       string
		p                          Portal
		wantDPS, wantREX, wantMag  bool
	}{
		{"defaults (empty) do not invert", Portal{}, false, false, false},
		{"dps normally-open inverts", Portal{DpsContact: "no"}, true, false, false},
		{"dps normally-closed is the default", Portal{DpsContact: "nc"}, false, false, false},
		{"rex normally-closed inverts", Portal{RexContact: "nc"}, false, true, false},
		{"rex normally-open is the default", Portal{RexContact: "no"}, false, false, false},
		{"maglock lock type", Portal{LockType: "maglock"}, false, false, true},
		{"strike lock type is the default", Portal{LockType: "strike"}, false, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.p.DPSInvert(); got != c.wantDPS {
				t.Errorf("DPSInvert() = %v, want %v", got, c.wantDPS)
			}
			if got := c.p.REXInvert(); got != c.wantREX {
				t.Errorf("REXInvert() = %v, want %v", got, c.wantREX)
			}
			if got := c.p.IsMaglock(); got != c.wantMag {
				t.Errorf("IsMaglock() = %v, want %v", got, c.wantMag)
			}
		})
	}
}

func TestAuxInputSenseDerivation(t *testing.T) {
	if (AuxInput{}).Invert() {
		t.Error("empty aux contact should not invert (default normally-open)")
	}
	if (AuxInput{Contact: "no"}).Invert() {
		t.Error("normally-open aux contact should not invert")
	}
	if !(AuxInput{Contact: "nc"}).Invert() {
		t.Error("normally-closed aux contact should invert")
	}
}
