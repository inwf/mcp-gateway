package gateway_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mcphub/internal/config"
	"mcphub/internal/gateway"
	"mcphub/internal/upstream"
)

// fakeUpstreams stands in for the connection manager. The system tools
// only read a handful of things from it, and faking those lets every
// error path be provoked without running child processes.
type fakeUpstreams struct {
	statuses  []upstream.Status
	tools     map[string][]*mcp.Tool
	resources map[string][]*mcp.Resource

	// calls records what was forwarded, and callErr forces a failure.
	calls   []string
	callErr error
	result  *mcp.CallToolResult
}

func (f *fakeUpstreams) Statuses() []upstream.Status           { return f.statuses }
func (f *fakeUpstreams) Tools() map[string][]*mcp.Tool         { return f.tools }
func (f *fakeUpstreams) Resources() map[string][]*mcp.Resource { return f.resources }

func (f *fakeUpstreams) CallTool(_ context.Context, server, tool string, args any) (*mcp.CallToolResult, error) {
	f.calls = append(f.calls, server+"/"+tool)
	if f.callErr != nil {
		return nil, f.callErr
	}
	if f.result != nil {
		return f.result, nil
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("called %s/%s with %v", server, tool, args)}},
	}, nil
}

func (f *fakeUpstreams) ReadResource(_ context.Context, server, uri string) (*mcp.ReadResourceResult, error) {
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{URI: uri, Text: "from " + server}},
	}, nil
}

// gatewayFixture starts a real MCP server with the system tools
// registered and returns a connected client session. Going through the
// protocol means the tests cover schema inference and result shapes, not
// just the handler bodies.
func gatewayFixture(t *testing.T, ups gateway.Upstreams, cfgs gateway.Configs) *mcp.ClientSession {
	t.Helper()

	server := mcp.NewServer(&mcp.Implementation{Name: "mcphub", Version: "test"}, nil)
	gateway.RegisterSystemTools(server, ups, cfgs)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect the server: %v", err)
	}
	t.Cleanup(func() { serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect the client: %v", err)
	}
	t.Cleanup(func() { session.Close() })

	return session
}

// configFixture builds a real configuration manager backed by a
// temporary file, so that tools which write configuration are tested
// against the thing that actually persists it.
func configFixture(t *testing.T, servers map[string]config.MCPServer) *config.Manager {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := config.Default()
	cfg.MCPServers = servers
	if err := config.Save(path, cfg); err != nil {
		t.Fatalf("save the configuration: %v", err)
	}

	m, err := config.NewManager(path)
	if err != nil {
		t.Fatalf("open the configuration: %v", err)
	}
	return m
}

// callSystemTool invokes one gateway tool and returns the result.
func callSystemTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	return result
}

// structured decodes a tool's structured output into target.
func structured(t *testing.T, result *mcp.CallToolResult, target any) {
	t.Helper()
	if result.IsError {
		t.Fatalf("the tool reported an error: %s", resultText(result))
	}
	if result.StructuredContent == nil {
		t.Fatalf("the tool returned no structured content; text was %q", resultText(result))
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal the structured content: %v", err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
}

func resultText(result *mcp.CallToolResult) string {
	var b strings.Builder
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			b.WriteString(text.Text)
		}
	}
	return b.String()
}

// twoServers is the fixture most tests use: one connected server with
// two tools, one failed server.
func twoServers() *fakeUpstreams {
	return &fakeUpstreams{
		statuses: []upstream.Status{
			{Name: "files", State: upstream.StateConnected, ToolCount: 2, ResourceCount: 1},
			{Name: "broken", State: upstream.StateFailed, Error: "command not found"},
		},
		tools: map[string][]*mcp.Tool{
			"files": {
				{Name: "read", Description: "read a file from disk",
					InputSchema: map[string]any{"type": "object",
						"properties": map[string]any{"path": map[string]any{"type": "string"}}}},
				{Name: "write", Description: "write a file to disk"},
			},
		},
		resources: map[string][]*mcp.Resource{
			"files": {{URI: "file:///tmp/a", Name: "a"}},
		},
	}
}

func twoServersConfig(t *testing.T) *config.Manager {
	return configFixture(t, map[string]config.MCPServer{
		"files": {
			Transport: config.TransportStdio, Command: "npx", Enabled: true,
			Timeout: time.Minute, Description: "local filesystem",
			Tags: map[string]string{"env": "dev", "kind": "fs"},
		},
		"broken": {
			Transport: config.TransportStdio, Command: "nope", Enabled: true,
			Timeout: time.Minute,
		},
	})
}

