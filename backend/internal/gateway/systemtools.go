package gateway

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mcphub/internal/config"
	"mcphub/internal/upstream"
)

// The gateway's own tools, as opposed to the upstream tools it forwards.
// A model discovers what is available through these.
const (
	ToolListServers             = "list_servers"
	ToolListTools               = "list_tools"
	ToolGetTool                 = "get_tool"
	ToolCallTool                = "call_tool"
	ToolSearchTools             = "search_tools"
	ToolListTags                = "list_tags"
	ToolUpdateServerDescription = "update_server_description"
)

// SystemToolNames lists every gateway tool, for callers that need to
// tell them apart from forwarded ones.
var SystemToolNames = []string{
	ToolListServers,
	ToolListTools,
	ToolGetTool,
	ToolCallTool,
	ToolSearchTools,
	ToolListTags,
	ToolUpdateServerDescription,
}

// IsSystemTool reports whether a name belongs to the gateway itself.
func IsSystemTool(name string) bool {
	return slices.Contains(SystemToolNames, name)
}

// Upstreams is what the system tools need from the set of connected
// servers. Narrowing it to this keeps the tools testable without running
// real child processes.
type Upstreams interface {
	Statuses() []upstream.Status
	Tools() map[string][]*mcp.Tool
	Resources() map[string][]*mcp.Resource
	CallTool(ctx context.Context, server, tool string, args any) (*mcp.CallToolResult, error)
	ReadResource(ctx context.Context, server, uri string) (*mcp.ReadResourceResult, error)
}

// Configs is what the system tools need from the configuration.
type Configs interface {
	Get() config.Config
	Update(mutate func(*config.Config) error) ([]config.Change, error)
}

// ===== list_servers =====

type listServersInput struct{}

// ServerSummary describes one configured server.
type ServerSummary struct {
	Name          string            `json:"name"`
	State         string            `json:"state"`
	Description   string            `json:"description,omitempty"`
	Tags          map[string]string `json:"tags,omitempty"`
	ToolCount     int               `json:"toolCount"`
	ResourceCount int               `json:"resourceCount"`
	Error         string            `json:"error,omitempty"`
}

type listServersOutput struct {
	Servers []ServerSummary `json:"servers"`
}

// ===== list_tools =====

type listToolsInput struct {
	Server string `json:"server" jsonschema:"exact name of the MCP server, as returned by list_servers"`
}

// ToolSummary is a tool without its full schema, which is what a model
// needs to decide whether to look closer.
type ToolSummary struct {
	Name        string `json:"name"`
	Exposed     string `json:"exposed"`
	Description string `json:"description,omitempty"`
}

type listToolsOutput struct {
	Server string        `json:"server"`
	Tools  []ToolSummary `json:"tools"`
}

// ===== get_tool =====

type getToolInput struct {
	Server string `json:"server" jsonschema:"exact name of the MCP server"`
	Tool   string `json:"tool" jsonschema:"exact name of the tool on that server"`
}

type getToolOutput struct {
	Server      string `json:"server"`
	Name        string `json:"name"`
	Exposed     string `json:"exposed"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"inputSchema,omitempty"`
}

// ===== call_tool =====

type callToolInput struct {
	Server string         `json:"server" jsonschema:"exact name of the MCP server"`
	Tool   string         `json:"tool" jsonschema:"exact name of the tool on that server"`
	Args   map[string]any `json:"args,omitempty" jsonschema:"arguments for the tool, matching its input schema"`
}

// ===== search_tools =====

type searchToolsInput struct {
	Query string `json:"query" jsonschema:"words to look for in tool names and descriptions"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum number of results, default 20"`
}

type searchToolsOutput struct {
	Query string      `json:"query"`
	Hits  []SearchHit `json:"hits"`
}

// defaultSearchLimit keeps a broad query from returning every tool in
// the installation.
const defaultSearchLimit = 20

// ===== list_tags =====

type listTagsInput struct {
	Server string `json:"server,omitempty" jsonschema:"limit the result to one server; omit for all servers"`
}

type serverTags struct {
	Server string            `json:"server"`
	Tags   map[string]string `json:"tags,omitempty"`
}

type listTagsOutput struct {
	Servers []serverTags `json:"servers"`
}

// ===== update_server_description =====

type updateDescriptionInput struct {
	Server      string `json:"server" jsonschema:"exact name of the MCP server"`
	Description string `json:"description" jsonschema:"new description, shown when listing servers"`
}

type updateDescriptionOutput struct {
	Server      string `json:"server"`
	Description string `json:"description"`
}

