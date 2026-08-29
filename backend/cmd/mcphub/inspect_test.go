package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"mcphub/internal/config"
	"mcphub/internal/gateway"
	"mcphub/internal/testmcp"
)

// ===== tools show =====

// `tools list` shortens every description to one line so that the list
// stays a list. This is where the full text and the input schema live,
// and the schema is what someone building a call actually needs.

func TestToolsShowGivesTheFullDescriptionAndSchema(t *testing.T) {
	address, cleanup := withTestServer(t, "probe")
	defer cleanup()

	code, stdout, stderr := execute(t, "tools", "show", "probe_echo", "--address", address)

	if code != exitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, exitOK, stderr)
	}
	for _, want := range []string{"probe_echo", "probe", "returns its argument", "message"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the output does not contain %q:\n%s", want, stdout)
		}
	}
	// The schema is the point of the command, so a rendering that dropped
	// it would leave nothing this command is for.
	if !strings.Contains(stdout, "INPUT SCHEMA") {
		t.Errorf("the input schema is missing:\n%s", stdout)
	}
}

// The argument table is what a reader scans before typing --arg. Which
// arguments are required is the part they cannot guess.
func TestToolsShowSaysWhichArgumentsAreRequired(t *testing.T) {
	address, cleanup := withTestServer(t, "probe")
	defer cleanup()

	_, stdout, _ := execute(t, "tools", "show", "probe_echo", "--address", address)

	if !strings.Contains(stdout, "REQUIRED") {
		t.Fatalf("there is no column saying what is required:\n%s", stdout)
	}
	row := rowFor(t, stdout, "message")
	if !strings.Contains(row, "yes") {
		t.Errorf("message is required and the row does not say so: %q", row)
	}
	if !strings.Contains(row, "string") {
		t.Errorf("the declared type is missing from the row: %q", row)
	}
}

func TestToolsShowWorksForTheGatewaysOwnTools(t *testing.T) {
	address, cleanup := withTestServer(t, "probe")
	defer cleanup()

	code, stdout, stderr := execute(t, "tools", "show", gateway.ToolListTools, "--address", address)

	if code != exitOK {
		t.Fatalf("exit code = %d\nstderr: %s", code, stderr)
	}
	// A gateway tool is forwarded from nowhere. Printing a blank or a dash
	// under a "server" heading would leave a reader wondering which one.
	if !strings.Contains(stdout, "the gateway itself") {
		t.Errorf("the output does not say where the tool comes from:\n%s", stdout)
	}
	if !strings.Contains(stdout, "server") {
		t.Errorf("the tool's own \"server\" argument is missing:\n%s", stdout)
	}
}

func TestToolsShowRefusesAToolThatIsNotThere(t *testing.T) {
	address, cleanup := withTestServer(t, "probe")
	defer cleanup()

	code, _, stderr := execute(t, "tools", "show", "nonesuch", "--address", address)

	if code != exitFailure {
		t.Errorf("exit code = %d, want %d", code, exitFailure)
	}
	if !strings.Contains(stderr, "nonesuch") {
		t.Errorf("the message does not name what was asked for:\n%s", stderr)
	}
}

func TestToolsShowCanPrintJSON(t *testing.T) {
	address, cleanup := withTestServer(t, "probe")
	defer cleanup()

	code, stdout, stderr := execute(t, "tools", "show", "probe_echo", "--json", "--address", address)

	if code != exitOK {
		t.Fatalf("exit code = %d\nstderr: %s", code, stderr)
	}
	var decoded struct {
		Exposed     string         `json:"exposed"`
		Server      string         `json:"server"`
		InputSchema map[string]any `json:"inputSchema"`
	}
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		t.Fatalf("--json did not print JSON: %v\n%s", err, stdout)
	}

	if decoded.Exposed != "probe_echo" {
		t.Errorf("exposed = %q, want %q", decoded.Exposed, "probe_echo")
	}
	if decoded.Server != "probe" {
		t.Errorf("server = %q, want %q", decoded.Server, "probe")
	}
	if decoded.InputSchema == nil {
		t.Error("the schema is missing, which is what --json is for")
	}
}

// ===== tags list =====

// withTaggedServers starts a gateway with two servers carrying tags.
func withTaggedServers(t *testing.T) (address string, cleanup func()) {
	t.Helper()

	base, stop, done := running(t, func(cfg *config.Config) {
		upstream, err := testmcp.ServerConfig(testmcp.ModeFull)
		if err != nil {
			t.Fatalf("build the upstream configuration: %v", err)
		}

		one := upstream
		one.Tags = map[string]string{"env": "dev", "kind": "probe"}
		two := upstream
		two.Tags = map[string]string{"env": "dev"}

		cfg.MCPServers = map[string]config.MCPServer{"one": one, "two": two}
	})

	address = hostPort(t, base)
	waitForState(t, address, "one", "connected")
	return address, func() { stop(); <-done }
}