func TestEverySystemToolIsRegistered(t *testing.T) {
	session := gatewayFixture(t, twoServers(), twoServersConfig(t))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	found := map[string]bool{}
	for _, tool := range listed.Tools {
		found[tool.Name] = true
	}
	for _, want := range gateway.SystemToolNames {
		if !found[want] {
			t.Errorf("tool %q is not registered; have %v", want, slices.Sorted(maps.Keys(found)))
		}
	}
}

// The schemas are inferred from Go types, so this checks the inference
// produced something a client can actually use.
func TestSystemToolSchemasAreUsable(t *testing.T) {
	session := gatewayFixture(t, twoServers(), twoServersConfig(t))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}

	for _, tool := range listed.Tools {
		if tool.Description == "" {
			t.Errorf("tool %q has no description", tool.Name)
		}
		if tool.InputSchema == nil {
			t.Errorf("tool %q has no input schema", tool.Name)
			continue
		}
		data, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Errorf("tool %q has an unmarshalable schema: %v", tool.Name, err)
			continue
		}
		var schema map[string]any
		if err := json.Unmarshal(data, &schema); err != nil {
			t.Errorf("tool %q schema is not an object: %v", tool.Name, err)
			continue
		}
		if schema["type"] != "object" {
			t.Errorf("tool %q schema type = %v, want object", tool.Name, schema["type"])
		}
	}
}

func TestListServers(t *testing.T) {
	session := gatewayFixture(t, twoServers(), twoServersConfig(t))

	var out struct {
		Servers []gateway.ServerSummary `json:"servers"`
	}
	structured(t, callSystemTool(t, session, gateway.ToolListServers, nil), &out)

	if len(out.Servers) != 2 {
		t.Fatalf("listed %d servers, want 2: %+v", len(out.Servers), out.Servers)
	}

	byName := map[string]gateway.ServerSummary{}
	for _, s := range out.Servers {
		byName[s.Name] = s
	}

	files := byName["files"]
	if files.State != string(upstream.StateConnected) {
		t.Errorf("files state = %q, want connected", files.State)
	}
	if files.Description != "local filesystem" {
		t.Errorf("files description = %q, want it from the configuration", files.Description)
	}
	if files.Tags["env"] != "dev" {
		t.Errorf("files tags = %v, want env=dev", files.Tags)
	}
	if files.ToolCount != 2 {
		t.Errorf("files toolCount = %d, want 2", files.ToolCount)
	}

	// A failed server is still listed, with its explanation: knowing it
	// exists and why it is down is the point.
	broken := byName["broken"]
	if broken.State != string(upstream.StateFailed) {
		t.Errorf("broken state = %q, want failed", broken.State)
	}
	if broken.Error == "" {
		t.Error("broken carries no error message")
	}
}

func TestListTools(t *testing.T) {
	session := gatewayFixture(t, twoServers(), twoServersConfig(t))

	var out struct {
		Server string                `json:"server"`
		Tools  []gateway.ToolSummary `json:"tools"`
	}
	structured(t, callSystemTool(t, session, gateway.ToolListTools,
		map[string]any{"server": "files"}), &out)

	if out.Server != "files" {
		t.Errorf("server = %q, want files", out.Server)
	}
	if len(out.Tools) != 2 {
		t.Fatalf("listed %d tools, want 2: %+v", len(out.Tools), out.Tools)
	}
	// Sorted, so a repeated call reads the same.
	if out.Tools[0].Name != "read" || out.Tools[1].Name != "write" {
		t.Errorf("tools = %+v, want read then write", out.Tools)
	}
	// The exposed name is what a client would call directly.
	if out.Tools[0].Exposed != "files_read" {
		t.Errorf("exposed = %q, want files_read", out.Tools[0].Exposed)
	}
}

func TestListToolsOnAnUnknownServer(t *testing.T) {
	session := gatewayFixture(t, twoServers(), twoServersConfig(t))

	result := callSystemTool(t, session, gateway.ToolListTools,
		map[string]any{"server": "ghost"})

	if !result.IsError {
		t.Fatal("listing tools on a server that does not exist succeeded")
	}
	text := resultText(result)
	if !strings.Contains(text, "ghost") {
		t.Errorf("error %q does not name the server that was asked for", text)
	}
	// Telling the model what does exist is what lets it recover.
	if !strings.Contains(text, "files") {
		t.Errorf("error %q does not list the servers that do exist", text)
	}
}

