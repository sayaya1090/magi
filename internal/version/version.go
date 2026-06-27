// Package version exposes build metadata injected at link time via -ldflags
// (set by goreleaser). Defaults indicate a local/dev build.
package version

import "fmt"

var (
	// Version is the semantic version, e.g. "v0.3.1" (or "dev").
	Version = "dev"
	// Commit is the short git SHA.
	Commit = "none"
	// Date is the build timestamp (RFC3339).
	Date = "unknown"
)

// String returns a human-readable version line.
func String() string {
	return fmt.Sprintf("magi %s (commit %s, built %s)", Version, Commit, Date)
}
