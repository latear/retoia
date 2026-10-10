package installer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tidwall/gjson"
)

func TestSetProviderBaseURL(t *testing.T) {
	cfg := copySample(t)
	const want = "https://gateway.example/v1"

	if err := SetProviderBaseURL(cfg, "retoia", want); err != nil {
		t.Fatalf("SetProviderBaseURL: %v", err)
	}

	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !gjson.ValidBytes(data) {
		t.Fatal("result is not valid JSON")
	}
	if got := gjson.GetBytes(data, "provider.retoia.options.baseURL").String(); got != want {
		t.Errorf("baseURL = %q, want %q", got, want)
	}
	// Existing provider models survive.
	if !gjson.GetBytes(data, "provider.retoia.models.model-a").Exists() {
		t.Error("existing provider models were lost")
	}
	if _, err := os.Stat(cfg + baseURLBackupSuffix); err != nil {
		t.Errorf("backup not created: %v", err)
	}
}

func TestSetOpenCodeCredential_NewFile(t *testing.T) {
	authPath := filepath.Join(t.TempDir(), "share", "opencode", "auth.json")

	if err := SetOpenCodeCredential(authPath, "retoia", "sk-newkey123"); err != nil {
		t.Fatalf("SetOpenCodeCredential: %v", err)
	}

	fi, err := os.Stat(authPath)
	if err != nil {
		t.Fatalf("auth file not created: %v", err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("auth file perms = %o, want 600", perm)
	}

	data, _ := os.ReadFile(authPath)
	if got := gjson.GetBytes(data, "retoia.type").String(); got != "api" {
		t.Errorf("retoia.type = %q, want api", got)
	}
	if got := gjson.GetBytes(data, "retoia.key").String(); got != "sk-newkey123" {
		t.Errorf("retoia.key = %q, want sk-newkey123", got)
	}
}

func TestSetOpenCodeCredential_PreservesOthers(t *testing.T) {
	authPath := filepath.Join(t.TempDir(), "auth.json")
	seed := map[string]any{
		"openai": map[string]string{"type": "api", "key": "sk-openai"},
		"retoia": map[string]string{"type": "api", "key": "sk-old"},
	}
	b, _ := json.Marshal(seed)
	if err := os.WriteFile(authPath, b, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := SetOpenCodeCredential(authPath, "retoia", "sk-fresh"); err != nil {
		t.Fatalf("SetOpenCodeCredential: %v", err)
	}

	data, _ := os.ReadFile(authPath)
	if got := gjson.GetBytes(data, "retoia.key").String(); got != "sk-fresh" {
		t.Errorf("retoia.key = %q, want sk-fresh (overwritten)", got)
	}
	if got := gjson.GetBytes(data, "openai.key").String(); got != "sk-openai" {
		t.Errorf("openai.key = %q, want sk-openai (preserved)", got)
	}
}
