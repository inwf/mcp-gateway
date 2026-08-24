// Command mcphub runs the MCP gateway: it proxies a set of configured
// upstream MCP servers behind a single MCP endpoint, and serves a web
// UI for managing them.
package main

import (
	"fmt"
	"os"
)

// version is the human-readable release version. Plain `go build` already
// stamps the git revision into the binary (readable via runtime/debug), so
// this only needs overriding for tagged release builds, via
// -ldflags "-X main.version=<value>".
var version = "dev"

func main() {
	if _, err := fmt.Fprintf(os.Stdout, "mcphub %s\n", version); err != nil {
		os.Exit(1)
	}
}
