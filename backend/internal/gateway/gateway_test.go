package gateway_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mcphub/internal/config"
	"mcphub/internal/events"
	"mcphub/internal/gateway"
	"mcphub/internal/guide"
	"mcphub/internal/upstream"
)

// gatewayOn builds a gateway over the given upstreams and serves it on a
// real HTTP listener, returning the URL and the gateway.
func gatewayOn(t *testing.T, ups gateway.Upstreams, adjust func(*gateway.Options)) (string, *gateway.Gateway) {
	t.Helper()

	opts := gateway.Options{
		Version:   "test",
		Upstreams: ups,
		// Forwarding is what most of these tests are about, so the default
		// here exposes everything on offer. A test about exposure itself
		// overrides this.
		Configs: exposingEverything(t, ups),
		Gateway: config.Default().Gateway,
	}
	if adjust != nil {
		adjust(&opts)
	}

	g := gateway.New(opts)
	g.Sync()

	server := httptest.NewServer(g.Handler())
	t.Cleanup(server.Close)

	return server.URL, g
}

// clientOn connects an MCP client to a gateway over streamable HTTP.
func clientOn(t *testing.T, url string, header http.Header) *mcp.ClientSession {
	t.Helper()

	transport := &mcp.StreamableClientTransport{Endpoint: url}
	if header != nil {
		transport.HTTPClient = &http.Client{Transport: headerRoundTripper{header: header}}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)

	client := mcp.NewClient(&mcp.Implementation{Name: "probe", Version: "1.0"}, nil)
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("connect to the gateway: %v", err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

// headerRoundTripper adds fixed headers to every request, which is how
// the session mode and User-Agent are exercised end to end.
type headerRoundTripper struct{ header http.Header }

func (rt headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for name, values := range rt.header {
		for _, value := range values {
			req.Header.Set(name, value)
		}
	}
	return http.DefaultTransport.RoundTrip(req)
}

func listedToolNames(t *testing.T, session *mcp.ClientSession) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	names := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	return names
}

func TestGatewayExposesSystemAndForwardedTools(t *testing.T) {
	url, _ := gatewayOn(t, twoServers(), nil)
	session := clientOn(t, url, nil)

	names := listedToolNames(t, session)

	for _, want := range gateway.SystemToolNames {
		if !slices.Contains(names, want) {
			t.Errorf("system tool %q is missing from %v", want, names)
		}
	}
	for _, want := range []string{"files_read", "files_write"} {
		if !slices.Contains(names, want) {
			t.Errorf("forwarded tool %q is missing from %v", want, names)
		}
	}
}

// The handshake is the only thing every client reads before it has called
// anything, so it is the only place that can explain the shape of this
// gateway to a caller who will otherwise see seven tools and conclude the
// upstream ones do not exist.
//
// The names are read out of the constant rather than written down here.
// The text this replaced named three of the seven tools and omitted
// call_tool — the one that runs anything — and no test noticed, because
// there was nothing comparing the prose against the tool set.
func TestTheHandshakeExplainsEveryGatewayTool(t *testing.T) {
	url, _ := gatewayOn(t, twoServers(), nil)
	session := clientOn(t, url, nil)

	init := session.InitializeResult()
	if init == nil {
		t.Fatal("the gateway returned no initialize result")
	}
	said := init.Instructions
	if said == "" {
		t.Fatal("the gateway told the client nothing at all")
	}

	for _, name := range gateway.SystemToolNames {
		if !strings.Contains(said, name) {
			t.Errorf("the handshake never mentions %q:\n%s", name, said)
		}
	}

	// The guide is the long version. A resource nothing points at is a
	// resource nobody reads.
	if !strings.Contains(said, gateway.GuideResourceURI) {
		t.Errorf("the handshake never points at %s:\n%s", gateway.GuideResourceURI, said)
	}
}

// A tool must not be described in less detail just because nobody exposed
// it.
//
// An exposed tool travels into tools/list as a whole copy of what the
// upstream published, annotations included. An unexposed one is only ever
// seen through get_tool, and get_tool used to report the description and the
// input schema and drop the rest. So the same tool answered two different
// questions depending on a setting that has nothing to do with what it does:
// readOnlyHint, which is how a caller judges whether a call is safe, was
// there or not there by accident.
//
// The output schema is the deliberate exception — see the bottom of this
// test and getToolOutput for why.
func TestGetToolReportsAsMuchAsTheToolListDoes(t *testing.T) {
	rich := func() *fakeUpstreams {
		return &fakeUpstreams{
			statuses: []upstream.Status{{Name: "files", State: upstream.StateConnected, ToolCount: 1}},
			tools: map[string][]*mcp.Tool{
				"files": {{
					Name:        "read",
					Title:       "Read a file",
					Description: "read a file from disk",
					InputSchema: map[string]any{"type": "object"},
					OutputSchema: map[string]any{
						"type":       "object",
						"properties": map[string]any{"text": map[string]any{"type": "string"}},
					},
					Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
				}},
			},
		}
	}

	// Exposed: the whole thing reaches tools/list, which is the standard
	// the other path has to meet.
	url, _ := gatewayOn(t, rich(), nil)
	session := clientOn(t, url, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var forwarded *mcp.Tool
	for _, tool := range listed.Tools {
		if tool.Name == "files_read" {
			forwarded = tool
		}
	}
	if forwarded == nil {
		t.Fatal("the exposed tool is not in the tool list")
	}
	if forwarded.Annotations == nil || !forwarded.Annotations.ReadOnlyHint {
		t.Fatalf("the exposed tool lost its annotations: %+v", forwarded.Annotations)
	}

	// Unexposed: nothing about it is in the tool list, and get_tool is the
	// only way to learn anything — so it has to be the whole thing.
	hidden, _ := gatewayOn(t, rich(), func(o *gateway.Options) {
		o.Configs = exposing(t, map[string][]string{"files": {}})
	})
	hiddenSession := clientOn(t, hidden, nil)

	if names := listedToolNames(t, hiddenSession); slices.Contains(names, "files_read") {
		t.Fatalf("the tool was meant to be unexposed, but %v", names)
	}

	var described struct {
		Title        string               `json:"title"`
		Exposed      string               `json:"exposed"`
		InputSchema  map[string]any       `json:"inputSchema"`
		OutputSchema map[string]any       `json:"outputSchema"`
		Annotations  *mcp.ToolAnnotations `json:"annotations"`
	}

	structured(t, callSystemTool(t, hiddenSession, gateway.ToolGetTool,
		map[string]any{"server": "files", "tool": "read"}), &described)

	if described.Exposed != "" {
		t.Errorf("exposed = %q, want none for a tool nobody exposed", described.Exposed)
	}
	if described.Annotations == nil || !described.Annotations.ReadOnlyHint {
		t.Errorf("annotations = %+v, want the upstream server's own", described.Annotations)
	}
	if described.Title != "Read a file" {
		t.Errorf("title = %q, want the upstream server's own", described.Title)
	}
	// The output schema is deliberately not reported, even though the
	// gateway holds it: see getToolOutput for the measurement behind that.
	// Asserted so that adding it back has to be a decision rather than a
	// tidy-up.
	if described.OutputSchema != nil {
		t.Errorf("outputSchema = %v, want it left out", described.OutputSchema)
	}
	if described.InputSchema == nil {
		t.Error("the input schema was dropped")
	}
}

// The gateway answers about itself under its own name.
//
// A caller that wants call_tool's schema has to name a server, and the only
// server it can name is this one. The old project made this a convention
// across its whole codebase; dropping it left the one question a new client
// always asks with no way to be asked.
func TestTheGatewayDescribesItsOwnTools(t *testing.T) {
	url, _ := gatewayOn(t, twoServers(), nil)
	session := clientOn(t, url, nil)

	var listed struct {
		Server string                `json:"server"`
		Tools  []gateway.ToolSummary `json:"tools"`
	}
	structured(t, callSystemTool(t, session, gateway.ToolListTools,
		map[string]any{"server": gateway.Name}), &listed)

	if listed.Server != gateway.Name {
		t.Errorf("server = %q, want %q", listed.Server, gateway.Name)
	}
	if len(listed.Tools) != len(gateway.SystemToolNames) {
		t.Fatalf("listed %d of its own tools, want all %d: %+v",
			len(listed.Tools), len(gateway.SystemToolNames), listed.Tools)
	}
	for _, tool := range listed.Tools {
		if !gateway.IsSystemTool(tool.Name) {
			t.Errorf("%q is not one of the gateway's tools", tool.Name)
		}
		// These are in tools/list under their own name, and that is how they
		// are called — so that is what they are exposed as.
		if tool.Exposed != tool.Name {
			t.Errorf("%q is exposed as %q, want its own name", tool.Name, tool.Exposed)
		}
		if tool.Description == "" {
			t.Errorf("%q has no description", tool.Name)
		}
	}
}

// The schema has to be the real one. A hand-written copy would describe the
// Go handlers as they were on the day somebody typed it, so this compares
// the two paths a client can reach the same schema by.
func TestTheGatewaysOwnSchemaMatchesTheOneItPublishes(t *testing.T) {
	url, _ := gatewayOn(t, twoServers(), nil)
	session := clientOn(t, url, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	published := map[string]*mcp.Tool{}
	for _, tool := range listed.Tools {
		published[tool.Name] = tool
	}

	for _, name := range gateway.SystemToolNames {
		var described struct {
			Server      string         `json:"server"`
			Name        string         `json:"name"`
			InputSchema map[string]any `json:"inputSchema"`
		}
		structured(t, callSystemTool(t, session, gateway.ToolGetTool,
			map[string]any{"server": gateway.Name, "tool": name}), &described)

		if described.Name != name {
			t.Errorf("get_tool(%s) named %q", name, described.Name)
		}
		want, ok := published[name]
		if !ok {
			t.Fatalf("%q is not in the published tool list", name)
		}
		if !sameJSON(t, described.InputSchema, want.InputSchema) {
			t.Errorf("%s: get_tool reports a different input schema than tools/list\n get_tool:  %s\n tools/list: %s",
				name, mustJSON(t, described.InputSchema), mustJSON(t, want.InputSchema))
		}
	}
}

func TestAskingTheGatewayForAToolItDoesNotHave(t *testing.T) {
	url, _ := gatewayOn(t, twoServers(), nil)
	session := clientOn(t, url, nil)

	result := callSystemTool(t, session, gateway.ToolGetTool,
		map[string]any{"server": gateway.Name, "tool": "teleport"})

	if !result.IsError {
		t.Fatal("asking the gateway for a tool it does not have succeeded")
	}
	// And it says what it does have, since the caller is clearly looking for
	// one of them.
	if text := resultText(result); !strings.Contains(text, gateway.ToolCallTool) {
		t.Errorf("error %q does not name the gateway's own tools", text)
	}
}

// It is a name list_tools and get_tool accept, so it is a name a caller
// will try here too — and "no server named mcphub" would be a strange thing
// to hear from mcphub.
func TestTheGatewayCannotBeGivenADescription(t *testing.T) {
	url, _ := gatewayOn(t, twoServers(), nil)
	session := clientOn(t, url, nil)

	result := callSystemTool(t, session, gateway.ToolUpdateServerDescription,
		map[string]any{"server": gateway.Name, "description": "the gateway itself"})

	if !result.IsError {
		t.Fatal("the gateway accepted a description of itself")
	}
	text := resultText(result)
	if strings.Contains(text, "no server named") {
		t.Errorf("error %q denies that the gateway exists", text)
	}
	if !strings.Contains(text, "itself") {
		t.Errorf("error %q does not explain what the gateway is", text)
	}
	// And it answers the question that was asked. The generic "mcphub is
	// this gateway" reply talks about tools, which is not what a caller
	// trying to record a description wanted to know.
	if !strings.Contains(text, "description") {
		t.Errorf("error %q never mentions the description it was asked to save", text)
	}
}

// The gateway is not one of the servers it proxies, and counting it among
// them would make "how many servers are behind this" wrong.
func TestTheGatewayIsNotListedAmongTheServers(t *testing.T) {
	url, _ := gatewayOn(t, twoServers(), nil)
	session := clientOn(t, url, nil)

	var out struct {
		Servers []gateway.ServerSummary `json:"servers"`
	}
	structured(t, callSystemTool(t, session, gateway.ToolListServers, nil), &out)

	for _, server := range out.Servers {
		if server.Name == gateway.Name {
			t.Errorf("the gateway lists itself as one of its own upstream servers")
		}
	}
}

func sameJSON(t *testing.T, a, b any) bool {
	t.Helper()
	return mustJSON(t, a) == mustJSON(t, b)
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	// Round-tripped through a map so that key order cannot make two equal
	// schemas compare unequal.
	encoded, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var normalized any
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	again, err := json.Marshal(normalized)
	if err != nil {
		t.Fatalf("marshal again: %v", err)
	}
	return string(again)
}

func TestGatewayForwardsAToolCall(t *testing.T) {
	ups := twoServers()
	url, _ := gatewayOn(t, ups, nil)
	session := clientOn(t, url, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "files_read",
		Arguments: map[string]any{"path": "/tmp/x"},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("call failed: %s", resultText(result))
	}

	if want := []string{"files/read"}; !slices.Equal(ups.calls, want) {
		t.Errorf("forwarded %v, want %v", ups.calls, want)
	}
	if text := resultText(result); !strings.Contains(text, "/tmp/x") {
		t.Errorf("result %q does not show the arguments reaching the upstream tool", text)
	}
}

// Arguments are handed to the upstream server exactly as the client sent
// them. Decoding and re-encoding on the way through would be a chance to
// lose things a schema cares about: the precision of a large number, or
// the order of keys.
func TestArgumentsAreForwardedVerbatim(t *testing.T) {
	ups := twoServers()
	url, _ := gatewayOn(t, ups, nil)
	session := clientOn(t, url, nil)

	sent := map[string]any{
		"path":     "/tmp/x",
		"big":      json.Number("12345678901234567890"),
		"nested":   map[string]any{"deep": []any{1, "two", true, nil}},
		"unicode":  "路径 🚀",
		"empty":    map[string]any{},
		"emptyArr": []any{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "files_read", Arguments: sent,
	}); err != nil {
		t.Fatalf("CallTool: %v", err)
	}

	if ups.lastArgs == nil {
		t.Fatal("no arguments reached the upstream server")
	}

	// Re-decode what arrived, so the comparison does not depend on key
	// order in the encoding. UseNumber keeps large integers exact, which
	// the default float64 decoding would not — the test must not lose
	// the precision it is checking for.
	var arrived map[string]any
	switch v := ups.lastArgs.(type) {
	case json.RawMessage:
		arrived = decodeExact(t, v)
	case []byte:
		arrived = decodeExact(t, v)
	case map[string]any:
		arrived = v
	default:
		t.Fatalf("arguments arrived as %T, want JSON or a map", ups.lastArgs)
	}

	if got := arrived["path"]; got != "/tmp/x" {
		t.Errorf("path = %v, want /tmp/x", got)
	}
	if got := arrived["unicode"]; got != "路径 🚀" {
		t.Errorf("unicode = %v, want it unchanged", got)
	}
	// A number too large for float64 must not have been rounded on the
	// way through.
	if got := fmt.Sprintf("%v", arrived["big"]); !strings.HasPrefix(got, "12345678901234567") {
		t.Errorf("big = %v, want the value the client sent", got)
	}
	nested, ok := arrived["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested = %T, want an object", arrived["nested"])
	}
	if deep, ok := nested["deep"].([]any); !ok || len(deep) != 4 {
		t.Errorf("nested.deep = %v, want the four elements that were sent", nested["deep"])
	}
}

// decodeExact reads JSON without rounding integers to float64.
func decodeExact(t *testing.T, data []byte) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()

	var out map[string]any
	if err := decoder.Decode(&out); err != nil {
		t.Fatalf("arguments arrived as unparseable JSON %q: %v", data, err)
	}
	return out
}

// A tool the client listed can vanish before it calls it, when the server
// it came from disconnects.
func TestCallingAToolThatWentAway(t *testing.T) {
	ups := twoServers()
	url, g := gatewayOn(t, ups, nil)
	session := clientOn(t, url, nil)

	// The server goes away, and the gateway republishes.
	ups.tools = map[string][]*mcp.Tool{}
	ups.statuses = nil
	g.Sync()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "files_read"})

	// Either the SDK rejects the unknown tool or the handler reports it;
	// what matters is that the client is told, not left hanging.
	if err == nil && !result.IsError {
		t.Fatal("calling a tool that no longer exists succeeded")
	}
}

