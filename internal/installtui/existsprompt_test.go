package installtui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestExistsPrompt(t *testing.T) {
	for key, want := range map[string]string{"o": "open", "r": "rebuild", "esc": "cancel", "q": "cancel"} {
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
