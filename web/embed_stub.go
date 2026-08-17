//go:build !ui

package web

import "io/fs"

// UI returns nil in development builds; the Vite dev server serves the
// frontend and proxies /api to this process.
func UI() fs.FS { return nil }
