// Package webui carries the built web interface.
//
// Whether a binary has one is a build-time choice, made with the
// `webui` build tag rather than by whether a directory happens to be
// present. Two reasons: `go build ./...` and `go test ./...` must work
// in a tree where the frontend has never been built, and a build that
// was meant to include the interface should fail loudly when the
// assets are missing rather than quietly producing a binary that
// serves nothing.
package webui
