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
	Name  string `json:"name"`
	State string `json:"state"`

	// Title is the name the server called itself during the handshake,
	// reported when it differs from the name it is configured under.
	//
	// The configured name is chosen by whoever wrote the file and is often
	// a shorthand — "xingzuo" for a server that calls itself 星座 MCP 服务.
	// A caller deciding whether a server is worth opening has nothing else
	// to go on when no description has been recorded, and the gateway has
	// had this string since the handshake.
	Title string `json:"title,omitempty"`

	Description   string            `json:"description,omitempty"`
	Tags          map[string]string `json:"tags,omitempty"`
	ToolCount     int               `json:"toolCount"`
	ResourceCount int               `json:"resourceCount"`
	Error         string            `json:"error,omitempty"`
}

// A server nobody has described says so, and names both ways out: look at
// what it offers, or record what you concluded. Two wordings because the
// tools are in front of the reader in one case and a call away in the
// other, and a sentence that points at something absent is worse than none.
const (
	undescribedInList     = "no description has been recorded; list_tools shows what this server offers, and update_server_description saves a description for it"
	undescribedInResource = "no description has been recorded; the tools below are what this server offers, and update_server_description saves a description for it"
)

type listServersOutput struct {
	Servers []ServerSummary `json:"servers"`
}

// ===== list_tools =====

type listToolsInput struct {
	Server string `json:"server,omitempty" jsonschema:"exact name of the MCP server, as returned by list_servers. Leave it out to list the tools of every connected server at once"`
}

// ToolSummary is a tool without its full schema, which is what a model
// needs to decide whether to look closer.
type ToolSummary struct {
	Name        string `json:"name"`
	Exposed     string `json:"exposed"`
	Description string `json:"description,omitempty"`
}

// serverTools is one server's tools, used when several servers are
// reported at once.
//
// A separate type rather than [listToolsOutput] nesting itself: the SDK
// infers this tool's output schema from these Go types, and a recursive
// type infers a schema with references into its own definitions. There is
// no reason to hand clients something that shaped when the nesting only
// ever goes one level deep.
type serverTools struct {
	Server string        `json:"server"`
	Tools  []ToolSummary `json:"tools"`
}

type listToolsOutput struct {
	// Server is the one that was asked about, and is empty when every
	// server was.
	Server string `json:"server,omitempty"`

	// Tools is the answer for a single server.
	Tools []ToolSummary `json:"tools,omitempty"`

	// Servers is the answer when no server was named: one entry each,
	// rather than a flat list repeating the origin on every row.
	Servers []serverTools `json:"servers,omitempty"`
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

	// Title is the tool's display name, when it has one distinct from its
	// name.
	Title string `json:"title,omitempty"`

	// InputSchema is a map rather than `any` because the SDK generates
	// this tool's output schema from these field types, and `any` becomes
	// the JSON Schema `true`.
	//
	// `true` is a legal schema — the one that accepts everything — but the
	// TypeScript MCP SDK validates each entry under "properties" as an
	// object and rejects a boolean there. It fails the whole tools/list
	// response, not just this field, so a single `any` here makes every
	// tool the gateway offers invisible to any client built on that SDK.
	InputSchema map[string]any `json:"inputSchema,omitempty"`

	// OutputSchema says what a successful call returns, when the upstream
	// server declares one. A caller that knows it can expect structured
	// content rather than guessing from the text.
	//
	// A map for the same reason as InputSchema, and passed through the same
	// decoding, since it comes from the same arbitrary upstream JSON.
	OutputSchema map[string]any `json:"outputSchema,omitempty"`

	// Annotations are the upstream server's hints about what calling this
	// tool does — read-only, destructive, idempotent, open-world.
	//
	// This is the field whose absence cost the most: a caller weighing
	// whether a call is safe to make had nothing to weigh, while the same
	// tool published through tools/list carried the hints in full. A tool
	// reached through call_tool is no less in need of them.
	Annotations *mcp.ToolAnnotations `json:"annotations,omitempty"`
}

// ===== call_tool =====

type callToolInput struct {
	// The second sentence is there because a caller that has just been
	// told a server's name will otherwise go looking for it again. Every
	// round trip it saves is one the caller was going to spend re-reading
	// something it already had.
	Server string         `json:"server" jsonschema:"exact name of the MCP server, as returned by list_servers. If a previous list_tools or search_tools result already told you the server name, use it and call straight away rather than searching again"`
	Tool   string         `json:"tool" jsonschema:"exact name of the tool on that server, as returned by list_tools or search_tools. It does not have to be a tool that appears in this gateway's own tool list"`
	Args   map[string]any `json:"args,omitempty" jsonschema:"arguments for the tool, matching its input schema. Omit it for a tool that takes none"`
}

// ===== search_tools =====

type searchToolsInput struct {
	Query string `json:"query" jsonschema:"words to look for in tool names and descriptions. Several words widen the search: describing one thing several ways is fine, and a word that matches nothing is reported rather than emptying the result"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum number of results, default 20"`
}

