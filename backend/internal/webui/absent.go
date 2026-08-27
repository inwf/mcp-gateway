//go:build !webui

package webui

import "io/fs"

// FS returns nothing: this binary was built without the web interface.
// The API reports that none is available rather than pretending
// otherwise.
func FS() fs.FS { return nil }

// Present reports whether this binary carries a web interface.
func Present() bool { return false }
