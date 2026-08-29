package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"mcphub/internal/gateway"
)

// listLimit is what the CLI asks for when listing tools.
//
// The API truncates to a default of 50 and says nothing about having done
// so, which would make this command quietly lie about an installation
// with more tools than that. A high explicit limit plus the note printed
// when the result reaches it is what keeps the output honest.
const listLimit = 1000

type toolListResponse struct {
	Tools []aggregatedTool `json:"tools"`
	Total int              `json:"total"`
}

// gatewayToolsResponse is what /gateway/tools answers: everything the
// gateway offers a client, and the names of those that are its own.
type gatewayToolsResponse struct {
	Tools       []gatewayTool `json:"tools"`
	SystemTools []string      `json:"systemTools"`
}

// gatewayTool is a tool as the MCP server publishes it. A gateway tool
// has no server behind it, so this is all there is to say about one.
type gatewayTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type aggregatedTool struct {
	Server      string         `json:"server"`
	Tool        string         `json:"tool"`
	Exposed     string         `json:"exposed"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`

	// system marks one of the gateway's own tools. They belong to no
	// server, so they are listed apart and called by another route.
	system bool
}

// toolSet is everything on offer, with the gateway's own tools kept
// apart from the forwarded ones.
type toolSet struct {
	system   []aggregatedTool
	upstream []aggregatedTool
}

func (s toolSet) all() []aggregatedTool {
	return append(slices.Clone(s.system), s.upstream...)
}

func (s toolSet) empty() bool { return len(s.system) == 0 && len(s.upstream) == 0 }

type toolCallResponse struct {
	IsError           bool            `json:"isError"`
	Content           []contentBlock  `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent"`
}

type contentBlock struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	MIMEType string `json:"mimeType"`
}

func newToolsCommand(global *globalOptions, stdout io.Writer) *cobra.Command {
	client := &clientOptions{global: global}

	cmd := &cobra.Command{
		Use:   "tools",
		Short: "Inspect and call the tools the gateway offers",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return usagef("specify a subcommand: %s", availableCommands(cmd))
		},
	}
	client.bind(cmd.PersistentFlags())

	cmd.AddCommand(
		newToolsListCommand(client, stdout),
		newToolsShowCommand(client, stdout),
		newToolsCallCommand(client, stdout),
	)
	return cmd
}

// ===== tools list =====

func newToolsListCommand(client *clientOptions, stdout io.Writer) *cobra.Command {
	var server, search string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the tools the gateway is offering",
		Long: "List the tools the gateway currently offers, under the names a client\n" +
			"sees them by. A tool a server has but the configuration does not expose\n" +
			"is not listed, because it is not on offer.\n\n" +
			"The gateway's own tools are listed first, apart from the forwarded ones:\n" +
			"they belong to no server, and they are how a model finds everything else.",
		Args: noPositionalArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			gateway, err := client.connect()
			if err != nil {
				return err
			}
			tools, err := fetchTools(cmd.Context(), gateway, search)
			if err != nil {
				return err
			}
			return printTools(stdout, tools, server, search)
		},
	}

	cmd.Flags().StringVar(&server, "server", "", "only tools from this server")
	cmd.Flags().StringVar(&search, "search", "", "rank the tools by how well they match these words")
	return cmd
}

// fetchTools asks for everything on offer: the forwarded tools from the
// aggregated endpoint, and the gateway's own from the endpoint that
// reports what its MCP server publishes.
//
// Two calls, because the two kinds of tool are genuinely different
// things: a forwarded tool has a server, a name on that server and a name
// it is exposed under, and the gateway's own has none of those.
func fetchTools(ctx context.Context, gateway *gatewayClient, search string) (toolSet, error) {
	query := url.Values{}
	query.Set("limit", strconv.Itoa(listLimit))
	if search != "" {
		query.Set("q", search)
	}

	var forwarded toolListResponse
	if err := gateway.get(ctx, "/tools?"+query.Encode(), &forwarded); err != nil {
		return toolSet{}, err
	}

	system, err := fetchSystemTools(ctx, gateway, search)
	if err != nil {
		return toolSet{}, err
	}

	return toolSet{system: system, upstream: forwarded.Tools}, nil
}

