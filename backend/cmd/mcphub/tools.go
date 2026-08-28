package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
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

type aggregatedTool struct {
	Server      string         `json:"server"`
	Tool        string         `json:"tool"`
	Exposed     string         `json:"exposed"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

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
			"is not listed, because it is not on offer.",
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

func fetchTools(ctx context.Context, gateway *gatewayClient, search string) ([]aggregatedTool, error) {
	query := url.Values{}
	query.Set("limit", strconv.Itoa(listLimit))
	if search != "" {
		query.Set("q", search)
	}

	var response toolListResponse
	if err := gateway.get(ctx, "/tools?"+query.Encode(), &response); err != nil {
		return nil, err
	}
	return response.Tools, nil
}

func printTools(stdout io.Writer, tools []aggregatedTool, server, search string) error {
	if server != "" {
		kept := tools[:0]
		for _, tool := range tools {
			if tool.Server == server {
				kept = append(kept, tool)
			}
		}
		tools = kept
	}

	if len(tools) == 0 {
		switch {
		case search != "" && server != "":
			fmt.Fprintf(stdout, "no tools on %s match %q\n", server, search)
		case search != "":
			fmt.Fprintf(stdout, "no tools match %q\n", search)
		case server != "":
			fmt.Fprintf(stdout, "%s is offering no tools\n", server)
		default:
			fmt.Fprintln(stdout, "no tools are being offered")
		}
		return nil
	}

	rows := newTable(stdout, "NAME", "SERVER", "DESCRIPTION")
	for _, tool := range tools {
		rows.row(tool.Exposed, tool.Server, summarise(tool.Description))
	}
	rows.flush()

	if len(tools) == listLimit {
		fmt.Fprintf(stdout, "\nstopped at %d tools; there may be more\n", listLimit)
	}
	return nil
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
			"server offers it.\n\n" +
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
func resolveTool(tools []aggregatedTool, name string) (aggregatedTool, error) {
	var bare []aggregatedTool
	for _, tool := range tools {
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
			name, suggest(tools, name))
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

	path := fmt.Sprintf("/servers/%s/tools/%s/call",
		url.PathEscape(tool.Server), url.PathEscape(tool.Tool))

	var result toolCallResponse
	body := map[string]any{"arguments": arguments}
	if err := gateway.post(ctx, path, body, &result); err != nil {
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