// A malformed upstream schema must not be able to take the gateway down.
// The SDK rejects a tool whose schema is missing or is not an object.
func TestUpstreamToolsWithUnusableSchemas(t *testing.T) {
	ups := &fakeUpstreams{
		statuses: []upstream.Status{{Name: "odd", State: upstream.StateConnected}},
		tools: map[string][]*mcp.Tool{
			"odd": {
				{Name: "no-schema"},
				{Name: "string-schema", InputSchema: map[string]any{"type": "string"}},
				{Name: "array-schema", InputSchema: map[string]any{"type": "array"}},
				{Name: "junk-schema", InputSchema: "not a schema at all"},
				{Name: "good", InputSchema: map[string]any{"type": "object"}},
			},
		},
	}

	url, _ := gatewayOn(t, ups, nil)
	session := clientOn(t, url, nil)

	names := listedToolNames(t, session)
	for _, want := range []string{
		"odd_no-schema", "odd_string-schema", "odd_array-schema", "odd_junk-schema", "odd_good",
	} {
		if !slices.Contains(names, want) {
			t.Errorf("tool %q was dropped; have %v", want, names)
		}
	}
}

// A tool with a substituted schema still has to be callable.
func TestAToolWithASubstitutedSchemaStillWorks(t *testing.T) {
	ups := &fakeUpstreams{
		statuses: []upstream.Status{{Name: "odd", State: upstream.StateConnected}},
		tools:    map[string][]*mcp.Tool{"odd": {{Name: "bare"}}},
	}
	url, _ := gatewayOn(t, ups, nil)
	session := clientOn(t, url, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "odd_bare",
		Arguments: map[string]any{"anything": 1},
	})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result.IsError {
		t.Fatalf("call failed: %s", resultText(result))
	}
	if want := []string{"odd/bare"}; !slices.Equal(ups.calls, want) {
		t.Errorf("forwarded %v, want %v", ups.calls, want)
	}
}

