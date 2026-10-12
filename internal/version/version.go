// file: internal/version/version.go

// Package version reports which build of this module a binary is, from the build
// info `go build` embeds. There are no ldflags: a release and a plain `go build`
// of the same checkout stamp the same thing, so the two cannot disagree.
package version

import "runtime/debug"

// Main is the module version `go build` stamped from the checkout: the tag on a
// clean tagged checkout (v0.1.0), a pseudo-version between tags, a "+dirty"
// suffix with uncommitted changes, and "(devel)" under `go run` or without VCS
// info.
func Main() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "(devel)"
}

// Dependency is the version of the module at path that this binary was built
// against, or "unknown". accessd reports PocketBase's this way because
// pocketbase.Version is only stamped in PocketBase's own releases and reads
// "(untracked)" in a binary that imports it as a library.
func Dependency(path string) string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, dep := range info.Deps {
		if dep.Path == path {
			return dep.Version
		}
	}
	return "unknown"
}