// A configured but disconnected server needs a different answer from one
// that does not exist: the fix is to connect it, not to rename it.
func TestListToolsOnADisconnectedServer(t *testing.T) {
	session := gatewayFixture(t, twoServers(), twoServersConfig(t))

	result := callSystemTool(t, session, gateway.ToolListTools,
		map[string]any{"server": "broken"})

	if !result.IsError {
		t.Fatal("listing tools on a failed server succeeded")
	}
	if text := resultText(result); !strings.Contains(text, "failed") {
		t.Errorf("error %q does not say the server is not connected", text)
	}
}

func TestGetTool(t *testing.T) {
	session := gatewayFixture(t, twoServers(), twoServersConfig(t))

	var out struct {
		Server      string `json:"server"`
		Name        string `json:"name"`
		Exposed     string `json:"exposed"`
		Description string `json:"description"`
		InputSchema any    `json:"inputSchema"`
	}
	structured(t, callSystemTool(t, session, gateway.ToolGetTool,
		map[string]any{"server": "files", "tool": "read"}), &out)

	if out.Name != "read" || out.Server != "files" {
		t.Errorf("got %s/%s, want files/read", out.Server, out.Name)
	}
	if out.Exposed != "files_read" {
		t.Errorf("exposed = %q, want files_read", out.Exposed)
	}
	// The schema is the reason to call this tool at all.
	if out.InputSchema == nil {
		t.Fatal("no input schema was returned")
	}
	schema, ok := out.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("input schema is %T, want an object", out.InputSchema)
	}
	if schema["type"] != "object" {
		t.Errorf("schema type = %v, want object", schema["type"])
	}
}

func TestGetToolThatDoesNotExist(t *testing.T) {
	session := gatewayFixture(t, twoServers(), twoServersConfig(t))

	result := callSystemTool(t, session, gateway.ToolGetTool,
		map[string]any{"server": "files", "tool": "teleport"})

	if !result.IsError {
		t.Fatal("getting a tool that does not exist succeeded")
	}
	if text := resultText(result); !strings.Contains(text, "teleport") {
		t.Errorf("error %q does not name the tool", text)
	}
}

func TestCallTool(t *testing.T) {
	ups := twoServers()
	session := gatewayFixture(t, ups, twoServersConfig(t))

	result := callSystemTool(t, session, gateway.ToolCallTool, map[string]any{
		"server": "files",
		"tool":   "read",
		"args":   map[string]any{"path": "/tmp/x"},
	})

	if result.IsError {
		t.Fatalf("call failed: %s", resultText(result))
	}
	if want := []string{"files/read"}; !slices.Equal(ups.calls, want) {
		t.Errorf("forwarded %v, want %v", ups.calls, want)
	}
	if text := resultText(result); !strings.Contains(text, "/tmp/x") {
		t.Errorf("result %q does not show the arguments reaching the tool", text)
	}
}

func TestCallToolWithoutArguments(t *testing.T) {
	ups := twoServers()
	session := gatewayFixture(t, ups, twoServersConfig(t))

	result := callSystemTool(t, session, gateway.ToolCallTool, map[string]any{
		"server": "files",
		"tool":   "write",
	})

	if result.IsError {
		t.Fatalf("call failed: %s", resultText(result))
	}
	if want := []string{"files/write"}; !slices.Equal(ups.calls, want) {
		t.Errorf("forwarded %v, want %v", ups.calls, want)
	}
}

// Reaching a gateway tool through call_tool would be a pointless
// indirection, and asking for call_tool itself would recurse.
func TestCallToolRefusesTheGatewaysOwnTools(t *testing.T) {
	ups := twoServers()
	session := gatewayFixture(t, ups, twoServersConfig(t))

	for _, name := range gateway.SystemToolNames {
		result := callSystemTool(t, session, gateway.ToolCallTool, map[string]any{
			"server": "files",
			"tool":   name,
		})

		if !result.IsError {
			t.Errorf("call_tool accepted the gateway tool %q", name)
			continue
		}
		if text := resultText(result); !strings.Contains(text, "directly") {
			t.Errorf("error for %q was %q, want it to say to call the tool directly", name, text)
		}
	}
	if len(ups.calls) != 0 {
		t.Errorf("forwarded %v upstream, want nothing", ups.calls)
	}
}