// The SDK notifies clients once per tool added, so re-adding tools that
// did not change would tell every client to refetch for nothing.
func TestSyncingWithNoChangesDoesNothing(t *testing.T) {
	ups := twoServers()
	url, g := gatewayOn(t, ups, nil)
	session := clientOn(t, url, nil)

	before := listedToolNames(t, session)
	for i := 0; i < 5; i++ {
		g.Sync()
	}
	after := listedToolNames(t, session)

	if !slices.Equal(before, after) {
		t.Errorf("the tool list changed across repeated syncs:\nbefore %v\nafter  %v", before, after)
	}
}

func TestSyncPublishesNewUpstreamTools(t *testing.T) {
	ups := twoServers()
	url, g := gatewayOn(t, ups, func(o *gateway.Options) {
		// The server arrives later but is configured up front, which is
		// the ordinary case: someone writes the configuration and the
		// gateway connects afterwards.
		o.Configs = exposing(t, map[string][]string{
			"files": {"read", "write"},
			"extra": {"thing"},
		})
	})
	session := clientOn(t, url, nil)

	if names := listedToolNames(t, session); slices.Contains(names, "extra_thing") {
		t.Fatalf("extra_thing is listed before its server appeared: %v", names)
	}

	ups.statuses = append(ups.statuses,
		upstream.Status{Name: "extra", State: upstream.StateConnected})
	ups.tools["extra"] = []*mcp.Tool{{Name: "thing"}}
	g.Sync()

	if names := listedToolNames(t, session); !slices.Contains(names, "extra_thing") {
		t.Errorf("extra_thing was not published: %v", names)
	}
}

