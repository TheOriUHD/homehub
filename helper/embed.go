//go:build helper

// Package helper embeds the caddy-reader helper binaries and installer so
// HomeHub serves them from its own origin — a LAN tool should not need the
// internet to set up a LAN helper. Build with -tags helper after running
// `make build-helper`.
package helper

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS returns the helper artifacts (install.sh, binaries, SHA256SUMS), or
// nil when the tag is off.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil
	}
	return sub
}
