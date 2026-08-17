// Package version exposes the application version, injected at build time.
package version

// Version is set via -ldflags "-X github.com/TheOriUHD/homehub/internal/version.Version=x.y.z".
var Version = "dev"