func TestSyncWithdrawsToolsOfAServerThatWentAway(t *testing.T) {
	ups := twoServers()
	url, g := gatewayOn(t, ups, nil)
	session := clientOn(t, url, nil)

	delete(ups.tools, "files")
	g.Sync()

	names := listedToolNames(t, session)
	for _, gone := range []string{"files_read", "files_write"} {
		if slices.Contains(names, gone) {
			t.Errorf("%q is still listed after its server went away: %v", gone, names)
		}
	}
	// The gateway's own tools are unaffected.
	if !slices.Contains(names, gateway.ToolListServers) {
		t.Errorf("a system tool was removed along with the upstream ones: %v", names)
	}
}

// A server's configuration can restrict which of its tools are exposed.
func TestExposedToolsFilterIsApplied(t *testing.T) {
	ups := twoServers()
	url, _ := gatewayOn(t, ups, func(o *gateway.Options) {
		o.Configs = configFixture(t, map[string]config.MCPServer{
			"files": {
				Transport: config.TransportStdio, Command: "npx", Enabled: true,
				Timeout: time.Minute, ExposedTools: []string{"read"},
			},
		})
	})
	session := clientOn(t, url, nil)

	names := listedToolNames(t, session)
	if !slices.Contains(names, "files_read") {
		t.Errorf("the allowed tool is missing: %v", names)
	}
	if slices.Contains(names, "files_write") {
		t.Errorf("a tool outside the allow list was exposed: %v", names)
	}
}