// RegisterSystemTools adds the gateway's own tools to an MCP server.
func RegisterSystemTools(server *mcp.Server, ups Upstreams, cfgs Configs) {
	mcp.AddTool(server, &mcp.Tool{
		Name: ToolListServers,
		Description: "List the configured MCP servers with their connection state, " +
			"description and tags. Start here to find out what is available.",
		Annotations: readOnly("List servers"),
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ listServersInput) (*mcp.CallToolResult, listServersOutput, error) {
		return nil, listServers(ups, cfgs), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: ToolListTools,
		Description: "List the tools one MCP server offers. Use the server name " +
			"exactly as returned by list_servers.",
		Annotations: readOnly("List tools on a server"),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in listToolsInput) (*mcp.CallToolResult, listToolsOutput, error) {
		out, err := listTools(ups, in.Server)
		if err != nil {
			return toolError(err), listToolsOutput{}, nil
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: ToolGetTool,
		Description: "Get the full input schema of one tool, which is what you need " +
			"to build a valid call.",
		Annotations: readOnly("Get a tool's schema"),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in getToolInput) (*mcp.CallToolResult, getToolOutput, error) {
		out, err := getTool(ups, in.Server, in.Tool)
		if err != nil {
			return toolError(err), getToolOutput{}, nil
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: ToolCallTool,
		Description: "Call a tool on one of the upstream MCP servers. " +
			"The gateway's own tools (" + joinNames(SystemToolNames) + ") are called directly, not through this one.",
		Annotations: &mcp.ToolAnnotations{
			Title:          "Call a tool on a server",
			ReadOnlyHint:   false,
			OpenWorldHint:  boolPtr(true),
			IdempotentHint: false,
		},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in callToolInput) (*mcp.CallToolResult, any, error) {
		result, err := callTool(ctx, ups, in)
		if err != nil {
			return toolError(err), nil, nil
		}
		return result, nil, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: ToolSearchTools,
		Description: "Search for tools across every connected server by name and " +
			"description. Every word must match.",
		Annotations: readOnly("Search tools"),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in searchToolsInput) (*mcp.CallToolResult, searchToolsOutput, error) {
		return nil, searchTools(ups, in), nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        ToolListTags,
		Description: "List the tags attached to servers, used for grouping them.",
		Annotations: readOnly("List tags"),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in listTagsInput) (*mcp.CallToolResult, listTagsOutput, error) {
		out, err := listTags(cfgs, in.Server)
		if err != nil {
			return toolError(err), listTagsOutput{}, nil
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name: ToolUpdateServerDescription,
		Description: "Change a server's description. The new text is saved to the " +
			"configuration file and shown when listing servers.",
		Annotations: &mcp.ToolAnnotations{
			Title:          "Update a server's description",
			ReadOnlyHint:   false,
			IdempotentHint: true,
		},
	}, func(_ context.Context, _ *mcp.CallToolRequest, in updateDescriptionInput) (*mcp.CallToolResult, updateDescriptionOutput, error) {
		out, err := updateDescription(cfgs, in)
		if err != nil {
			return toolError(err), updateDescriptionOutput{}, nil
		}
		return nil, out, nil
	})
}

func listServers(ups Upstreams, cfgs Configs) listServersOutput {
	cfg := cfgs.Get()
	statuses := ups.Statuses()

	out := listServersOutput{Servers: make([]ServerSummary, 0, len(statuses))}
	for _, status := range statuses {
		summary := ServerSummary{
			Name:          status.Name,
			State:         string(status.State),
			ToolCount:     status.ToolCount,
			ResourceCount: status.ResourceCount,
			Error:         status.Error,
		}
		if server, ok := cfg.MCPServers[status.Name]; ok {
			summary.Description = server.Description
			summary.Tags = server.Tags
		}
		out.Servers = append(out.Servers, summary)
	}
	return out
}

func listTools(ups Upstreams, server string) (listToolsOutput, error) {
	all := ups.Tools()
	if err := requireServer(ups, server); err != nil {
		return listToolsOutput{}, err
	}

	names := BuildNames(all)
	out := listToolsOutput{Server: server, Tools: []ToolSummary{}}
	for _, tool := range all[server] {
		if tool == nil || tool.Name == "" {
			continue
		}
		exposed, _ := names.Exposed(server, tool.Name)
		out.Tools = append(out.Tools, ToolSummary{
			Name:        tool.Name,
			Exposed:     exposed,
			Description: tool.Description,
		})
	}
	slices.SortFunc(out.Tools, func(a, b ToolSummary) int {
		return compareStrings(a.Name, b.Name)
	})
	return out, nil
}

func getTool(ups Upstreams, server, tool string) (getToolOutput, error) {
	all := ups.Tools()
	if err := requireServer(ups, server); err != nil {
		return getToolOutput{}, err
	}

	for _, candidate := range all[server] {
		if candidate == nil || candidate.Name != tool {
			continue
		}
		exposed, _ := BuildNames(all).Exposed(server, tool)
		return getToolOutput{
			Server:      server,
			Name:        candidate.Name,
			Exposed:     exposed,
			Description: candidate.Description,
			InputSchema: candidate.InputSchema,
		}, nil
	}
	return getToolOutput{}, fmt.Errorf("server %q has no tool named %q", server, tool)
}

func callTool(ctx context.Context, ups Upstreams, in callToolInput) (*mcp.CallToolResult, error) {
	// Calling a gateway tool through this one would be a needless
	// indirection, and asking for call_tool by name would recurse.
	if IsSystemTool(in.Tool) {
		return nil, fmt.Errorf("%q is one of the gateway's own tools; call it directly rather than through %s",
			in.Tool, ToolCallTool)
	}
	if err := requireServer(ups, in.Server); err != nil {
		return nil, err
	}

	var args any
	if in.Args != nil {
		args = in.Args
	}
	return ups.CallTool(ctx, in.Server, in.Tool, args)
}

func searchTools(ups Upstreams, in searchToolsInput) searchToolsOutput {
	all := ups.Tools()
	names := BuildNames(all)

	candidates := make([]Searchable, 0, names.Len())
	for _, server := range slices.Sorted(maps.Keys(all)) {
		for _, tool := range all[server] {
			if tool == nil || tool.Name == "" {
				continue
			}
			exposed, ok := names.Exposed(server, tool.Name)
			if !ok {
				continue
			}
			candidates = append(candidates, Searchable{
				Server:      server,
				Tool:        tool.Name,
				Exposed:     exposed,
				Description: tool.Description,
			})
		}
	}

	limit := in.Limit
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	return searchToolsOutput{Query: in.Query, Hits: SearchTools(in.Query, candidates, limit)}
}

func listTags(cfgs Configs, server string) (listTagsOutput, error) {
	cfg := cfgs.Get()

	if server != "" {
		entry, ok := cfg.MCPServers[server]
		if !ok {
			return listTagsOutput{}, unknownServer(server, slices.Sorted(maps.Keys(cfg.MCPServers)))
		}
		return listTagsOutput{Servers: []serverTags{{Server: server, Tags: entry.Tags}}}, nil
	}

	out := listTagsOutput{Servers: []serverTags{}}
	for _, name := range slices.Sorted(maps.Keys(cfg.MCPServers)) {
		out.Servers = append(out.Servers, serverTags{Server: name, Tags: cfg.MCPServers[name].Tags})
	}
	return out, nil
}

func updateDescription(cfgs Configs, in updateDescriptionInput) (updateDescriptionOutput, error) {
	if _, ok := cfgs.Get().MCPServers[in.Server]; !ok {
		return updateDescriptionOutput{}, unknownServer(in.Server,
			slices.Sorted(maps.Keys(cfgs.Get().MCPServers)))
	}

	if _, err := cfgs.Update(func(c *config.Config) error {
		entry := c.MCPServers[in.Server]
		entry.Description = in.Description
		c.MCPServers[in.Server] = entry
		return nil
	}); err != nil {
		return updateDescriptionOutput{}, fmt.Errorf("save the new description: %w", err)
	}

	return updateDescriptionOutput{Server: in.Server, Description: in.Description}, nil
}

// requireServer reports a usable error when a server is unknown or not
// currently connected, since those need different responses from the
// caller.
func requireServer(ups Upstreams, server string) error {
	if server == "" {
		return fmt.Errorf("no server was named")
	}
	for _, status := range ups.Statuses() {
		if status.Name != server {
			continue
		}
		if !status.Connected() {
			return fmt.Errorf("server %q is %s, so its tools are unavailable", server, status.State)
		}
		return nil
	}

	known := make([]string, 0)
	for _, status := range ups.Statuses() {
		known = append(known, status.Name)
	}
	return unknownServer(server, known)
}

func unknownServer(server string, known []string) error {
	if len(known) == 0 {
		return fmt.Errorf("no server named %q; no servers are configured", server)
	}
	return fmt.Errorf("no server named %q; the configured servers are %s", server, joinNames(known))
}

// toolError turns a failure into a result the model can read and act on,
// rather than a protocol error that it cannot see.
func toolError(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}
}

func readOnly(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		Title:          title,
		ReadOnlyHint:   true,
		IdempotentHint: true,
	}
}

func boolPtr(b bool) *bool { return &b }

func joinNames(names []string) string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = fmt.Sprintf("%q", name)
	}
	switch len(quoted) {
	case 0:
		return ""
	case 1:
		return quoted[0]
	}
	return fmt.Sprintf("%s and %s",
		joinWithCommas(quoted[:len(quoted)-1]), quoted[len(quoted)-1])
}

func joinWithCommas(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
