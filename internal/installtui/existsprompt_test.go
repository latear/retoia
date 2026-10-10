package installtui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestExistsPrompt(t *testing.T) {
	for key, want := range map[string]string{"o": "open", "r": "rebuild", "d": "delete", "esc": "cancel", "q": "cancel"} {
		m, _ := existsPromptModel{running: true}.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
		var km tea.KeyMsg
		if key == "esc" {
			km = tea.KeyMsg{Type: tea.KeyEsc}
		} else {
			km = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
		}
		nm, _ := m.(existsPromptModel).Update(km)
		if got := nm.(existsPromptModel).choice; got != want {
			t.Errorf("key %q → choice %q, want %q", key, got, want)
		}
	}
	// It shows the banner + the "already" notice.
	v, _ := existsPromptModel{running: true}.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	view := v.(existsPromptModel).View()
	if !strings.Contains(view, "already") || !strings.Contains(view, bannerFragment) {
		t.Error("exists prompt should show the 'already' notice and the banner")
	}
	if !strings.Contains(view, "delete") {
		t.Error("exists prompt should list the delete option")
	}
}

func TestExistsPromptShowsOutdated(t *testing.T) {
	m := existsPromptModel{running: true, builtVersion: "v0.4.5", currentVersion: "v0.4.8", age: "2d ago", outdated: true}
	um, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	view := um.(existsPromptModel).View()
	for _, want := range []string{"v0.4.5", "2d ago", "outdated", "v0.4.8"} {
		if !strings.Contains(view, want) {
			t.Errorf("outdated prompt view missing %q", want)
		}
	}
}

func TestConfirmDataModel(t *testing.T) {
	cases := []struct {
		key           string
		del, canceled bool
	}{{"y", true, false}, {"n", false, false}, {"esc", false, true}}
	for _, c := range cases {
		km := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(c.key)}
		if c.key == "esc" {
			km = tea.KeyMsg{Type: tea.KeyEsc}
		}
		nm, _ := confirmDataModel{}.Update(km)
		got := nm.(confirmDataModel)
		if got.deleteData != c.del || got.cancelled != c.canceled {
			t.Errorf("key %q → delete=%v cancelled=%v, want %v/%v", c.key, got.deleteData, got.cancelled, c.del, c.canceled)
		}
	}
}

func TestScaffoldPresent(t *testing.T) {
	d := t.TempDir()
	if scaffoldPresent(d) {
		t.Fatal("empty dir must not count as a scaffold")
	}
	_ = os.WriteFile(filepath.Join(d, "compose.sandbox.yaml"), nil, 0o644)
	if scaffoldPresent(d) {
		t.Fatal("compose alone is not enough")
	}
	_ = os.Mkdir(filepath.Join(d, "opencode-config"), 0o755)
	if !scaffoldPresent(d) {
		t.Fatal("compose + opencode-config should count")
	}
}