type searchToolsOutput struct {
	Query string      `json:"query"`
	Hits  []SearchHit `json:"hits"`

	// Unmatched names the query terms no tool contained. It is absent when
	// every term landed somewhere, so its presence is the signal: without
	// it, an empty result is indistinguishable from "this gateway has no
	// such capability".
	Unmatched []string `json:"unmatched,omitempty"`
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
		Description: "List the tools one MCP server offers, with their descriptions. " +
			"Use the server name exactly as returned by list_servers, or leave it " +
			"out to get every connected server in one call.",
		Annotations: readOnly("List tools on a server"),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in listToolsInput) (*mcp.CallToolResult, listToolsOutput, error) {
		out, err := listTools(ups, cfgs, in.Server)
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
		out, err := getTool(ups, cfgs, in.Server, in.Tool)
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
			"description. Several words widen the search rather than narrowing it: " +
			"a tool matching more of them ranks higher, and a word that matches " +
			"nothing is reported back rather than emptying the result.",
		Annotations: readOnly("Search tools"),
	}, func(_ context.Context, _ *mcp.CallToolRequest, in searchToolsInput) (*mcp.CallToolResult, searchToolsOutput, error) {
		return nil, searchTools(ups, cfgs, in), nil
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
		if status.ServerName != status.Name {
			summary.Title = status.ServerName
		}
		if server, ok := cfg.MCPServers[status.Name]; ok {
			summary.Description = server.Description
			summary.Tags = server.Tags
		}
		// Only a server that is actually up gets the invitation: telling a
		// caller to run list_tools against a failed server sends it to an
		// error, and for that server the state and the error are the
		// description.
		if summary.Description == "" && status.Connected() {
			summary.Description = undescribedInList
		}
		out.Servers = append(out.Servers, summary)
	}
	return out
}

func listTools(ups Upstreams, cfgs Configs, server string) (listToolsOutput, error) {
	all := ups.Tools()
	names := PublishedNames(all, cfgs.Get())

	// No server named means every connected one. Understanding an
	// installation otherwise costs one call per server — and the caller has
	// to list the servers first just to know how many calls that is.
	if server == "" {
		out := listToolsOutput{Servers: []serverTools{}}
		for _, status := range ups.Statuses() {
			if !status.Connected() {
				continue
			}
			out.Servers = append(out.Servers, serverTools{
				Server: status.Name,
				Tools:  summarize(all[status.Name], status.Name, names),
			})
		}
		return out, nil
	}

	if err := requireServer(ups, server); err != nil {
		return listToolsOutput{}, err
	}
	return listToolsOutput{Server: server, Tools: summarize(all[server], server, names)}, nil
}

// summarize turns one server's tools into the reported form.
//
// Every tool the server offers is included, exposed or not: this is how a
// model discovers what is available, and the whole point of exposing
// little is that discovery still reaches everything.
func summarize(tools []*mcp.Tool, server string, names NameMap) []ToolSummary {
	out := make([]ToolSummary, 0, len(tools))
	for _, tool := range tools {
		if tool == nil || tool.Name == "" {
			continue
		}
		exposed, _ := names.Exposed(server, tool.Name)
		out = append(out, ToolSummary{
			Name:        tool.Name,
			Exposed:     exposed,
			Description: tool.Description,
		})
	}
	slices.SortFunc(out, func(a, b ToolSummary) int {
		return compareStrings(a.Name, b.Name)
	})
	return out
}

func getTool(ups Upstreams, cfgs Configs, server, tool string) (getToolOutput, error) {
	all := ups.Tools()
	if err := requireServer(ups, server); err != nil {
		return getToolOutput{}, withSystemToolHint(err, tool)
	}

	for _, candidate := range all[server] {
		if candidate == nil || candidate.Name != tool {
			continue
		}
		exposed, _ := PublishedNames(all, cfgs.Get()).Exposed(server, tool)

		// The declared type says this is an object, so an upstream that
		// published something else must not be forwarded verbatim: the
		// SDK validates structured results against the schema it
		// generated, and a mismatch would fail the call.
		schema, ok := decodeObjectSchema(candidate.InputSchema)
		if !ok {
			schema = emptyObjectSchema()
		}

		out := getToolOutput{
			Server:      server,
			Name:        candidate.Name,
			Exposed:     exposed,
			Description: candidate.Description,
			Title:       candidate.Title,
			InputSchema: schema,
			Annotations: candidate.Annotations,
		}
		// An output schema is optional upstream, so an unusable one is
		// simply not reported — unlike the input schema, which a caller
		// needs in order to build a call at all and therefore gets an empty
		// object rather than nothing.
		if candidate.OutputSchema != nil {
			if declared, ok := decodeObjectSchema(candidate.OutputSchema); ok {
				out.OutputSchema = declared
			}
		}
		return out, nil
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

func searchTools(ups Upstreams, cfgs Configs, in searchToolsInput) searchToolsOutput {
	all := ups.Tools()
	names := PublishedNames(all, cfgs.Get())

	candidates := make([]Searchable, 0, len(all))
	for _, server := range slices.Sorted(maps.Keys(all)) {
		for _, tool := range all[server] {
			if tool == nil || tool.Name == "" {
				continue
			}
			// A tool with no exposed name is still a search result. Search
			// is discovery, and leaving out everything unexposed would make
			// it useless on an installation that exposes little — which is
			// the ordinary case.
			exposed, _ := names.Exposed(server, tool.Name)
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
	return searchToolsOutput{
		Query:     in.Query,
		Hits:      SearchTools(in.Query, candidates, limit),
		Unmatched: UnmatchedTerms(in.Query, candidates),
	}
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

// withSystemToolHint supplies the half of the answer the caller actually
// needed, when it asked some server for one of the gateway's own tools.
//
// Listing the configured servers tells a caller that its guess was wrong.
// It does not tell it the thing it is looking for needs no server at all —
// so a caller that guessed the gateway's own name learns nothing it can
// act on, and goes on guessing. The check is on the tool rather than on
// the name guessed at, because the gateway cannot know what a client calls
// it, but it does know which tools are its own.
//
// Only reached once the server has already been rejected: an upstream
// server is free to have a tool called list_servers, and asking that
// server for it must still work.
func withSystemToolHint(err error, tool string) error {
	if !IsSystemTool(tool) {
		return err
	}
	return fmt.Errorf("%w; note that %q is one of this gateway's own tools — it is already in the tool list, "+
		"call it directly and pass no server name", err, tool)
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
