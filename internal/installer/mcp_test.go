package installer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tidwall/gjson"
)

func TestConfigureMCPLocal(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "opencode.json")
	if err := os.WriteFile(cfg, []byte(`{"$schema":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	spec := MCPSpec{
		Type:        "local",
		Command:     []string{"npx", "-y", "@burtthecoder/mcp-shodan"},
		Environment: map[string]string{"SHODAN_API_KEY": "{env:SHODAN_API_KEY}"},
	}
	if err := ConfigureMCP(cfg, "shodan", spec); err != nil {
		t.Fatalf("ConfigureMCP: %v", err)
	}

	data, _ := os.ReadFile(cfg)
	if !gjson.ValidBytes(data) {
		t.Fatal("result is not valid JSON")
	}
	if got := gjson.GetBytes(data, "mcp.shodan.type").String(); got != "local" {
		t.Errorf("type = %q, want local", got)
	}
	if got := gjson.GetBytes(data, "mcp.shodan.command.0").String(); got != "npx" {
		t.Errorf("command[0] = %q, want npx", got)
	}
	if got := gjson.GetBytes(data, "mcp.shodan.environment.SHODAN_API_KEY").String(); got != "{env:SHODAN_API_KEY}" {
		t.Errorf("env = %q, want the {env:...} reference", got)
	}
	// A backup was created.
	if _, err := os.Stat(cfg + mcpBackupSuffix); err != nil {
		t.Errorf("backup not created: %v", err)
	}
}

func TestConfigureMCPRemote(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "opencode.json")
	if err := os.WriteFile(cfg, []byte(`{"$schema":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := MCPSpec{Type: "remote", URL: "https://mcp.example/v1", Headers: map[string]string{"Authorization": "Bearer {env:TOK}"}}
	if err := ConfigureMCP(cfg, "api", spec); err != nil {
		t.Fatalf("ConfigureMCP: %v", err)
	}
	data, _ := os.ReadFile(cfg)
	if got := gjson.GetBytes(data, "mcp.api.type").String(); got != "remote" {
		t.Errorf("type = %q, want remote", got)
	}
	if got := gjson.GetBytes(data, "mcp.api.url").String(); got != "https://mcp.example/v1" {
		t.Errorf("url = %q", got)
	}
	// A local spec must not have leaked a command into the remote entry.
	if gjson.GetBytes(data, "mcp.api.command").Exists() {
		t.Error("remote entry unexpectedly has a command")
	}
}

func TestConfigureMCPValidatesSpec(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "opencode.json")
	if err := os.WriteFile(cfg, []byte(`{"$schema":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []MCPSpec{
		{Type: "local"},                         // no command
		{Type: "remote"},                        // no url
		{Type: "bogus", Command: []string{"x"}}, // unknown type
	}
	for _, spec := range cases {
		if err := ConfigureMCP(cfg, "x", spec); err == nil {
			t.Errorf("ConfigureMCP(%+v) = nil, want error", spec)
		}
	}
}

func TestConfigureMCPPreservesExisting(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "opencode.json")
	if err := os.WriteFile(cfg, []byte(`{"$schema":"x","mcp":{"engram":{"type":"local","command":["engram","mcp"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ConfigureMCP(cfg, "shodan", MCPSpec{Type: "local", Command: []string{"npx", "pkg"}}); err != nil {
		t.Fatalf("ConfigureMCP: %v", err)
	}
	data, _ := os.ReadFile(cfg)
	if !gjson.GetBytes(data, "mcp.engram").Exists() {
		t.Error("existing mcp.engram was lost")
	}
	if !gjson.GetBytes(data, "mcp.shodan").Exists() {
		t.Error("mcp.shodan was not added")
	}
}

func TestConfiguredMCP(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "opencode.json")
	if err := os.WriteFile(cfg, []byte(`{"mcp":{"shodan":{"type":"local","command":["x"]},"engram":{"type":"local","command":["y"]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	names, err := ConfiguredMCP(cfg)
	if err != nil {
		t.Fatalf("ConfiguredMCP: %v", err)
	}
	if len(names) != 2 || names[0] != "engram" || names[1] != "shodan" {
		t.Errorf("names = %v, want [engram shodan] (sorted)", names)
	}

	// A missing file is not an error.
	if got, err := ConfiguredMCP(filepath.Join(t.TempDir(), "none.json")); err != nil || got != nil {
		t.Errorf("missing file: got (%v, %v), want (nil, nil)", got, err)
	}
}
