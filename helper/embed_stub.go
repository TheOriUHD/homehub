//go:build !helper

package helper

import "io/fs"

// FS returns nil in builds without the helper artifacts.
func FS() fs.FS { return nil }
