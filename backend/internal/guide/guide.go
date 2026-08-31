// Package guide holds the usage guide and the ways of reading it.
//
// It is a package of its own because two surfaces serve the same document:
// the CLI's `guide` command, and the MCP resource the gateway publishes so
// that a model can read it without being told to. Two embedded copies
// would be two documents that happen to start out identical, and the way
// that fails is silent — one of them gets edited and the other keeps
// answering the old text.
package guide

import (
	_ "embed"
	"strings"
)

// The guide is Markdown in its own file rather than a string constant, so
// that it can be read and edited as the document it is — and so that a
// change to it is a diff of prose rather than of Go quoting.
//
//go:embed guide.md
var document string

// MIMEType is what the guide is, for the places that have to say.
const MIMEType = "text/markdown"

// Text is the whole guide.
func Text() string { return document }

// Section extracts one "## " section, so that a reader after one answer
// does not have to scroll the whole document.
//
// Matching is on a substring of the heading rather than an exact title:
// the headings are prose, and requiring one to be typed exactly would
// make the flag more trouble than reading the whole thing.
func Section(wanted string) (string, bool) {
	var (
		found   []string
		heading string
		keeping bool
	)
	lower := strings.ToLower(wanted)

	for _, line := range strings.Split(document, "\n") {
		if after, isHeading := strings.CutPrefix(line, "## "); isHeading {
			heading = after
			keeping = strings.Contains(strings.ToLower(heading), lower)
		}
		if keeping {
			found = append(found, line)
		}
	}

	if len(found) == 0 {
		return "", false
	}
	return strings.TrimRight(strings.Join(found, "\n"), "\n") + "\n", true
}

// Headings lists the section titles, for a caller that has to say which
// sections do exist.
func Headings() []string {
	var headings []string
	for _, line := range strings.Split(document, "\n") {
		if after, isHeading := strings.CutPrefix(line, "## "); isHeading {
			headings = append(headings, after)
		}
	}
	return headings
}
