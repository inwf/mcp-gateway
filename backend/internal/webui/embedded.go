//go:build webui

package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var assets embed.FS

// FS returns the built interface, rooted at the directory holding
// index.html.
func FS() fs.FS {
	// The embedded tree is rooted at "dist"; the server expects to find
	// index.html at the top.
	sub, err := fs.Sub(assets, "dist")
	if err != nil {
		// Only reachable if the embed directive above and this path
		// disagree, which is a build-time mistake rather than a runtime
		// condition.
		panic("webui: embedded assets are not rooted at dist: " + err.Error())
	}
	return sub
}

// Present reports whether this binary carries a web interface.
func Present() bool { return true }
