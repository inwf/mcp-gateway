package main

import (
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// Tags are how servers are grouped. They are attached to a server rather
// than to a tool, and the aggregated endpoints filter by them, so knowing
// which exist is what makes `--tag` usable at all.

func newTagsCommand(global *globalOptions, stdout io.Writer) *cobra.Command {
	client := &clientOptions{global: global}

	cmd := &cobra.Command{
		Use:   "tags",
		Short: "Inspect the tags the servers are grouped by",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return usagef("specify a subcommand: %s", availableCommands(cmd))
		},
	}
	client.bind(cmd.PersistentFlags())

	cmd.AddCommand(newTagsListCommand(client, stdout))
	return cmd
}

func newTagsListCommand(client *clientOptions, stdout io.Writer) *cobra.Command {
	var server string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the tags in use and which servers carry them",
		Long: "List every tag in use, with the servers carrying it.\n\n" +
			"A tag belongs to a server rather than to a tool, so this is the list\n" +
			"of groups an installation is divided into. It is also what the tag\n" +
			"filters take: a server tagged env=dev is matched by both \"env\" and\n" +
			"\"env=dev\".",
		Args: noPositionalArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			gateway, err := client.connect()
			if err != nil {
				return err
			}

			var response serverListResponse
			if err := gateway.get(cmd.Context(), "/servers", &response); err != nil {
				return err
			}
			return printTags(stdout, response.Servers, server)
		},
	}

	cmd.Flags().StringVar(&server, "server", "", "only the tags this server carries")
	return cmd
}

// taggedServers is one tag and everything carrying it.
type taggedServers struct {
	key     string
	value   string
	servers []string
}

func printTags(stdout io.Writer, servers []serverRow, only string) error {
	if only != "" && !slices.ContainsFunc(servers, func(s serverRow) bool { return s.Name == only }) {
		// Saying "it has no tags" about a server that does not exist would
		// send someone looking for a tag that was never the problem.
		return fmt.Errorf("no server named %q is configured%s", only, nearestServer(servers, only))
	}

	// Grouped by tag rather than listed per server: the question a tag
	// list answers is "what can I filter by", and the same tag on four
	// servers is one filter, not four.
	carriers := map[string][]string{}
	for _, server := range servers {
		if only != "" && server.Name != only {
			continue
		}
		for key, value := range server.Config.Tags {
			carriers[key+"\x00"+value] = append(carriers[key+"\x00"+value], server.Name)
		}
	}

	if len(carriers) == 0 {
		if only != "" {
			fmt.Fprintf(stdout, "%s carries no tags\n", only)
		} else {
			fmt.Fprintln(stdout, "no tags are in use")
		}
		return nil
	}

	tags := make([]taggedServers, 0, len(carriers))
	for combined, names := range carriers {
		key, value, _ := strings.Cut(combined, "\x00")
		sort.Strings(names)
		tags = append(tags, taggedServers{key: key, value: value, servers: names})
	}
	sort.Slice(tags, func(i, j int) bool {
		if tags[i].key != tags[j].key {
			return tags[i].key < tags[j].key
		}
		return tags[i].value < tags[j].value
	})

	rows := newTable(stdout, "TAG", "VALUE", "SERVERS")
	for _, tag := range tags {
		rows.row(tag.key, dash(tag.value), strings.Join(tag.servers, ", "))
	}
	rows.flush()
	return nil
}

// nearestServer offers the configured names, since a tag list is usually
// reached by typing a server name from memory.
func nearestServer(servers []serverRow, name string) string {
	known := make([]string, 0, len(servers))
	for _, server := range servers {
		known = append(known, server.Name)
	}
	if len(known) == 0 {
		return "; no servers are configured"
	}
	slices.Sort(known)
	return "; the configured servers are " + strings.Join(known, ", ")
}