// Nothing is exposed unless the configuration says so, which is the
// point: a client's opening tools/list is the gateway's own tools and
// nothing else.
func TestAServerExposesNothingUntilItIsAskedTo(t *testing.T) {
	ups := twoServers()
	url, _ := gatewayOn(t, ups, func(o *gateway.Options) {
		o.Configs = exposing(t, map[string][]string{"files": nil})
	})
	session := clientOn(t, url, nil)

	names := listedToolNames(t, session)
	for _, tool := range []string{"files_read", "files_write"} {
		if slices.Contains(names, tool) {
			t.Errorf("%q is exposed although nothing was listed: %v", tool, names)
		}
	}
	if !slices.Contains(names, gateway.ToolListServers) {
		t.Errorf("the gateway's own tools went with them: %v", names)
	}
}

// The whole design rests on this.
//
// Exposing nothing by default is only a deferral — "not in the opening
// hand" — if an unexposed tool can still be reached. Were the gateway's
// own call_tool to honour the same filter, the strict default would be a
// lock instead, and a fresh installation would be able to call nothing at
// all. That the system tools read unfiltered upstream state is currently
// a property of how they are written; this is what makes it a promise.
func TestAnUnexposedToolIsStillFoundAndCalled(t *testing.T) {
	ups := twoServers()
	url, _ := gatewayOn(t, ups, func(o *gateway.Options) {
		o.Configs = exposing(t, map[string][]string{"files": nil})
	})
	session := clientOn(t, url, nil)

	// Not on offer...
	if names := listedToolNames(t, session); slices.Contains(names, "files_read") {
		t.Fatalf("files_read is exposed although nothing was listed: %v", names)
	}

	// ...but list_tools still finds it,
	listed := callSystemTool(t, session, gateway.ToolListTools,
		map[string]any{"server": "files"})
	if text := resultText(listed); !strings.Contains(text, "read") {
		t.Errorf("list_tools does not report an unexposed tool: %s", text)
	}

	// ...get_tool still describes it,
	described := callSystemTool(t, session, gateway.ToolGetTool,
		map[string]any{"server": "files", "tool": "read"})
	if described.IsError {
		t.Errorf("get_tool refused an unexposed tool: %s", resultText(described))
	}

	// ...and call_tool still calls it.
	called := callSystemTool(t, session, gateway.ToolCallTool,
		map[string]any{"server": "files", "tool": "read"})
	if called.IsError {
		t.Fatalf("call_tool refused an unexposed tool: %s", resultText(called))
	}
	if !slices.Contains(ups.calls, "files/read") {
		t.Errorf("the call never reached the upstream; calls = %v", ups.calls)
	}
}