func TestCallToolOnAnUnknownServer(t *testing.T) {
	ups := twoServers()
	session := gatewayFixture(t, ups, twoServersConfig(t))

	result := callSystemTool(t, session, gateway.ToolCallTool, map[string]any{
		"server": "ghost", "tool": "read",
	})

	if !result.IsError {
		t.Fatal("call_tool accepted a server that does not exist")
	}
	if len(ups.calls) != 0 {
		t.Errorf("forwarded %v upstream, want nothing", ups.calls)
	}
}

// An upstream failure has to reach the model as something it can read,
// not vanish into a protocol error.
func TestCallToolReportsAnUpstreamFailure(t *testing.T) {
	ups := twoServers()
	ups.callErr = errors.New("the disk is on fire")
	session := gatewayFixture(t, ups, twoServersConfig(t))

	result := callSystemTool(t, session, gateway.ToolCallTool, map[string]any{
		"server": "files", "tool": "read",
	})

	if !result.IsError {
		t.Fatal("an upstream failure was reported as success")
	}
	if text := resultText(result); !strings.Contains(text, "disk is on fire") {
		t.Errorf("error %q does not carry the upstream explanation", text)
	}
}

func TestSearchTools(t *testing.T) {
	session := gatewayFixture(t, twoServers(), twoServersConfig(t))

	var out struct {
		Query string              `json:"query"`
		Hits  []gateway.SearchHit `json:"hits"`
	}
	structured(t, callSystemTool(t, session, gateway.ToolSearchTools,
		map[string]any{"query": "read"}), &out)

	if len(out.Hits) == 0 {
		t.Fatal("no hits for a query naming an existing tool")
	}
	if out.Hits[0].Tool != "read" {
		t.Errorf("best hit = %+v, want the read tool", out.Hits[0])
	}
	if out.Hits[0].Server != "files" {
		t.Errorf("best hit server = %q, want files", out.Hits[0].Server)
	}
}

func TestSearchToolsRespectsTheLimit(t *testing.T) {
	session := gatewayFixture(t, twoServers(), twoServersConfig(t))

	var out struct {
		Hits []gateway.SearchHit `json:"hits"`
	}
	structured(t, callSystemTool(t, session, gateway.ToolSearchTools,
		map[string]any{"query": "disk", "limit": 1}), &out)

	if len(out.Hits) != 1 {
		t.Errorf("returned %d hits, want 1", len(out.Hits))
	}
}

func TestSearchToolsFindsNothing(t *testing.T) {
	session := gatewayFixture(t, twoServers(), twoServersConfig(t))

	var out struct {
		Hits []gateway.SearchHit `json:"hits"`
	}
	structured(t, callSystemTool(t, session, gateway.ToolSearchTools,
		map[string]any{"query": "kubernetes"}), &out)

	if len(out.Hits) != 0 {
		t.Errorf("returned %+v, want nothing", out.Hits)
	}
}

func TestListTagsForEveryServer(t *testing.T) {
	session := gatewayFixture(t, twoServers(), twoServersConfig(t))

	var out struct {
		Servers []struct {
			Server string            `json:"server"`
			Tags   map[string]string `json:"tags"`
		} `json:"servers"`
	}
	structured(t, callSystemTool(t, session, gateway.ToolListTags, nil), &out)

	if len(out.Servers) != 2 {
		t.Fatalf("listed %d servers, want 2: %+v", len(out.Servers), out.Servers)
	}
	// Sorted, so the answer is stable.
	if out.Servers[0].Server != "broken" || out.Servers[1].Server != "files" {
		t.Errorf("servers = %+v, want them ordered by name", out.Servers)
	}
	if out.Servers[1].Tags["kind"] != "fs" {
		t.Errorf("files tags = %v, want kind=fs", out.Servers[1].Tags)
	}
}

func TestListTagsForOneServer(t *testing.T) {
	session := gatewayFixture(t, twoServers(), twoServersConfig(t))

	var out struct {
		Servers []struct {
			Server string            `json:"server"`
			Tags   map[string]string `json:"tags"`
		} `json:"servers"`
	}
	structured(t, callSystemTool(t, session, gateway.ToolListTags,
		map[string]any{"server": "files"}), &out)

	if len(out.Servers) != 1 || out.Servers[0].Server != "files" {
		t.Fatalf("got %+v, want only files", out.Servers)
	}
}

