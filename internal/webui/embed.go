// Package webui embeds the compiled management UI (the Vite build output under
// ./public) so accessd serves it as part of a single self-contained binary —
// no pb_public directory to ship alongside the executable.
//
// Build order matters: the embed happens at Go compile time, so build the UI
// first, then the binary:
//
//	npm --prefix ui run build      # populates internal/webui/public
//	go build ./cmd/accessd          # bakes it into the binary
//
// The built UI under ./public is committed to the repo (like the platform's
// pb_public), so a fresh checkout embeds a working UI and `go build` needs no
// npm. Rebuild the UI and re-commit ./public whenever the frontend changes.
package webui

import "embed"

// FS holds the built UI. Files live under the "public/" prefix; callers should
// fs.Sub(FS, "public") before serving.
//
//go:embed all:public
var FS embed.FS
