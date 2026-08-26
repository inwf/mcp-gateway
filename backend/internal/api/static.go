package api

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
)

// indexFile is the single-page app's entry document.
const indexFile = "index.html"

// staticFiles serves the web UI.
//
// A single-page app owns its own routing, so any path it does not have a
// file for has to return the entry document rather than a 404 — that is
// what lets a user reload the page on a deep link. An API path must
// never fall through to it: a client parsing HTML where it expected an
// error envelope gets a confusing decode failure instead of the status
// that actually happened.
type staticFiles struct {
	files fs.FS
}

// newStaticFiles prepares a file system for serving, reporting whether
// there is anything to serve. A build without the frontend embedded, or
// a development tree where it has not been built yet, has not.
func newStaticFiles(files fs.FS) (*staticFiles, bool) {
	if files == nil {
		return nil, false
	}
	if _, err := fs.Stat(files, indexFile); err != nil {
		return nil, false
	}
	return &staticFiles{files: files}, true
}

// serve responds with the requested file, or with the entry document.
func (s *staticFiles) serve(c *gin.Context) {
	name := strings.TrimPrefix(path.Clean("/"+c.Request.URL.Path), "/")
	if name == "" {
		name = indexFile
	}

	if s.tryFile(c, name) {
		return
	}
	// Unknown path: hand the app its entry document and let its router
	// decide what the path means.
	if !s.tryFile(c, indexFile) {
		fail(c, Internal("the web interface is not available", errors.New("no "+indexFile)))
	}
}

// tryFile writes one file, reporting whether it existed.
func (s *staticFiles) tryFile(c *gin.Context, name string) bool {
	file, err := s.files.Open(name)
	if err != nil {
		return false
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil || info.IsDir() {
		return false
	}

	// ServeContent needs to seek to sniff the type and to answer range
	// requests. An embedded file does; something exotic might not, in
	// which case a plain copy is still correct.
	seeker, ok := file.(io.ReadSeeker)
	if !ok {
		c.Data(http.StatusOK, contentTypeOf(name), nil)
		_, _ = io.Copy(c.Writer, file)
		return true
	}

	// The entry document must not be cached: a stale one would go on
	// loading the previous build's assets, which the new build no longer
	// has.
	if name == indexFile {
		c.Header("Cache-Control", "no-cache")
	}
	http.ServeContent(c.Writer, c.Request, name, info.ModTime(), seeker)
	return true
}

// contentTypeOf is a fallback for the rare file that cannot be seeked
// and so cannot be sniffed.
func contentTypeOf(name string) string {
	switch path.Ext(name) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json":
		return "application/json"
	case ".svg":
		return "image/svg+xml"
	default:
		return "application/octet-stream"
	}
}
