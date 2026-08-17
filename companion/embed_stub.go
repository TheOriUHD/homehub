//go:build !companion

package companion

import "io/fs"

// FS is nil in builds without the companion tag (development): the
// downloads API then reports no builds instead of stale ones.
func FS() fs.FS { return nil }
