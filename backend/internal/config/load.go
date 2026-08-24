package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
)

// Load reads and parses the configuration at path.
//
// Fields the file omits keep the value from [Default], so a file need
// only state what it changes. A missing file is reported as an error
// wrapping [fs.ErrNotExist]; use [LoadOrDefault] for the first-run case.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %s: %w", path, err)
	}

	cfg, err := Parse(data)
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// LoadOrDefault behaves like [Load], except that a missing file yields
// the default configuration instead of an error. This is the first-run
// case: mcphub starts with no upstream servers and writes a file once
// the user configures one.
func LoadOrDefault(path string) (Config, error) {
	cfg, err := Load(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	return cfg, err
}

// Parse decodes a configuration document over the defaults. An empty
// document yields exactly [Default].
func Parse(data []byte) (Config, error) {
	cfg := Default()

	// An empty document is not valid YAML to decode into a struct, but
	// it is a meaningful configuration: everything is defaulted.
	if len(bytes.TrimSpace(data)) == 0 {
		return cfg, nil
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	// The pass above decoded each upstream entry into a zero-valued
	// struct, so entries that omitted a field got Go's zero value rather
	// than the intended default. Decode them again over the per-server
	// defaults to correct that.
	servers, err := parseMCPServers(data)
	if err != nil {
		return Config{}, err
	}
	if servers != nil {
		cfg.MCPServers = servers
	}

	return cfg, nil
}

// parseMCPServers decodes each upstream entry over [DefaultMCPServer].
// It returns nil when the document has no mcpServers section at all,
// which the caller distinguishes from an empty section.
func parseMCPServers(data []byte) (map[string]MCPServer, error) {
	var doc struct {
		MCPServers map[string]ast.Node `yaml:"mcpServers"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if doc.MCPServers == nil {
		return nil, nil
	}

	servers := make(map[string]MCPServer, len(doc.MCPServers))
	for name, node := range doc.MCPServers {
		server := DefaultMCPServer()
		// A key with no body ("myserver:") decodes to a null node and
		// leaves the defaults in place, which is the useful reading.
		if node != nil {
			if err := yaml.NodeToValue(node, &server); err != nil {
				return nil, fmt.Errorf("parse config: mcpServers.%s: %w", name, err)
			}
		}
		servers[name] = server
	}
	return servers, nil
}
