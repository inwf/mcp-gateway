package integration

import (
	"flag"
	"os/exec"
	"path/filepath"
	"testing"
)

var tsSDKDir = flag.String("mcp-ts-sdk-dir", "", "directory with the TypeScript MCP SDK installed for optional interop tests")

// Opt-in because make check otherwise needs only the repository's Go and
// frontend dependencies. The SDK stays in an isolated development directory.
func TestTypeScriptSDKCanDiscoverAndCallThroughBothUpstreamTransports(t *testing.T) {
	if *tsSDKDir == "" {
		t.Skip("set -args -mcp-ts-sdk-dir=/path/to/sdk-installation to run the actual TypeScript SDK")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("the interop test requires node: ", err)
	}
	directory, err := filepath.Abs(*tsSDKDir)
	if err != nil {
		t.Fatal(err)
	}
	stack := discoveryStack(t)
	cmd := exec.CommandContext(t.Context(), node, "testdata/interop.mjs", directory, stack.URL)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("TypeScript MCP SDK interop failed: %v\n%s", err, output)
	}
	t.Log(string(output))
}