// The exposed name a system tool hands out has to be a name the gateway
// actually registered.
//
// Two computations used to answer "what is this tool called": the system
// tools worked names out over every upstream tool, while Sync registered
// only the exposed ones. Nothing compared them, so list_tools handed out
// names that did not exist. The collision case is the sharper one — an
// unexposed tool sharing a name counted as a clash on one side and not
// the other, so even an exposed tool came back under the wrong name.
func TestReportedExposedNamesAreNamesThatWereRegistered(t *testing.T) {
	// Both servers offer "read". Only one exposes it, so there is no
	// collision among the published tools and the exposed one must keep
	// its unsuffixed name.
	ups := &fakeUpstreams{
		statuses: []upstream.Status{
			{Name: "files", State: upstream.StateConnected},
			{Name: "notes", State: upstream.StateConnected},
		},
		tools: map[string][]*mcp.Tool{
			"files": {{Name: "read"}, {Name: "write"}},
			"notes": {{Name: "read"}},
		},
	}
	url, _ := gatewayOn(t, ups, func(o *gateway.Options) {
		o.Configs = exposing(t, map[string][]string{
			"files": {"read"},
			"notes": nil,
		})
	})
	session := clientOn(t, url, nil)

	registered := listedToolNames(t, session)

	for _, probe := range []struct{ server, tool string }{
		{"files", "read"}, {"files", "write"}, {"notes", "read"},
	} {
		var out struct {
			Tools []gateway.ToolSummary `json:"tools"`
		}
		structured(t, callSystemTool(t, session, gateway.ToolListTools,
			map[string]any{"server": probe.server}), &out)

		for _, summary := range out.Tools {
			if summary.Name != probe.tool || summary.Exposed == "" {
				continue
			}
			if !slices.Contains(registered, summary.Exposed) {
				t.Errorf("list_tools offers %s/%s as %q, which is not registered; registered = %v",
					probe.server, probe.tool, summary.Exposed, registered)
			}
		}
	}

	// And the exposed one is reported, not silently dropped.
	if !slices.Contains(registered, "files_read") {
		t.Errorf("files_read was not registered at all: %v", registered)
	}
}

// Resources are published individually rather than as templates, so a
// client sees what actually exists instead of a pattern it has to guess
// values for.
func TestGatewayListsResources(t *testing.T) {
	url, _ := gatewayOn(t, twoServers(), nil)
	session := clientOn(t, url, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := session.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}

	var listed []string
	for _, resource := range result.Resources {
		listed = append(listed, resource.URI)
	}

	// One per configured server, plus one standing in for each upstream
	// resource.
	for _, want := range []string{
		gateway.ServerResourceURI("files"),
		gateway.ServerResourceURI("broken"),
		gateway.ForwardedResourceURI("files", "file:///tmp/a"),
	} {
		if !slices.Contains(listed, want) {
			t.Errorf("resource %q is missing from %v", want, listed)
		}
	}
}

func TestResourcesFollowUpstreamChanges(t *testing.T) {
	ups := twoServers()
	url, g := gatewayOn(t, ups, nil)
	session := clientOn(t, url, nil)

	delete(ups.resources, "files")
	g.Sync()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := session.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}

	gone := gateway.ForwardedResourceURI("files", "file:///tmp/a")
	for _, resource := range result.Resources {
		if resource.URI == gone {
			t.Errorf("%q is still listed after the upstream resource went away", gone)
		}
	}
	// The server's own resource is unaffected.
	var listed []string
	for _, resource := range result.Resources {
		listed = append(listed, resource.URI)
	}
	if !slices.Contains(listed, gateway.ServerResourceURI("files")) {
		t.Errorf("the server resource was removed too: %v", listed)
	}
}