func fetchSystemTools(ctx context.Context, gateway *gatewayClient, search string) ([]aggregatedTool, error) {
	var response gatewayToolsResponse
	if err := gateway.get(ctx, "/gateway/tools", &response); err != nil {
		return nil, err
	}

	out := make([]aggregatedTool, 0, len(response.SystemTools))
	for _, tool := range response.Tools {
		if !slices.Contains(response.SystemTools, tool.Name) {
			continue
		}
		// A gateway tool is offered under its own name, so the name a
		// client calls and the name it is known by are the same one.
		out = append(out, aggregatedTool{
			Tool:        tool.Name,
			Exposed:     tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
			system:      true,
		})
	}

	// The aggregated endpoint ranks its own results, so these have to be
	// ranked here to match — otherwise a search would filter one group and
	// not the other.
	if search != "" {
		out = rankTools(search, out)
	}
	return out, nil
}

// rankTools applies the gateway's own ranking to a list the server did
// not rank, so that both groups of a search answer the same question.
func rankTools(search string, tools []aggregatedTool) []aggregatedTool {
	byName := make(map[string]aggregatedTool, len(tools))
	candidates := make([]gateway.Searchable, 0, len(tools))
	for _, tool := range tools {
		byName[tool.Exposed] = tool
		candidates = append(candidates, gateway.Searchable{
			Tool:        tool.Tool,
			Exposed:     tool.Exposed,
			Description: tool.Description,
		})
	}

	hits := gateway.SearchTools(search, candidates, len(tools))
	out := make([]aggregatedTool, 0, len(hits))
	for _, hit := range hits {
		out = append(out, byName[hit.Exposed])
	}
	return out
}

func printTools(stdout io.Writer, tools toolSet, server, search string) error {
	// A server filter is a question about one server, and the gateway's
	// own tools are not on any server — so they are not an answer to it.
	if server != "" {
		tools.system = nil
		kept := tools.upstream[:0]
		for _, tool := range tools.upstream {
			if tool.Server == server {
				kept = append(kept, tool)
			}
		}
		tools.upstream = kept
	}

	if tools.empty() {
		fmt.Fprintln(stdout, nothingToShow(server, search))
		return nil
	}

	if len(tools.system) > 0 {
		fmt.Fprintf(stdout, "GATEWAY TOOLS (%d)\n", len(tools.system))
		rows := newTable(stdout, "NAME", "DESCRIPTION")
		for _, tool := range tools.system {
			rows.row(tool.Exposed, summarise(tool.Description))
		}
		rows.flush()
	}

	if len(tools.upstream) > 0 {
		if len(tools.system) > 0 {
			fmt.Fprintln(stdout)
		}
		fmt.Fprintf(stdout, "SERVER TOOLS (%d)\n", len(tools.upstream))
		rows := newTable(stdout, "NAME", "SERVER", "DESCRIPTION")
		for _, tool := range tools.upstream {
			rows.row(tool.Exposed, tool.Server, summarise(tool.Description))
		}
		rows.flush()
	}

	if len(tools.upstream) == listLimit {
		fmt.Fprintf(stdout, "\nstopped at %d tools; there may be more\n", listLimit)
	}
	return nil
}

func nothingToShow(server, search string) string {
	switch {
	case search != "" && server != "":
		return fmt.Sprintf("no tools on %s match %q", server, search)
	case search != "":
		return fmt.Sprintf("no tools match %q", search)
	case server != "":
		return fmt.Sprintf("%s is offering no tools", server)
	default:
		return "no tools are being offered"
	}
}

// descriptionWidth bounds the last column of the tool list.
//
// Real servers write long descriptions — the official filesystem server's
// run past 300 characters — and an untruncated column wraps across
// several terminal lines each, which destroys the list as a list. The
// first sentence is what a list is for; `get_tool` is where the full text
// lives.
const descriptionWidth = 96

