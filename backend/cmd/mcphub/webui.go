package main

import "io/fs"

// webUI returns the built frontend, or nil when this binary was built
// without one.
//
// The frontend is embedded at a later step; until then a backend-only
// build is the only kind there is, and the API reports that no web
// interface is available rather than pretending otherwise.
func webUI() fs.FS { return nil }