// A tag belongs to a server rather than to a tool, and the same tag on
// two servers is one filter rather than two — so the list is by tag, with
// the servers carrying it.
func TestTagsListGroupsServersUnderEachTag(t *testing.T) {
	address, cleanup := withTaggedServers(t)
	defer cleanup()

	code, stdout, stderr := execute(t, "tags", "list", "--address", address)

	if code != exitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, exitOK, stderr)
	}

	shared := rowFor(t, stdout, "env")
	for _, want := range []string{"dev", "one", "two"} {
		if !strings.Contains(shared, want) {
			t.Errorf("the row for env does not contain %q: %q", want, shared)
		}
	}

	// A tag only one server carries lists only that one.
	only := rowFor(t, stdout, "kind")
	if !strings.Contains(only, "one") {
		t.Errorf("the row for kind does not name the server carrying it: %q", only)
	}
	if strings.Contains(only, "two") {
		t.Errorf("the row for kind names a server that does not carry it: %q", only)
	}
}

func TestTagsListCanNarrowToOneServer(t *testing.T) {
	address, cleanup := withTaggedServers(t)
	defer cleanup()

	code, stdout, _ := execute(t, "tags", "list", "--server", "two", "--address", address)

	if code != exitOK {
		t.Fatalf("exit code = %d, want %d", code, exitOK)
	}
	if !strings.Contains(stdout, "env") {
		t.Errorf("the tag the server carries is missing:\n%s", stdout)
	}
	if strings.Contains(stdout, "kind") {
		t.Errorf("a tag the server does not carry was listed:\n%s", stdout)
	}
}

// Reporting "no tags" for a server that does not exist would send someone
// looking for a tag that was never the problem.
func TestTagsListRefusesAServerThatIsNotThere(t *testing.T) {
	address, cleanup := withTaggedServers(t)
	defer cleanup()

	code, _, stderr := execute(t, "tags", "list", "--server", "nowhere", "--address", address)

	if code != exitFailure {
		t.Errorf("exit code = %d, want %d", code, exitFailure)
	}
	if !strings.Contains(stderr, "nowhere") {
		t.Errorf("the message does not name the server that was asked for:\n%s", stderr)
	}
	// The configured names are what someone typing from memory needs.
	if !strings.Contains(stderr, "one") || !strings.Contains(stderr, "two") {
		t.Errorf("the message does not say which servers exist:\n%s", stderr)
	}
}

func TestTagsListSaysSoWhenThereAreNone(t *testing.T) {
	address, cleanup := withTestServer(t, "probe")
	defer cleanup()

	code, stdout, _ := execute(t, "tags", "list", "--address", address)

	if code != exitOK {
		t.Fatalf("exit code = %d, want %d", code, exitOK)
	}
	if !strings.Contains(stdout, "no tags") {
		t.Errorf("the output does not say that there are none:\n%s", stdout)
	}
}

// ===== ui =====

// The URL is printed whether or not a browser opens, so that the command
// is still useful over ssh or in a container.
func TestUIPrintsTheAddress(t *testing.T) {
	address, cleanup := withTestServer(t, "probe")
	defer cleanup()

	code, stdout, stderr := execute(t, "ui", "--print", "--address", address)

	if code != exitOK {
		t.Fatalf("exit code = %d, want %d\nstderr: %s", code, exitOK, stderr)
	}
	if !strings.Contains(stdout, "http://"+address) {
		t.Errorf("the address is missing from the output: %q", stdout)
	}
}

// A browser window on a refused connection is a worse answer than a
// sentence saying nothing is running.
func TestUIReportsThatNoGatewayIsRunning(t *testing.T) {
	code, _, stderr := execute(t, "ui", "--address", unusedAddress(t))

	if code != exitFailure {
		t.Errorf("exit code = %d, want %d", code, exitFailure)
	}
	if stderr == "" {
		t.Error("nothing was written to standard error")
	}
}

// What --print suppresses is the opening, not the printing.
func TestUIOpensWhatItPrints(t *testing.T) {
	address, cleanup := withTestServer(t, "probe")
	defer cleanup()

	// The command is built here with an opener that records rather than
	// launches: a test suite must not open real browser windows.
	var opened string
	stdout := &strings.Builder{}
	cmd := newUICommand(&globalOptions{}, stdout, func(_ context.Context, url string) error {
		opened = url
		return nil
	})
	cmd.SetArgs([]string{"--address", address})
	cmd.SetOut(stdout)
	cmd.SetErr(stdout)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("ui: %v", err)
	}
	if opened != "http://"+address {
		t.Errorf("opened %q, want %q", opened, "http://"+address)
	}
	if !strings.Contains(stdout.String(), opened) {
		t.Errorf("what was opened was not printed: %q", stdout.String())
	}
}
