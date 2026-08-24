package upstream_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The tests in this package need a real MCP server on the other end of a
// real pipe. Rather than depending on npx or a checked-in binary, the
// test binary re-executes itself: TestMain notices the environment
// variable and serves MCP instead of running tests.
//
// That keeps the tests hermetic — no network, no toolchain outside Go —
// and fast, since there is nothing to build.

const serverModeEnv = "MCPHUB_TEST_MCP_SERVER_MODE"

// Server behaviours the tests need to provoke.
const (
	// modeFull declares and serves tools, resources and logging.
	modeFull = "full"

	// modeToolsOnly serves tools and declares nothing else, which is
	// what capability-aware refreshing has to cope with.
	modeToolsOnly = "tools-only"

	// modeNoisyStderr writes to standard error before serving.
	modeNoisyStderr = "noisy-stderr"

	// modeCrash complains on standard error and exits without speaking
	// MCP at all.
	modeCrash = "crash"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv(serverModeEnv); mode != "" {
		os.Exit(serveTestServer(mode))
	}
	os.Exit(m.Run())
}

func serveTestServer(mode string) int {
	if mode == modeCrash {
		fmt.Fprintln(os.Stderr, "cannot start: the flux capacitor is missing")
		return 3
	}
	if mode == modeNoisyStderr {
		fmt.Fprintln(os.Stderr, "warming up")
		fmt.Fprintln(os.Stderr, "listening on stdio")
		// A trailing line with no newline is the common shape of output
		// from a process that is interrupted.
		fmt.Fprint(os.Stderr, "no newline at the end")
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "test-server",
		Version: "9.9.9",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "echo",
		Description: "returns its argument",
	}, echoTool)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "sleep",
		Description: "blocks for a while, to provoke a timeout",
	}, sleepTool)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "grow",
		Description: "adds another tool, which makes the server report a changed tool list",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
		mcp.AddTool(server, &mcp.Tool{Name: "grown", Description: "added at runtime"}, echoTool)
		return textResult("grown"), nil, nil
	})

	if mode == modeFull {
		server.AddResource(
			&mcp.Resource{URI: "test://greeting", Name: "greeting", MIMEType: "text/plain"},
			func(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
				return &mcp.ReadResourceResult{
					Contents: []*mcp.ResourceContents{
						{URI: "test://greeting", MIMEType: "text/plain", Text: "hello"},
					},
				}, nil
			})
	}

	// Run returns once the client closes the connection, which is the
	// normal way this process ends rather than a failure.
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		fmt.Fprintf(os.Stderr, "test server stopped: %v\n", err)
	}
	return 0
}

type echoInput struct {
	Message string `json:"message"`
}

func echoTool(_ context.Context, _ *mcp.CallToolRequest, in echoInput) (*mcp.CallToolResult, any, error) {
	return textResult(in.Message), nil, nil
}

type sleepInput struct {
	Seconds int `json:"seconds"`
}

func sleepTool(ctx context.Context, _ *mcp.CallToolRequest, in sleepInput) (*mcp.CallToolResult, any, error) {
	select {
	case <-time.After(time.Duration(in.Seconds) * time.Second):
		return textResult("slept"), nil, nil
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}
