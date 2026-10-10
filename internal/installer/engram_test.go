package installer

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/tidwall/gjson"
)

func TestConfigureEngramMCP(t *testing.T) {
	cfg := copySample(t)
	const engram = "/opt/homebrew/bin/engram"

	if err := ConfigureEngramMCP(cfg, engram); err != nil {
		t.Fatalf("ConfigureEngramMCP: %v", err)
	}

	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !gjson.ValidBytes(data) {
		t.Fatal("result is not valid JSON")
	}

	if got := gjson.GetBytes(data, "mcp.engram.command.0").String(); got != engram {
		t.Errorf("command[0] = %q, want %q", got, engram)
	}
	if got := gjson.GetBytes(data, "mcp.engram.command.1").String(); got != "mcp" {
		t.Errorf("command[1] = %q, want mcp", got)
	}
	if got := gjson.GetBytes(data, "mcp.engram.command.2").String(); got != "--tools=agent" {
		t.Errorf("command[2] = %q, want --tools=agent", got)
	}
	if got := gjson.GetBytes(data, "mcp.engram.type").String(); got != "local" {
		t.Errorf("type = %q, want local", got)
	}

	// The pre-existing provider block is untouched.
	if !gjson.GetBytes(data, "provider.caldensec.models.model-a").Exists() {
		t.Error("existing provider block was lost")
	}

	// A backup was created (the shared MCP writer's backup).
	if _, err := os.Stat(cfg + mcpBackupSuffix); err != nil {
		t.Errorf("backup not created: %v", err)
	}
}

func TestConfigureEngramMCP_PreservesExistingMCP(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "opencode.json")
	seed := `{
	  "provider": {"caldensec": {"options": {"baseURL": "https://gateway.example/v1"}}},
	  "mcp": {"context7": {"type": "remote", "url": "https://mcp.context7.com/mcp"}}
	}`
	if err := os.WriteFile(cfg, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ConfigureEngramMCP(cfg, "/usr/local/bin/engram"); err != nil {
		t.Fatalf("ConfigureEngramMCP: %v", err)
	}

	data, _ := os.ReadFile(cfg)
	if got := gjson.GetBytes(data, "mcp.context7.url").String(); got != "https://mcp.context7.com/mcp" {
		t.Errorf("context7 mcp entry was lost: %q", got)
	}
	if got := gjson.GetBytes(data, "mcp.engram.command.0").String(); got != "/usr/local/bin/engram" {
		t.Errorf("engram command[0] = %q", got)
	}
	if got := gjson.GetBytes(data, "provider.caldensec.options.baseURL").String(); got != "https://gateway.example/v1" {
		t.Errorf("provider baseURL was lost: %q", got)
	}
}

func TestResolveEngramPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH executable probing differs on Windows")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "engram")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	got, ok := ResolveEngramPath()
	if !ok {
		t.Fatal("ResolveEngramPath: not found, want found")
	}
	if got != exe {
		t.Errorf("ResolveEngramPath = %q, want %q", got, exe)
	}
}
