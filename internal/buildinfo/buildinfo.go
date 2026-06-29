// Package buildinfo exposes version metadata about the harness binary.
//
// Values are intended to be overridden at build time via -ldflags, e.g.:
//
//	go build -ldflags "-X github.com/thefcan/k8s-resilience-harness/internal/buildinfo.Version=v0.1.0"
package buildinfo

// Build metadata. These default to "dev"/"none" for local `go run` / `go build`
// and are stamped by the release/CI build via -ldflags.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String returns a single-line, human-readable build identifier.
func String() string {
	return Version + " (commit " + Commit + ", built " + Date + ")"
}
