// Package version carries build metadata.
package version

// Values are overridden at build time with -ldflags.
var (
	Version = "0.1.0-dev"
	Commit  = "none"
	Date    = "unknown"
)

// String returns a human-readable version line.
func String() string { return Version + " (" + Commit + ", " + Date + ")" }