func TestListTagsForAnUnknownServer(t *testing.T) {
	session := gatewayFixture(t, twoServers(), twoServersConfig(t))

	result := callSystemTool(t, session, gateway.ToolListTags,
		map[string]any{"server": "ghost"})

	if !result.IsError {
		t.Fatal("listing tags for a server that does not exist succeeded")
	}
}

// The description is written to the configuration file, so the change
// has to survive a reload rather than only living in memory.
func TestUpdateServerDescriptionPersists(t *testing.T) {
	cfgs := twoServersConfig(t)
	session := gatewayFixture(t, twoServers(), cfgs)

	var out struct {
		Server      string `json:"server"`
		Description string `json:"description"`
	}
	structured(t, callSystemTool(t, session, gateway.ToolUpdateServerDescription,
		map[string]any{"server": "files", "description": "the good filesystem"}), &out)

	if out.Description != "the good filesystem" {
		t.Errorf("returned description = %q", out.Description)
	}
	if got := cfgs.Get().MCPServers["files"].Description; got != "the good filesystem" {
		t.Errorf("in-memory description = %q, want the new text", got)
	}

	reloaded, err := config.Load(cfgs.Path())
	if err != nil {
		t.Fatalf("reload the configuration: %v", err)
	}
	if got := reloaded.MCPServers["files"].Description; got != "the good filesystem" {
		t.Errorf("persisted description = %q, want the new text", got)
	}
}

// Changing a description must not disturb anything else about the server.
func TestUpdateServerDescriptionLeavesTheRestAlone(t *testing.T) {
	cfgs := twoServersConfig(t)
	before := cfgs.Get().MCPServers["files"]
	session := gatewayFixture(t, twoServers(), cfgs)

	callSystemTool(t, session, gateway.ToolUpdateServerDescription,
		map[string]any{"server": "files", "description": "new text"})

	after := cfgs.Get().MCPServers["files"]
	if after.Command != before.Command || after.Transport != before.Transport {
		t.Errorf("command or transport changed: %+v then %+v", before, after)
	}
	if after.Timeout != before.Timeout {
		t.Errorf("timeout changed from %v to %v", before.Timeout, after.Timeout)
	}
	if len(after.Tags) != len(before.Tags) {
		t.Errorf("tags changed from %v to %v", before.Tags, after.Tags)
	}
}

func TestUpdateDescriptionOfAnUnknownServer(t *testing.T) {
	cfgs := twoServersConfig(t)
	session := gatewayFixture(t, twoServers(), cfgs)

	result := callSystemTool(t, session, gateway.ToolUpdateServerDescription,
		map[string]any{"server": "ghost", "description": "nope"})

	if !result.IsError {
		t.Fatal("updating a server that does not exist succeeded")
	}
	if _, appeared := cfgs.Get().MCPServers["ghost"]; appeared {
		t.Error("the failed update created the server")
	}
}

func TestIsSystemTool(t *testing.T) {
	for _, name := range gateway.SystemToolNames {
		if !gateway.IsSystemTool(name) {
			t.Errorf("IsSystemTool(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"", "read", "files_read", "list_server"} {
		if gateway.IsSystemTool(name) {
			t.Errorf("IsSystemTool(%q) = true, want false", name)
		}
	}
}

// With nothing configured the tools must still answer, so a fresh
// installation is explorable rather than broken.
func TestSystemToolsWithNoServers(t *testing.T) {
	empty := &fakeUpstreams{}
	session := gatewayFixture(t, empty, configFixture(t, map[string]config.MCPServer{}))

	var servers struct {
		Servers []gateway.ServerSummary `json:"servers"`
	}
	structured(t, callSystemTool(t, session, gateway.ToolListServers, nil), &servers)
	if len(servers.Servers) != 0 {
		t.Errorf("listed %+v, want nothing", servers.Servers)
	}

	var hits struct {
		Hits []gateway.SearchHit `json:"hits"`
	}
	structured(t, callSystemTool(t, session, gateway.ToolSearchTools,
		map[string]any{"query": "anything"}), &hits)
	if len(hits.Hits) != 0 {
		t.Errorf("search returned %+v, want nothing", hits.Hits)
	}

	result := callSystemTool(t, session, gateway.ToolListTools, map[string]any{"server": "any"})
	if !result.IsError {
		t.Error("listing tools with no servers configured succeeded")
	}
	if text := resultText(result); !strings.Contains(text, "no servers are configured") {
		t.Errorf("error %q does not explain that nothing is configured", text)
	}
}
