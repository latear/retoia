package installtui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tidwall/gjson"

	"github.com/ficopix/caldensec/internal/config"
)

// TestApplyInstallFreshConfig covers a clean machine where opencode.json does
// not exist yet: apply() must create it and end up with the gateway
// provider's baseURL and its (validated) models.
func TestApplyInstallFreshConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OPENCODE_AUTH_PATH", filepath.Join(home, "auth.json"))
	t.Setenv("CALDENSEC_API_KEY", "sk-fresh")

	cfg := filepath.Join(home, ".config", "opencode", "opencode.json") // does NOT exist
	setup := setupResult{}                                             // no extras
	key := keyResult{models: []config.Model{{ID: "model-a", Name: "Orca"}, {ID: "cloud-b", Name: "GLM"}}}

	res, err := apply(cfg, "caldensec", "https://gateway.example/v1", "", setup, key)
	if err != nil {
		t.Fatalf("apply on a fresh machine: %v", err)
	}
	_ = res

	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatalf("config was not created: %v", err)
	}
	if !gjson.ValidBytes(data) {
		t.Fatal("created config is not valid JSON")
	}
	if got := gjson.GetBytes(data, "provider.caldensec.options.baseURL").String(); got != "https://gateway.example/v1" {
		t.Errorf("baseURL = %q, want the gateway", got)
	}
	if !gjson.GetBytes(data, "provider.caldensec.models.model-a").Exists() {
		t.Error("gateway provider models were not written (OpenCode could not route)")
	}
}

// TestApplyInstallEndToEnd drives the real install core, apply(), against
// throwaway copies (never the user's real config): a temp HOME, temp auth
// store, and the sample opencode.json fixture. It exercises the exact code the
// TUI runs after its pages close — set the provider base URL, write the
// OpenCode credential, configure Engram and Context7 as MCP servers — then
// asserts the resulting files.
func TestApplyInstallEndToEnd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Redirect every destination into temp; provide the key via env so the
	// credential write does not touch the real keychain or the gateway.
	authPath := filepath.Join(home, "auth.json")
	t.Setenv("OPENCODE_AUTH_PATH", authPath)
	t.Setenv("CALDENSEC_API_KEY", "sk-integration-test-key")

	// A temp copy of the sample config (never the real ~/.config file).
	sample, err := os.ReadFile("../../testdata/opencode.sample.json")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	cfg := filepath.Join(home, "opencode.json")
	if err := os.WriteFile(cfg, sample, 0o644); err != nil {
		t.Fatal(err)
	}

	const base = "https://gateway.example/v1"
	setup := setupResult{installEngram: true, installContext7: true}
	key := keyResult{keepCurrent: true}

	res, err := apply(cfg, "caldensec", base, "", setup, key)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}

	data, _ := os.ReadFile(cfg)
	if !gjson.ValidBytes(data) {
		t.Fatal("resulting opencode.json is not valid JSON")
	}

	// Provider base URL points at the gateway, not any stale host.
	if got := gjson.GetBytes(data, "provider.caldensec.options.baseURL").String(); got != base {
		t.Errorf("baseURL = %q, want %q", got, base)
	}
	// Engram: when the engram binary is available it is registered as a local
	// MCP server; when it is not (e.g. CI without engram or Homebrew), the run
	// degrades to a skip reason instead of failing. Assert whichever happened,
	// so the test is independent of the host environment.
	if res.engramConfigured {
		if got := gjson.GetBytes(data, "mcp.engram.command.1").String(); got != "mcp" {
			t.Errorf("mcp.engram not configured (command[1]=%q)", got)
		}
		if got := gjson.GetBytes(data, "mcp.engram.type").String(); got != "local" {
			t.Errorf("mcp.engram.type = %q, want local", got)
		}
	} else if res.engramSkipReason == "" {
		t.Error("engram neither configured nor skipped with a reason")
	}
	// Context7 docs MCP wired.
	if !res.context7Configured {
		t.Error("context7 was not configured")
	} else if got := gjson.GetBytes(data, "mcp.context7.type").String(); got != "remote" {
		t.Errorf("mcp.context7.type = %q, want remote", got)
	}

	// OpenCode credential written to the temp auth store with the env key.
	authData, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("auth store not written: %v", err)
	}
	if got := gjson.GetBytes(authData, "caldensec.key").String(); got != "sk-integration-test-key" {
		t.Errorf("auth key = %q, want the env key", got)
	}

	// Report what happened, visible under `go test -v`.
	t.Logf("provider baseURL:    %s (set=%v)", res.baseURL, res.baseURLSet)
	t.Logf("credential:          set=%v provider=%s", res.credentialSet, res.credentialProvider)
	t.Logf("context7 configured: %v", res.context7Configured)
	t.Logf("engram configured:   %v path=%s (chosen=%v)", res.engramConfigured, res.engramPath, res.engramChosen)

	// Render the final screen so the "Next steps" commands are exercised.
	view := summaryModel{res: res, width: 80}.View()
	for _, want := range []string{"Next steps", "caldensec models", "caldensec key", "restart OpenCode", "Engram"} {
		if !strings.Contains(view, want) {
			t.Errorf("summary is missing %q", want)
		}
	}
	t.Logf("\n----- final install screen -----\n%s", view)
}
