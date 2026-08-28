package main

import (
	_ "embed"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

// The guide is Markdown in its own file rather than a string constant, so
// that it can be read and edited as the document it is — and so that a
// change to it is a diff of prose rather than of Go quoting.
//
//go:embed guide.md
var guide string

func newGuideCommand(stdout io.Writer) *cobra.Command {
	var topic string

	cmd := &cobra.Command{
		Use:   "guide",
		Short: "Print the usage guide",
		Long: "Print the usage guide as Markdown.\n\n" +
			"It is written to standard output unrendered, so it can be piped to a\n" +
			"pager, saved to a file, or handed to an assistant that reads Markdown.",
		Args: noPositionalArgs,
		RunE: func(*cobra.Command, []string) error {
			text := guide
			if topic != "" {
				section, ok := guideSection(guide, topic)
				if !ok {
					return usagef("no section matches %q; the sections are: %s",
						topic, strings.Join(guideSections(guide), ", "))
				}
				text = section
			}
			_, err := io.WriteString(stdout, text)
			return err
		},
	}

	cmd.Flags().StringVar(&topic, "section", "",
		"print only the section whose heading contains this text")
	return cmd
}

// guideSection extracts one "## " section, so that a reader after one
// answer does not have to scroll the whole document.
//
// Matching is on a substring of the heading rather than an exact title:
// the headings are prose, and requiring one to be typed exactly would
// make the flag more trouble than reading the whole thing.
func guideSection(document, wanted string) (string, bool) {
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

func guideSections(document string) []string {
	var headings []string
	for _, line := range strings.Split(document, "\n") {
		if after, isHeading := strings.CutPrefix(line, "## "); isHeading {
			headings = append(headings, fmt.Sprintf("%q", after))
		}
	}
	return headings
}