// summarise reduces a description to one short line.
func summarise(s string) string {
	line, _, multiline := strings.Cut(s, "\n")
	line = strings.TrimSpace(line)

	// Counting runes rather than bytes: a description may be in any
	// language, and cutting a multi-byte character in half produces a
	// replacement character rather than a shorter line.
	runes := []rune(line)
	if len(runes) > descriptionWidth {
		return strings.TrimSpace(string(runes[:descriptionWidth])) + "…"
	}
	if multiline {
		return line + " …"
	}
	return line
}

// ===== tools show =====

func newToolsShowCommand(client *clientOptions, stdout io.Writer) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "show <tool>",
		Short: "Show one tool in full, with the arguments it takes",
		Long: "Show everything about one tool: where it came from, what it does, and\n" +
			"the arguments it declares.\n\n" +
			"`tools list` shortens each description to one line so that the list\n" +
			"stays a list. This is where the full text lives, and the only place\n" +
			"the input schema is shown — which is what you need to build a call.",
		Args: exactlyOneArg("tool"),
		RunE: func(cmd *cobra.Command, args []string) error {
			gateway, err := client.connect()
			if err != nil {
				return err
			}
			tools, err := fetchTools(cmd.Context(), gateway, "")
			if err != nil {
				return err
			}
			tool, err := resolveTool(tools, args[0])
			if err != nil {
				return err
			}
			return showTool(stdout, tool, asJSON)
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "print the tool as JSON instead of rendering it")
	return cmd
}

func showTool(stdout io.Writer, tool aggregatedTool, asJSON bool) error {
	if asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(map[string]any{
			"exposed":     tool.Exposed,
			"tool":        tool.Tool,
			"server":      tool.Server,
			"system":      tool.system,
			"description": tool.Description,
			"inputSchema": tool.InputSchema,
		})
	}

	facts := newTable(stdout)
	facts.row("name", tool.Exposed)
	if tool.system {
		// A gateway tool is not forwarded from anywhere, and saying "-"
		// under a SERVER heading would leave a reader wondering which one.
		facts.row("origin", "the gateway itself")
	} else {
		facts.row("server", tool.Server)
		if tool.Tool != tool.Exposed {
			// The gateway prefixes, and renames on a collision. The name on
			// the server is what its own documentation talks about.
			facts.row("name on the server", tool.Tool)
		}
	}
	facts.flush()

	if tool.Description != "" {
		fmt.Fprintf(stdout, "\n%s\n", tool.Description)
	}

	printArguments(stdout, tool.InputSchema)
	return nil
}

// printArguments renders the input schema as a list of arguments, and
// then in full.
//
// The list is what someone building a call actually reads: names, types,
// and which are required. The schema follows because it is the only
// complete answer — an upstream is free to use constructs no summary
// covers, and a summary that quietly dropped one would be worse than no
// summary at all.
func printArguments(stdout io.Writer, schema map[string]any) {
	if len(schema) == 0 {
		fmt.Fprintln(stdout, "\nthis tool takes no arguments")
		return
	}

	properties, _ := schema["properties"].(map[string]any)
	if len(properties) == 0 {
		fmt.Fprintln(stdout, "\nthis tool declares no arguments")
	} else {
		required := map[string]bool{}
		if list, ok := schema["required"].([]any); ok {
			for _, name := range list {
				if text, ok := name.(string); ok {
					required[text] = true
				}
			}
		}

		fmt.Fprintln(stdout, "\nARGUMENTS")
		rows := newTable(stdout, "NAME", "TYPE", "REQUIRED", "DESCRIPTION")
		for _, name := range slices.Sorted(maps.Keys(properties)) {
			property, _ := properties[name].(map[string]any)
			kind, _ := property["type"].(string)
			description, _ := property["description"].(string)
			rows.row(name, dash(kind), yesNo(required[name]), summarise(description))
		}
		rows.flush()
	}

	encoded, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return
	}
	fmt.Fprintf(stdout, "\nINPUT SCHEMA\n%s\n", encoded)
}

// ===== tools call =====

