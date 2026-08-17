//go:build ui

// Package web embeds the built frontend into production binaries. Build with
// -tags ui after `npm run build` (the Dockerfile does both).
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// UI returns the built frontend rooted at its dist directory.
func UI() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
