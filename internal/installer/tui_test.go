package installer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tidwall/gjson"
)

func TestConfigureTUIPluginsFreshFile(t *testing.T) {
	tui := filepath.Join(t.TempDir(), "tui.json") // does not exist
	added, err := ConfigureTUIPlugins(tui, RetoiaTUIPlugins)
	if err != nil {
		t.Fatalf("ConfigureTUIPlugins: %v", err)
	}
	if len(added) != len(RetoiaTUIPlugins) {
		t.Errorf("added %d, want %d", len(added), len(RetoiaTUIPlugins))
	}
	data, _ := os.ReadFile(tui)
	if !gjson.ValidBytes(data) {
		t.Fatal("not valid JSON")
	}
	if got := gjson.GetBytes(data, "plugin.#").Int(); got != int64(len(RetoiaTUIPlugins)) {
		t.Errorf("plugin count = %d, want %d", got, len(RetoiaTUIPlugins))
	}
	if gjson.GetBytes(data, "plugin.0").String() != "opencode-agents-monitor@0.1.2" {
		t.Errorf("first plugin = %q", gjson.GetBytes(data, "plugin.0").String())
	}
}

func TestConfigureTUIPluginsMergeNoClobber(t *testing.T) {
	tui := filepath.Join(t.TempDir(), "tui.json")
	// Pre-existing tui.json with the user's own plugins, including a different
	// version of agents-monitor and a local path.
	seed := `{"$schema":"https://opencode.ai/tui.json","plugin":["opencode-subagent-statusline","opencode-agents-monitor@0.1.1","/Users/x/gentle-logo.tsx"]}`
	if err := os.WriteFile(tui, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	added, err := ConfigureTUIPlugins(tui, RetoiaTUIPlugins)
	if err != nil {
		t.Fatalf("ConfigureTUIPlugins: %v", err)
	}
	// agents-monitor already present (different version) → not re-added; todo is
	// new → added.
	if len(added) != 1 {
		t.Errorf("added %v, want 1 (todo)", added)
	}
	data, _ := os.ReadFile(tui)
	var got []string
	gjson.GetBytes(data, "plugin").ForEach(func(_, v gjson.Result) bool { got = append(got, v.String()); return true })
	// Existing entries preserved.
	for _, want := range []string{"opencode-subagent-statusline", "opencode-agents-monitor@0.1.1", "/Users/x/gentle-logo.tsx", "opencode-todo@0.0.3"} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
			}
		}
		if !found {
			t.Errorf("missing %q in %v", want, got)
		}
	}
	// The user's agents-monitor version was kept (not overwritten to 0.1.2).
	for _, g := range got {
		if g == "opencode-agents-monitor@0.1.2" {
			t.Error("clobbered the user's agents-monitor version")
		}
	}
}

func TestConfigureTUIPluginsIdempotent(t *testing.T) {
	tui := filepath.Join(t.TempDir(), "tui.json")
	if _, err := ConfigureTUIPlugins(tui, RetoiaTUIPlugins); err != nil {
		t.Fatal(err)
	}
	added, err := ConfigureTUIPlugins(tui, RetoiaTUIPlugins)
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 0 {
		t.Errorf("second run added %v, want none", added)
	}
}

func TestRemoveTUIPlugins(t *testing.T) {
	tui := filepath.Join(t.TempDir(), "tui.json")
	// User's own plugin + ours (agents-monitor at a different version, plus todo).
	seed := `{"$schema":"x","plugin":["opencode-subagent-statusline","opencode-agents-monitor@0.1.1","opencode-todo"]}`
	os.WriteFile(tui, []byte(seed), 0o644)

	n, err := RemoveTUIPlugins(tui, RetoiaTUIPlugins)
	if err != nil {
		t.Fatalf("RemoveTUIPlugins: %v", err)
	}
	if n != 2 {
		t.Errorf("removed %d, want 2 (agents-monitor, todo)", n)
	}
	data, _ := os.ReadFile(tui)
	var got []string
	gjson.GetBytes(data, "plugin").ForEach(func(_, v gjson.Result) bool { got = append(got, v.String()); return true })
	if len(got) != 1 || got[0] != "opencode-subagent-statusline" {
		t.Errorf("kept = %v, want only the user's own plugin", got)
	}
	// Idempotent: a second run removes nothing.
	if n2, _ := RemoveTUIPlugins(tui, RetoiaTUIPlugins); n2 != 0 {
		t.Errorf("second run removed %d, want 0", n2)
	}
}

// TestRetoiaTheme covers shipping, selecting, and removing the red theme.
func TestRetoiaTheme(t *testing.T) {
	dir := t.TempDir()
	path, err := WriteRetoiaTheme(dir)
	if err != nil {
		t.Fatalf("WriteRetoiaTheme: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("theme not written: %v", err)
	}
	if !gjson.ValidBytes(data) {
		t.Fatal("theme JSON is invalid")
	}
	if gjson.GetBytes(data, "theme.primary").String() == "" {
		t.Error("theme.primary missing")
	}

	tuiPath := filepath.Join(dir, "tui.json")
	if err := SetTUITheme(tuiPath, RetoiaThemeName); err != nil {
		t.Fatalf("SetTUITheme: %v", err)
	}
	td, _ := os.ReadFile(tuiPath)
	if got := gjson.GetBytes(td, "theme").String(); got != RetoiaThemeName {
		t.Errorf("tui.json theme = %q, want %q", got, RetoiaThemeName)
	}

	if err := RemoveRetoiaTheme(dir); err != nil {
		t.Fatalf("RemoveRetoiaTheme: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("theme file should be removed")
	}
	td2, _ := os.ReadFile(tuiPath)
	if gjson.GetBytes(td2, "theme").Exists() {
		t.Error("tui.json theme key should be cleared after removal")
	}
}
