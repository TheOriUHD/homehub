// Package version exposes the application version, injected at build time.
package version

// Version is set via -ldflags "-X homehub/internal/version.Version=x.y.z".
var Version = "dev"
