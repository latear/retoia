package installer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tidwall/gjson"
)

func TestSetAutoupdate(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "opencode.json")
	if err := os.WriteFile(cfg, []byte(`{"$schema":"x","provider":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetAutoupdate(cfg, false); err != nil {
		t.Fatalf("SetAutoupdate: %v", err)
	}
	data, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !gjson.ValidBytes(data) {
		t.Fatal("config is no longer valid JSON")
	}
	av := gjson.GetBytes(data, "autoupdate")
	if !av.Exists() || av.Bool() {
		t.Errorf("autoupdate should be false; got %q", av.Raw)
	}
	// The pre-existing content is preserved.
	if gjson.GetBytes(data, "$schema").String() != "x" {
		t.Error("unrelated keys should be preserved")
	}
}

func TestSetAutoupdateRejectsInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "opencode.json")
	if err := os.WriteFile(cfg, []byte(`not json`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetAutoupdate(cfg, false); err == nil {
		t.Error("expected an error on invalid JSON input")
	}
}
