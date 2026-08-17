//go:build companion

// Package companion embeds the built companion plugin jars so a production
// HomeHub serves its own downloads. The Gradle build writes dist/ (one jar
// per loader plus manifest.json); `go build -tags companion` picks them up.
package companion

import (
	"embed"
	"io/fs"
)

//go:embed dist
var embedded embed.FS

// FS returns the dist tree, or nil when the build produced nothing.
func FS() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		return nil
	}
	return sub
}
