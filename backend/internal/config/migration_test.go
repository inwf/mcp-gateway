package config_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mcphub/internal/config"
)

func TestLegacyServerTagsAreRejectedWithoutRewritingTheConfiguration(t *testing.T) {
	document := []byte("mcpServers:\n  files:\n    command: echo\n    tags: {env: dev}\n")
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, document, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(path)
	if err == nil || !strings.Contains(err.Error(), "tags") || !strings.Contains(err.Error(), "4:") {
		t.Fatalf("legacy tags should identify the field and its line: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, document) {
		t.Fatal("loading rewrote the user's configuration")
	}
	if _, err := config.ParseServer([]byte("command: echo\ntags: {}\n")); err == nil {
		t.Error("single-server parsing silently accepted the removed field")
	}
}

func TestATagsEnvironmentVariableIsStillValidConfiguration(t *testing.T) {
	server, err := config.ParseServer([]byte("command: echo\nenv:\n  tags: business-data\n"))
	if err != nil {
		t.Fatal(err)
	}
	if server.Env["tags"] != "business-data" {
		t.Errorf("upstream environment was modified: %v", server.Env)
	}
}
