package commandapi

import "testing"

func TestValidPosture(t *testing.T) {
	valid := []string{"secure", "free_access", "unlocked", "lockdown", "disabled", "clear"}
	for _, p := range valid {
		if !validPosture(p) {
			t.Errorf("validPosture(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"", "bogus", "Secure", "open", "grant"} {
		if validPosture(p) {
			t.Errorf("validPosture(%q) = true, want false", p)
		}
	}
}

func TestValidAction(t *testing.T) {
	for _, a := range []string{"on", "off", "pulse"} {
		if !validAction(a) {
			t.Errorf("validAction(%q) = false, want true", a)
		}
	}
	for _, a := range []string{"", "ON", "toggle", "grant"} {
		if validAction(a) {
			t.Errorf("validAction(%q) = true, want false", a)
		}
	}
}
