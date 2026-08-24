package config_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"mcphub/internal/config"
)

// writeConfig puts content in a temporary file and returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

func TestLoadFullConfig(t *testing.T) {
	path := writeConfig(t, `
version: 1
listen:
  host: 0.0.0.0
  port: 9000
logging:
  level: debug
  format: json
  maxAge: 48h
  maxSizeMB: 10
  mcpWireDebug: true
security:
  allowedNetworks: ["10.0.0.0/8"]
  maxConnections: 5
  connectionTimeout: 15s
gateway:
  defaultSessionMode: stateless
  notifyDebounce: 1s
startup:
  maxRetries: 7
mcpServers:
  files:
    transport: stdio
    command: npx
    args: ["-y", "server-filesystem"]
    timeout: 90s
    tags:
      env: prod
`)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Listen.Host != "0.0.0.0" || cfg.Listen.Port != 9000 {
		t.Errorf("listen = %+v, want 0.0.0.0:9000", cfg.Listen)
	}
	if cfg.Logging.Level != config.LevelDebug || cfg.Logging.Format != "json" {
		t.Errorf("logging = %+v, want debug/json", cfg.Logging)
	}
	if cfg.Logging.MaxAge != 48*time.Hour {
		t.Errorf("logging.maxAge = %v, want 48h", cfg.Logging.MaxAge)
	}
	if !cfg.Logging.MCPWireDebug {
		t.Error("logging.mcpWireDebug = false, want true")
	}
	if cfg.Security.ConnectionTimeout != 15*time.Second {
		t.Errorf("security.connectionTimeout = %v, want 15s", cfg.Security.ConnectionTimeout)
	}
	if cfg.Gateway.DefaultSessionMode != config.SessionModeStateless {
		t.Errorf("gateway.defaultSessionMode = %q, want stateless", cfg.Gateway.DefaultSessionMode)
	}
	if cfg.Startup.MaxRetries != 7 {
		t.Errorf("startup.maxRetries = %d, want 7", cfg.Startup.MaxRetries)
	}

	files, ok := cfg.MCPServers["files"]
	if !ok {
		t.Fatalf("mcpServers = %v, want a %q entry", cfg.MCPServers, "files")
	}
	if files.Command != "npx" || len(files.Args) != 2 {
		t.Errorf("mcpServers.files command/args = %q/%v", files.Command, files.Args)
	}
	if files.Timeout != 90*time.Second {
		t.Errorf("mcpServers.files.timeout = %v, want 90s", files.Timeout)
	}
	if files.Tags["env"] != "prod" {
		t.Errorf("mcpServers.files.tags = %v, want env=prod", files.Tags)
	}
}

func TestLoadEmptyFileYieldsDefaults(t *testing.T) {
	for _, content := range []string{"", "   \n\n  ", "\n"} {
		cfg, err := config.Load(writeConfig(t, content))
		if err != nil {
			t.Fatalf("Load(%q): %v", content, err)
		}
		if cfg.Listen.Port != config.Default().Listen.Port {
			t.Errorf("Load(%q) port = %d, want default", content, cfg.Listen.Port)
		}
		if cfg.Logging.Level != config.Default().Logging.Level {
			t.Errorf("Load(%q) level = %q, want default", content, cfg.Logging.Level)
		}
	}
}

func TestLoadPartialKeepsDefaultsForTheRest(t *testing.T) {
	path := writeConfig(t, "logging:\n  level: debug\n")

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	def := config.Default()

	if cfg.Logging.Level != config.LevelDebug {
		t.Errorf("logging.level = %q, want debug", cfg.Logging.Level)
	}
	// Siblings inside the same section must survive.
	if cfg.Logging.Format != def.Logging.Format {
		t.Errorf("logging.format = %q, want default %q", cfg.Logging.Format, def.Logging.Format)
	}
	if cfg.Logging.MaxAge != def.Logging.MaxAge {
		t.Errorf("logging.maxAge = %v, want default %v", cfg.Logging.MaxAge, def.Logging.MaxAge)
	}
	// Untouched sections must survive.
	if cfg.Listen.Port != def.Listen.Port {
		t.Errorf("listen.port = %d, want default %d", cfg.Listen.Port, def.Listen.Port)
	}
	if len(cfg.Security.AllowedNetworks) != len(def.Security.AllowedNetworks) {
		t.Errorf("security.allowedNetworks = %v, want default %v",
			cfg.Security.AllowedNetworks, def.Security.AllowedNetworks)
	}
}

func TestLoadMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist.yaml")

	if _, err := config.Load(missing); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Load error = %v, want it to wrap fs.ErrNotExist", err)
	}

	cfg, err := config.LoadOrDefault(missing)
	if err != nil {
		t.Fatalf("LoadOrDefault: %v", err)
	}
	if cfg.Listen.Port != config.Default().Listen.Port {
		t.Errorf("LoadOrDefault port = %d, want default", cfg.Listen.Port)
	}
}

func TestLoadSyntaxErrorIsLocated(t *testing.T) {
	path := writeConfig(t, "listen:\n  host: 127.0.0.1\n  port: not-a-number\n")

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("Load succeeded on a malformed document, want an error")
	}
	// The message has to be actionable: it should point at the offending
	// line, since a user fixes this by editing the file.
	if !strings.Contains(err.Error(), "3") {
		t.Errorf("error %q does not mention the offending line number", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not mention the file path", err)
	}
}

func TestLoadAppliesPerServerDefaults(t *testing.T) {
	path := writeConfig(t, `
mcpServers:
  omits-everything:
    command: npx
  explicitly-disabled:
    command: npx
    enabled: false
  overrides-timeout:
    command: npx
    timeout: 5s
  no-body:
`)

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	def := config.DefaultMCPServer()

	t.Run("omitted enabled means enabled", func(t *testing.T) {
		if !cfg.MCPServers["omits-everything"].Enabled {
			t.Error("enabled = false, want true when the key is omitted")
		}
	})

	t.Run("explicit false is honoured", func(t *testing.T) {
		if cfg.MCPServers["explicitly-disabled"].Enabled {
			t.Error("enabled = true, want false when explicitly set")
		}
	})

	t.Run("omitted timeout gets the default", func(t *testing.T) {
		if got := cfg.MCPServers["omits-everything"].Timeout; got != def.Timeout {
			t.Errorf("timeout = %v, want default %v", got, def.Timeout)
		}
	})

	t.Run("explicit timeout wins", func(t *testing.T) {
		if got := cfg.MCPServers["overrides-timeout"].Timeout; got != 5*time.Second {
			t.Errorf("timeout = %v, want 5s", got)
		}
	})

	t.Run("omitted transport gets the default", func(t *testing.T) {
		if got := cfg.MCPServers["omits-everything"].Transport; got != def.Transport {
			t.Errorf("transport = %q, want default %q", got, def.Transport)
		}
	})

	t.Run("entry with no body is all defaults", func(t *testing.T) {
		got, ok := cfg.MCPServers["no-body"]
		if !ok {
			t.Fatal("entry with no body was dropped")
		}
		if !reflect.DeepEqual(got, def) {
			t.Errorf("entry = %+v, want %+v", got, def)
		}
	})
}

func TestLoadEmptyServerSectionIsNotTheSameAsAbsent(t *testing.T) {
	absent, err := config.Load(writeConfig(t, "listen:\n  port: 1234\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if absent.MCPServers == nil {
		t.Error("mcpServers is nil when the section is absent; want an empty, writable map")
	}
	if len(absent.MCPServers) != 0 {
		t.Errorf("mcpServers = %v, want empty", absent.MCPServers)
	}

	empty, err := config.Load(writeConfig(t, "mcpServers: {}\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(empty.MCPServers) != 0 {
		t.Errorf("mcpServers = %v, want empty", empty.MCPServers)
	}
}

// An explicitly empty list means "allow every client", which must not be
// confused with the key being absent and defaulting to loopback only.
func TestLoadDistinguishesEmptyListFromAbsentList(t *testing.T) {
	absent, err := config.Load(writeConfig(t, "listen:\n  port: 1234\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(absent.Security.AllowedNetworks) == 0 {
		t.Error("allowedNetworks is empty when absent; want the loopback default")
	}

	empty, err := config.Load(writeConfig(t, "security:\n  allowedNetworks: []\n"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(empty.Security.AllowedNetworks) != 0 {
		t.Errorf("allowedNetworks = %v, want empty", empty.Security.AllowedNetworks)
	}
}

func TestParseIsIndependentOfDefaults(t *testing.T) {
	cfg, err := config.Parse([]byte("security:\n  allowedNetworks: [\"10.0.0.0/8\"]\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	cfg.Security.AllowedNetworks[0] = "0.0.0.0/0"

	if config.Default().Security.AllowedNetworks[0] != "127.0.0.1/32" {
		t.Error("parsing mutated the package defaults")
	}
}
