// Package version exposes build metadata, overridden via -ldflags at build time.
package version

var (
	Version = "0.0.0-dev"
	Commit  = "unknown"
)
