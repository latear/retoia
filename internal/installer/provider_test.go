package installer

import (
	"os"
	"testing"

	"github.com/tidwall/gjson"

	"github.com/ficopix/caldensec/internal/config"
)

func TestEnsureConfig(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/nested/opencode.json"

	// Missing: creates a valid JSON file (and the dir).
	if err := EnsureConfig(path); err != nil {
		t.Fatalf("EnsureConfig (missing): %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("config not created: %v", err)
	}
	if !gjson.ValidBytes(data) {
		t.Errorf("created config is not valid JSON: %s", data)
	}

	// Existing: no-op, does not overwrite.
	if err := os.WriteFile(path, []byte(`{"keep":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureConfig(path); err != nil {
		t.Fatalf("EnsureConfig (existing): %v", err)
	}
	data, _ = os.ReadFile(path)
	if !gjson.GetBytes(data, "keep").Bool() {
		t.Error("EnsureConfig overwrote an existing config")
	}
}

func TestAddProvider(t *testing.T) {
	cfg := copySample(t)
	const base = "https://api.z.ai/api/paas/v4"
	models := []config.Model{
		{ID: "glm-4.6", Name: "GLM 4.6"},           // dot in the key
		{ID: "anthropic/claude-x", Name: "Claude"}, // slash in the key
	}

	if err := AddProvider(cfg, "zai", base, models); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}

	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !gjson.ValidBytes(data) {
		t.Fatal("result is not valid JSON")
	}

	if got := gjson.GetBytes(data, "provider.zai.options.baseURL").String(); got != base {
		t.Errorf("baseURL = %q, want %q", got, base)
	}
	// gjson path: "." in a key is escaped with a backslash; "/" is literal.
	if got := gjson.GetBytes(data, `provider.zai.models.glm-4\.6.name`).String(); got != "GLM 4.6" {
		t.Errorf("glm-4.6 name = %q, want %q", got, "GLM 4.6")
	}
	if got := gjson.GetBytes(data, `provider.zai.models.anthropic/claude-x.name`).String(); got != "Claude" {
		t.Errorf("anthropic/claude-x name = %q, want Claude", got)
	}

	// The pre-existing gateway provider is untouched.
	if !gjson.GetBytes(data, "provider.caldensec.models.model-a").Exists() {
		t.Error("existing provider.caldensec was lost")
	}
	if _, err := os.Stat(cfg + providerBackupSuffix); err != nil {
		t.Errorf("backup not created: %v", err)
	}
}

// TestAddProviderWritesLimits verifies each model gets a limit block (needed by
// OpenCode for context counting / compaction), defaulting when the model carries
// none and honoring the env overrides.
func TestAddProviderWritesLimits(t *testing.T) {
	cfg := copySample(t)
	models := []config.Model{
		{ID: "m-default"}, // no limit → default
		{ID: "m-explicit", ContextLimit: 200000, OutputLimit: 16384}, // explicit
	}
	if err := AddProvider(cfg, "zai", "https://api.z.ai/v1", models); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}
	data, _ := os.ReadFile(cfg)
	if got := gjson.GetBytes(data, "provider.zai.models.m-default.limit.context").Int(); got != defaultContextLimit {
		t.Errorf("default context = %d, want %d", got, defaultContextLimit)
	}
	if got := gjson.GetBytes(data, "provider.zai.models.m-explicit.limit.context").Int(); got != 200000 {
		t.Errorf("explicit context = %d, want 200000", got)
	}
	if got := gjson.GetBytes(data, "provider.zai.models.m-explicit.limit.output").Int(); got != 16384 {
		t.Errorf("explicit output = %d, want 16384", got)
	}

	// Env override applies to models without an explicit limit.
	t.Setenv("CALDENSEC_CONTEXT_LIMIT", "65536")
	cfg2 := copySample(t)
	if err := AddProvider(cfg2, "zai", "https://api.z.ai/v1", []config.Model{{ID: "m"}}); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}
	data2, _ := os.ReadFile(cfg2)
	if got := gjson.GetBytes(data2, "provider.zai.models.m.limit.context").Int(); got != 65536 {
		t.Errorf("env-override context = %d, want 65536", got)
	}
}
