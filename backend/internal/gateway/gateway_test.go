package gateway_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
