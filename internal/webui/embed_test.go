package webui

import (
	"io/fs"
	"testing"
)

// TestEmbeddedUIPresent asserts the compiled UI is actually embedded — index.html
// must be reachable under the "public" prefix exactly as accessd serves it. It
// guards against shipping a binary whose UI was never built (the embed would
// otherwise fall back to the .gitkeep placeholder and 404 at "/").
func TestEmbeddedUIPresent(t *testing.T) {
	sub, err := fs.Sub(FS, "public")
	if err != nil {
		t.Fatalf("fs.Sub(FS, public): %v", err)
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		t.Fatalf("embedded UI missing index.html — run `npm --prefix ui run build` then rebuild: %v", err)
	}
}