func TestReadingAServerResource(t *testing.T) {
	url, _ := gatewayOn(t, twoServers(), nil)
	session := clientOn(t, url, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := session.ReadResource(ctx, &mcp.ReadResourceParams{
		URI: gateway.ServerResourceURI("files"),
	})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(result.Contents) == 0 {
		t.Fatal("the server resource has no contents")
	}
	// It describes the server, so the server's own name must appear.
	if text := result.Contents[0].Text; !strings.Contains(text, "files") {
		t.Errorf("contents %q do not describe the server", text)
	}
}

// The tool list is why this resource is worth reading: without it,
// understanding a server with seven tools costs seven calls to get_tool.
// One read has to answer "what can this server do".
func TestAServerResourceListsWhatTheServerCanDo(t *testing.T) {
	url, _ := gatewayOn(t, twoServers(), nil)
	session := clientOn(t, url, nil)

	described := readServerResource(t, session, "files")

	want := map[string]string{
		"read":  "read a file from disk",
		"write": "write a file to disk",
	}
	if !maps.Equal(described.Tools, want) {
		t.Errorf("tools = %v, want %v", described.Tools, want)
	}

	// This configuration records no description, and a caller can do
	// something about that — so the field says so and names the tool that
	// fixes it, rather than being absent.
	if !strings.Contains(described.Description, "update_server_description") {
		t.Errorf("description %q does not say how to record one", described.Description)
	}
}

func TestAServerResourceCarriesTheRecordedDescription(t *testing.T) {
	url, _ := gatewayOn(t, twoServers(), func(o *gateway.Options) {
		o.Configs = twoServersConfig(t)
	})
	session := clientOn(t, url, nil)

	described := readServerResource(t, session, "files")

	if described.Description != "local filesystem" {
		t.Errorf("description = %q, want the one from the configuration", described.Description)
	}
	if described.Tags["env"] != "dev" {
		t.Errorf("tags = %v, want the ones from the configuration", described.Tags)
	}
	// The connection status is still there; the description is added to it,
	// not substituted for it.
	if described.State != upstream.StateConnected {
		t.Errorf("state = %q, want the connection state as well", described.State)
	}
}

// serverResource is the shape a client sees at hub://servers/{name}. It is
// spelled out here rather than imported so that the test breaks when the
// published shape changes, which is the thing worth noticing.
type serverResource struct {
	Name        string            `json:"name"`
	State       upstream.State    `json:"state"`
	Description string            `json:"description"`
	Tags        map[string]string `json:"tags"`
	Tools       map[string]string `json:"tools"`
}

func readServerResource(t *testing.T, session *mcp.ClientSession, server string) serverResource {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := session.ReadResource(ctx, &mcp.ReadResourceParams{
		URI: gateway.ServerResourceURI(server),
	})
	if err != nil {
		t.Fatalf("ReadResource(%s): %v", server, err)
	}
	if len(result.Contents) == 0 {
		t.Fatalf("the resource for %q has no contents", server)
	}

	var described serverResource
	if err := json.Unmarshal([]byte(result.Contents[0].Text), &described); err != nil {
		t.Fatalf("decode %s: %v", result.Contents[0].Text, err)
	}
	return described
}

func TestReadingAForwardedResource(t *testing.T) {
	url, _ := gatewayOn(t, twoServers(), nil)
	session := clientOn(t, url, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := session.ReadResource(ctx, &mcp.ReadResourceParams{
		URI: gateway.ForwardedResourceURI("files", "file:///tmp/a"),
	})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(result.Contents) == 0 || !strings.Contains(result.Contents[0].Text, "files") {
		t.Errorf("contents = %+v, want the text from the upstream server", result.Contents)
	}
}

// The guide is published so that a model can read how to use the gateway
// without anyone pasting the document into its context. It matters most on
// an installation where nothing is exposed yet, which is the default.
func TestTheGuideIsPublishedAsAResource(t *testing.T) {
	url, _ := gatewayOn(t, twoServers(), nil)
	session := clientOn(t, url, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	listed, err := session.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if !slices.ContainsFunc(listed.Resources, func(r *mcp.Resource) bool {
		return r.URI == gateway.GuideResourceURI
	}) {
		t.Fatalf("%q is not listed: %v", gateway.GuideResourceURI, uris(listed.Resources))
	}

	result, err := session.ReadResource(ctx, &mcp.ReadResourceParams{
		URI: gateway.GuideResourceURI,
	})
	if err != nil {
		t.Fatalf("ReadResource: %v", err)
	}
	if len(result.Contents) != 1 {
		t.Fatalf("the guide came back as %d parts, want one document", len(result.Contents))
	}

	// The same document the CLI prints, not a copy of it. Two embedded
	// copies would start out identical and drift the first time one is
	// edited, which is a failure nothing else would notice.
	if got := result.Contents[0].Text; got != guide.Text() {
		t.Errorf("the resource is not the shared document (%d bytes served, %d in the package)",
			len(got), len(guide.Text()))
	}
	if got := result.Contents[0].MIMEType; got != guide.MIMEType {
		t.Errorf("mime type = %q, want %q", got, guide.MIMEType)
	}
}

func TestReadingAResourceThatIsNotOurs(t *testing.T) {
	url, _ := gatewayOn(t, twoServers(), nil)
	session := clientOn(t, url, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := session.ReadResource(ctx, &mcp.ReadResourceParams{
		URI: "file:///etc/passwd",
	}); err == nil {
		t.Error("reading a resource outside the gateway succeeded")
	}
}

func TestSessionsAreReported(t *testing.T) {
	url, g := gatewayOn(t, twoServers(), nil)

	if got := g.Sessions(); len(got) != 0 {
		t.Errorf("Sessions = %+v before anyone connected, want none", got)
	}

	clientOn(t, url, nil)

	// The session appears once the handshake has been processed.
	deadline := time.Now().Add(5 * time.Second)
	var sessions []gateway.SessionInfo
	for time.Now().Before(deadline) {
		sessions = g.Sessions()
		if len(sessions) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// One connecting client can leave more than one session behind: the
	// SDK client offers the newest protocol version first and starts
	// over at an older one if the server negotiates down, abandoning the
	// first session. Abandoned sessions are reaped by the session
	// timeout, so what matters here is that the client is identified,
	// not how many entries it produced.
	if len(sessions) == 0 {
		t.Fatal("no session was recorded after a client connected")
	}
	for _, session := range sessions {
		if session.ID == "" {
			t.Errorf("session %+v has no id", session)
		}
		if session.ClientName != "probe" {
			t.Errorf("clientName = %q, want probe", session.ClientName)
		}
		if session.ProtocolVersion == "" {
			t.Errorf("session %s records no protocol version", session.ID)
		}
	}
}

// Sessions are ordered so a list in the UI does not reshuffle.
func TestSessionsAreOrderedById(t *testing.T) {
	url, g := gatewayOn(t, twoServers(), nil)
	clientOn(t, url, nil)
	clientOn(t, url, nil)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(g.Sessions()) >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	sessions := g.Sessions()
	for i := 1; i < len(sessions); i++ {
		if sessions[i-1].ID > sessions[i].ID {
			t.Errorf("sessions are not ordered by id: %q before %q",
				sessions[i-1].ID, sessions[i].ID)
		}
	}
}

// In stateless mode the SDK answers a GET with 405, which is how a
// client that cannot hold a session avoids waiting on a stream that will
// never arrive.
func TestStatelessModeRejectsAnEventStream(t *testing.T) {
	url, _ := gatewayOn(t, twoServers(), nil)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build the request: %v", err)
	}
	req.Header.Set(gateway.SessionModeHeader, string(config.SessionModeStateless))
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

// A stateful client works the same way it would against a plain MCP
// server, which is the mode most clients want.
func TestStatefulModeServesASession(t *testing.T) {
	url, g := gatewayOn(t, twoServers(), nil)

	session := clientOn(t, url, http.Header{
		gateway.SessionModeHeader: []string{string(config.SessionModeStateful)},
	})
	if names := listedToolNames(t, session); len(names) == 0 {
		t.Error("no tools were listed in stateful mode")
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(g.Sessions()) > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("a stateful request did not produce a session")
}

// A stateless request must not leave a session behind, or the session
// list would fill up with clients that are not there.
func TestStatelessModeLeavesNoSession(t *testing.T) {
	url, g := gatewayOn(t, twoServers(), nil)

	session := clientOn(t, url, http.Header{
		gateway.SessionModeHeader: []string{string(config.SessionModeStateless)},
	})
	if names := listedToolNames(t, session); len(names) == 0 {
		t.Error("no tools were listed in stateless mode")
	}

	if got := g.Sessions(); len(got) != 0 {
		t.Errorf("Sessions = %+v after a stateless request, want none", got)
	}
}

// A client that cannot set headers is routed by its User-Agent instead.
func TestUserAgentSelectsStatelessMode(t *testing.T) {
	url, g := gatewayOn(t, twoServers(), func(o *gateway.Options) {
		o.Gateway.SessionModeRules.Stateless = []string{"probe"}
	})

	session := clientOn(t, url, http.Header{"User-Agent": []string{"probe/1.0"}})
	if names := listedToolNames(t, session); len(names) == 0 {
		t.Error("no tools were listed")
	}

	if got := g.Sessions(); len(got) != 0 {
		t.Errorf("Sessions = %+v, want none; the User-Agent rule was not applied", got)
	}
}

// Watch is what keeps the exposed tools current without anyone polling.
func TestWatchRepublishesOnUpstreamChanges(t *testing.T) {
	ups := twoServers()
	bus := events.NewBus()
	defer bus.Close()

	url, g := gatewayOn(t, ups, func(o *gateway.Options) {
		// No debounce delay, so the test does not wait on a window.
		o.Gateway.NotifyDebounce = 0
		o.Configs = exposing(t, map[string][]string{
			"files": {"read", "write"},
			"late":  {"arrival"},
		})
	})
	session := clientOn(t, url, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g.Watch(ctx, bus)

	ups.statuses = append(ups.statuses,
		upstream.Status{Name: "late", State: upstream.StateConnected})
	ups.tools["late"] = []*mcp.Tool{{Name: "arrival"}}
	bus.PublishServer(events.ServerConnected, "late", nil)

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if slices.Contains(listedToolNames(t, session), "late_arrival") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("the new tool never appeared; have %v", listedToolNames(t, session))
}

func TestWatchStopsWithItsContext(t *testing.T) {
	bus := events.NewBus()
	defer bus.Close()

	_, g := gatewayOn(t, twoServers(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	g.Watch(ctx, bus)
	cancel()

	// Publishing after the watch has stopped must not panic or block.
	for i := 0; i < 10; i++ {
		bus.PublishServer(events.ToolsChanged, "files", nil)
	}
}

func TestWatchWithoutABusIsHarmless(t *testing.T) {
	_, g := gatewayOn(t, twoServers(), nil)
	g.Watch(context.Background(), nil)
}

func TestWireDebugDoesNotBreakAnything(t *testing.T) {
	url, _ := gatewayOn(t, twoServers(), func(o *gateway.Options) {
		o.WireDebug = true
	})
	session := clientOn(t, url, nil)

	if names := listedToolNames(t, session); len(names) == 0 {
		t.Error("no tools were listed with wire logging on")
	}
}
