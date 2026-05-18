// Package version exposes the binary's build-stamped version string,
// overridden at link time via -ldflags.
package version

// BuildVersion is the human-readable version of this binary, overridden at
// link time (defaults to "dev" for non-release builds).
var BuildVersion = "dev"

// String returns BuildVersion for callers that prefer a function over a
// package variable.
func String() string {
	return BuildVersion
}
