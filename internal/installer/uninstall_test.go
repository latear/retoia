package installer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tidwall/gjson"
)

func TestDeleteConfigPaths(t *testing.T) {
	cfg := copySample(t)
	// Seed a couple of things to remove alongside the sample's provider.
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(AddProvider(cfg, "retoia", "https://gw/v1", nil))
	must(ConfigureEngramMCP(cfg, "/opt/homebrew/bin/engram"))

	paths := []string{
		"agent.Retoia-Orchestrator",
		"provider.retoia", "mcp.engram",
		"agent.does-not-exist", // skipped
	}
	n, err := DeleteConfigPaths(cfg, paths)
	if err != nil {
		t.Fatalf("DeleteConfigPaths: %v", err)
	}
	if n != 3 {
		t.Errorf("removed %d, want 3", n)
	}
	data, _ := os.ReadFile(cfg)
	if !gjson.ValidBytes(data) {
		t.Fatal("result is not valid JSON")
	}
	for _, p := range []string{"agent.Retoia-Orchestrator", "provider.retoia", "mcp.engram"} {
		if gjson.GetBytes(data, p).Exists() {
			t.Errorf("%s still present after removal", p)
		}
	}
	if _, err := os.Stat(cfg + uninstallBackupSuffix); err != nil {
		t.Errorf("backup not created: %v", err)
	}
}

func TestDeleteConfigPathsMissingFile(t *testing.T) {
	n, err := DeleteConfigPaths(filepath.Join(t.TempDir(), "nope.json"), []string{"agent.X"})
	if err != nil || n != 0 {
		t.Errorf("missing file: got (%d, %v), want (0, nil)", n, err)
	}
}

func TestRemoveOpenCodeCredential(t *testing.T) {
	authPath := filepath.Join(t.TempDir(), "auth.json")
	seed := `{"openai":{"type":"api","key":"sk-o"},"retoia":{"type":"api","key":"sk-c"}}`
	if err := os.WriteFile(authPath, []byte(seed), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RemoveOpenCodeCredential(authPath, "retoia"); err != nil {
		t.Fatalf("RemoveOpenCodeCredential: %v", err)
	}
	data, _ := os.ReadFile(authPath)
	if gjson.GetBytes(data, "retoia").Exists() {
		t.Error("retoia credential still present")
	}
	if !gjson.GetBytes(data, "openai").Exists() {
		t.Error("openai credential was wrongly removed")
	}
	// No-op when absent.
	if err := RemoveOpenCodeCredential(authPath, "retoia"); err != nil {
		t.Errorf("second removal should be a no-op, got %v", err)
	}
}
