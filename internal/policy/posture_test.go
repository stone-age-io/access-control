package policy

import "testing"

// IsSettablePosture is the single gate shared by the accessd command bridge and
// the edge controller; this pins the exact accepted set so the two cannot drift.
func TestIsSettablePosture(t *testing.T) {
	for _, p := range []string{
		PostureSecure, PostureFreeAccess, PostureUnlocked, PostureLockdown, PostureDisabled,
	} {
		if !IsSettablePosture(p) {
			t.Errorf("IsSettablePosture(%q) = false, want true", p)
		}
	}
	// "clear" is a revert directive, not a settable posture — the command path
	// handles it before the gate, so the predicate must reject it.
	for _, p := range []string{"", "clear", "bogus", "Secure", "open", "grant"} {
		if IsSettablePosture(p) {
			t.Errorf("IsSettablePosture(%q) = true, want false", p)
		}
	}
}