func newToolsCallCommand(client *clientOptions, stdout io.Writer) *cobra.Command {
	var (
		argsJSON string
		pairs    []string
		asJSON   bool
	)

	cmd := &cobra.Command{
		Use:   "call <tool>",
		Short: "Call a tool through the gateway",
		Long: "Call a tool and print what it returned.\n\n" +
			"The tool is named the way a client would name it — the exposed name,\n" +
			"such as \"files_read\". A bare tool name is accepted too when only one\n" +
			"server offers it. The gateway's own tools, such as \"list_servers\", are\n" +
			"called by their own names.\n\n" +
			"Arguments can be given as one JSON object with --args, as repeated\n" +
			"--arg key=value pairs, or both; a pair overrides the same key in the\n" +
			"JSON. A pair's value is converted using the type the tool's schema\n" +
			"declares for that argument, so --arg limit=5 sends a number where the\n" +
			"schema asks for one and the string \"5\" where it asks for a string.",
		Args: exactlyOneArg("tool"),
		RunE: func(cmd *cobra.Command, args []string) error {
			gateway, err := client.connect()
			if err != nil {
				return err
			}

			tools, err := fetchTools(cmd.Context(), gateway, "")
			if err != nil {
				return err
			}
			tool, err := resolveTool(tools, args[0])
			if err != nil {
				return err
			}

			arguments, err := buildArguments(argsJSON, pairs, tool.InputSchema)
			if err != nil {
				return err
			}

			return callTool(cmd.Context(), gateway, stdout, tool, arguments, asJSON)
		},
	}

	cmd.Flags().StringVar(&argsJSON, "args", "", "arguments as a JSON object")
	cmd.Flags().StringArrayVar(&pairs, "arg", nil,
		"one argument as key=value; repeat for more")
	cmd.Flags().BoolVar(&asJSON, "json", false,
		"print the raw result instead of rendering it")
	return cmd
}

// resolveTool finds the tool the user meant.
//
// The exposed name is what a client sees and is always unambiguous, so it
// wins. A bare tool name is accepted as a convenience when exactly one
// server offers it; when several do, refusing and naming them is the only
// safe answer, because picking one would silently call the wrong server.
func resolveTool(tools toolSet, name string) (aggregatedTool, error) {
	candidates := tools.all()

	var bare []aggregatedTool
	for _, tool := range candidates {
		if tool.Exposed == name {
			return tool, nil
		}
		if tool.Tool == name {
			bare = append(bare, tool)
		}
	}

	switch len(bare) {
	case 1:
		return bare[0], nil
	case 0:
		return aggregatedTool{}, fmt.Errorf("no tool named %q is being offered%s",
			name, suggest(candidates, name))
	default:
		names := make([]string, len(bare))
		for i, tool := range bare {
			names[i] = tool.Exposed
		}
		return aggregatedTool{}, fmt.Errorf(
			"%q is offered by more than one server; use one of: %s",
			name, strings.Join(names, ", "))
	}
}

// suggest offers the names that contain what was typed, which covers the
// usual case of a half-remembered tool.
func suggest(tools []aggregatedTool, name string) string {
	var near []string
	lower := strings.ToLower(name)
	for _, tool := range tools {
		if strings.Contains(strings.ToLower(tool.Exposed), lower) {
			near = append(near, tool.Exposed)
		}
	}
	if len(near) == 0 {
		return "; run \"mcphub tools list\" to see what is"
	}
	if len(near) > 5 {
		near = near[:5]
	}
	return "; did you mean " + strings.Join(near, ", ") + "?"
}

// buildArguments combines the two ways of giving arguments.
func buildArguments(argsJSON string, pairs []string, schema map[string]any) (map[string]any, error) {
	arguments := map[string]any{}

	if argsJSON != "" {
		if err := json.Unmarshal([]byte(argsJSON), &arguments); err != nil {
			return nil, fmt.Errorf("--args is not a JSON object: %w", err)
		}
	}

	for _, pair := range pairs {
		key, raw, found := strings.Cut(pair, "=")
		if !found || key == "" {
			return nil, usagef("--arg %q is not in key=value form", pair)
		}
		value, err := coerce(raw, declaredType(schema, key))
		if err != nil {
			return nil, fmt.Errorf("--arg %s: %w", key, err)
		}
		arguments[key] = value
	}

	return arguments, nil
}

