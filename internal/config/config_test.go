package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tidwall/gjson"
)

const sampleConfigRelPath = "../../testdata/opencode.sample.json"

func TestManagedAgentsExcludesNonRetoiasecAndIncludesRetoiaAgents(t *testing.T) {
	cfg, err := Load(sampleConfigRelPath, "retoia")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	agents := cfg.ManagedAgents()

	names := make(map[string]Agent, len(agents))
	for _, a := range agents {
		names[a.Name] = a
	}

	if _, ok := names["gentle-orchestrator"]; ok {
		t.Fatalf("expected gentle-orchestrator to be excluded, got agents: %+v", agents)
	}

	wantNames := []string{"Retoia-Orchestrator", "cs-recon", "cs-reviewer"}
	if len(agents) != len(wantNames) {
		t.Fatalf("expected %d managed agents, got %d: %+v", len(wantNames), len(agents), agents)
	}
	for _, name := range wantNames {
		if _, ok := names[name]; !ok {
			t.Fatalf("expected managed agent %q, not found in %+v", name, agents)
		}
	}

	// cs-recon references a model (model-b) absent from the models
	// list; it must still be captured with its raw current model id.
	recon := names["cs-recon"]
	if recon.CurrentModel != "model-b" {
		t.Fatalf("expected cs-recon current model %q, got %q", "model-b", recon.CurrentModel)
	}
	if recon.Mode != "subagent" {
		t.Fatalf("expected cs-recon mode %q, got %q", "subagent", recon.Mode)
	}
}

func TestAvailableModelsListsAllProviderModels(t *testing.T) {
	cfg, err := Load(sampleConfigRelPath, "retoia")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	models := cfg.AvailableModels()
	if len(models) != 3 {
		t.Fatalf("expected 3 available models, got %d: %+v", len(models), models)
	}

	byID := make(map[string]string, len(models))
	for _, m := range models {
		byID[m.ID] = m.Name
	}

	want := map[string]string{
		"model-a": "Model A",
		"model-c": "Model C",
		"auto":    "Auto",
	}
	for id, name := range want {
		if got, ok := byID[id]; !ok || got != name {
			t.Fatalf("expected model %q name %q, got %q (present=%v)", id, name, got, ok)
		}
	}
}

func TestSetAgentModelChangesOnlyOneValueAndBacksUp(t *testing.T) {
	original, err := os.ReadFile(sampleConfigRelPath)
	if err != nil {
		t.Fatalf("reading sample config: %v", err)
	}

	tmpDir := t.TempDir()
	copyPath := filepath.Join(tmpDir, "opencode.json")
	if err := os.WriteFile(copyPath, original, 0o644); err != nil {
		t.Fatalf("writing temp copy: %v", err)
	}

	if err := SetAgentModel(copyPath, "retoia", "cs-recon", "model-a"); err != nil {
		t.Fatalf("SetAgentModel() error = %v", err)
	}

	updated, err := os.ReadFile(copyPath)
	if err != nil {
		t.Fatalf("reading updated config: %v", err)
	}

	if !gjson.ValidBytes(updated) {
		t.Fatalf("updated config is not valid JSON")
	}

	// The only change must be cs-recon's model.
	gotModel := gjson.GetBytes(updated, "agent.cs-recon.model").String()
	if gotModel != "retoia/model-a" {
		t.Fatalf("expected agent.cs-recon.model = %q, got %q", "retoia/model-a", gotModel)
	}

	cfg, err := Load(copyPath, "retoia")
	if err != nil {
		t.Fatalf("Load() on updated config error = %v", err)
	}
	agents := cfg.ManagedAgents()
	for _, a := range agents {
		switch a.Name {
		case "cs-recon":
			if a.CurrentModel != "model-a" {
				t.Fatalf("cs-recon current model changed unexpectedly: %q", a.CurrentModel)
			}
		case "Retoia-Orchestrator":
			if a.CurrentModel != "model-a" {
				t.Fatalf("Retoia-Orchestrator current model should be unchanged, got %q", a.CurrentModel)
			}
		case "cs-reviewer":
			if a.CurrentModel != "model-c" {
				t.Fatalf("cs-reviewer current model should be unchanged, got %q", a.CurrentModel)
			}
		}
	}

	backupPath := copyPath + BackupSuffix
	backup, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("expected backup file at %q: %v", backupPath, err)
	}
	if string(backup) != string(original) {
		t.Fatalf("backup content does not match original file")
	}
}

func TestSetAgent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	orig := []byte(`{"provider":{"retoia":{}}}`)
	if err := os.WriteFile(path, orig, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SetAgent(path, "retoia", "primary", "Reto IA — principal agent.", "retoia", "auto", "PROMPT"); err != nil {
		t.Fatalf("SetAgent() error = %v", err)
	}
	data, _ := os.ReadFile(path)
	if !gjson.ValidBytes(data) {
		t.Fatal("config is not valid JSON after SetAgent")
	}
	for p, want := range map[string]string{
		"agent.retoia.mode":        "primary",
		"agent.retoia.description": "Reto IA — principal agent.",
		"agent.retoia.model":       "retoia/auto",
		"agent.retoia.prompt":      "PROMPT",
	} {
		if got := gjson.GetBytes(data, p).String(); got != want {
			t.Errorf("%s = %q, want %q", p, got, want)
		}
	}
	if !gjson.GetBytes(data, "provider.retoia").Exists() {
		t.Error("existing provider block was lost")
	}
	if bak, err := os.ReadFile(path + BackupSuffix); err != nil || string(bak) != string(orig) {
		t.Errorf("backup missing or differs: %v", err)
	}

	// Empty prompt: no prompt key is written.
	np := filepath.Join(dir, "noprompt.json")
	if err := os.WriteFile(np, orig, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetAgent(np, "retoia", "primary", "d", "retoia", "auto", ""); err != nil {
		t.Fatal(err)
	}
	npData, _ := os.ReadFile(np)
	if !gjson.ValidBytes(npData) || gjson.GetBytes(npData, "agent.retoia.prompt").Exists() {
		t.Error("empty prompt must write no prompt key and keep valid JSON")
	}

	// Invalid input config: the write fails validation and the original is restored.
	bad := filepath.Join(dir, "bad.json")
	garbage := []byte(`{not json`)
	if err := os.WriteFile(bad, garbage, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetAgent(bad, "retoia", "primary", "d", "retoia", "auto", ""); err == nil {
		t.Error("expected an error for invalid JSON config")
	}
	if got, _ := os.ReadFile(bad); string(got) != string(garbage) {
		t.Errorf("original not restored: %q", got)
	}
}