// declaredType reports the JSON Schema type the tool declares for one
// argument, or "" when it declares none.
func declaredType(schema map[string]any, key string) string {
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return ""
	}
	property, ok := properties[key].(map[string]any)
	if !ok {
		return ""
	}
	declared, _ := property["type"].(string)
	return declared
}

// coerce turns a command-line string into the type the schema asks for.
//
// Everything on a command line is a string, but a tool that wants a
// number and is handed "5" rejects it. Using the declared type rather
// than guessing is what makes --arg limit=5 and --arg name=5 both do the
// right thing.
func coerce(raw, declared string) (any, error) {
	switch declared {
	case "string":
		return raw, nil

	case "integer":
		number, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("the schema asks for an integer, and %q is not one", raw)
		}
		return number, nil

	case "number":
		number, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("the schema asks for a number, and %q is not one", raw)
		}
		return number, nil

	case "boolean":
		truth, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("the schema asks for true or false, and %q is neither", raw)
		}
		return truth, nil

	case "array", "object":
		var decoded any
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			return nil, fmt.Errorf("the schema asks for a JSON %s: %w", declared, err)
		}
		return decoded, nil

	default:
		// The tool did not say, so the value is read as JSON if it looks
		// like JSON and left as text otherwise. That makes numbers and
		// booleans work without making every bare word an error.
		var decoded any
		if err := json.Unmarshal([]byte(raw), &decoded); err == nil {
			return decoded, nil
		}
		return raw, nil
	}
}

func callTool(ctx context.Context, gateway *gatewayClient, stdout io.Writer,
	tool aggregatedTool, arguments map[string]any, asJSON bool) error {

	var result toolCallResponse
	body := map[string]any{"arguments": arguments}
	if err := gateway.post(ctx, callPath(tool), body, &result); err != nil {
		return err
	}

	if asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(result); err != nil {
			return err
		}
	} else {
		renderResult(stdout, result)
	}

	// A tool that ran and reported a problem is a failed command even
	// though the call itself succeeded: a script that ignored this would
	// treat the error text as an answer.
	if result.IsError {
		return errors.New("the tool reported an error")
	}
	return nil
}

// callPath is where a tool is called.
//
// The gateway's own tools have a route of their own because they belong
// to no server: there is no name to put in the per-server path.
func callPath(tool aggregatedTool) string {
	if tool.system {
		return fmt.Sprintf("/gateway/tools/%s/call", url.PathEscape(tool.Tool))
	}
	return fmt.Sprintf("/servers/%s/tools/%s/call",
		url.PathEscape(tool.Server), url.PathEscape(tool.Tool))
}

func renderResult(stdout io.Writer, result toolCallResponse) {
	for _, block := range result.Content {
		if block.Type == "text" {
			fmt.Fprintln(stdout, block.Text)
			continue
		}
		// Binary content is not something to spray at a terminal.
		fmt.Fprintf(stdout, "[%s content%s; use --json to see it]\n",
			block.Type, mimeNote(block.MIMEType))
	}

	// Structured output repeats the text on most servers, so it is only
	// shown when there was no text to show.
	if len(result.Content) == 0 && len(result.StructuredContent) > 0 &&
		string(result.StructuredContent) != "null" {
		var pretty json.RawMessage = result.StructuredContent
		if indented, err := json.MarshalIndent(pretty, "", "  "); err == nil {
			fmt.Fprintln(stdout, string(indented))
		}
	}
}

func mimeNote(mime string) string {
	if mime == "" {
		return ""
	}
	return " (" + mime + ")"
}

// exactlyOneArg names what the missing argument is, which a bare count
// mismatch does not.
func exactlyOneArg(what string) cobra.PositionalArgs {
	return func(_ *cobra.Command, args []string) error {
		switch {
		case len(args) == 0:
			return usagef("no %s given", what)
		case len(args) > 1:
			return usagef("unexpected argument %q; only one %s is taken", args[1], what)
		}
		return nil
	}
}
