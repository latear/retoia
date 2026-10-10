package installtui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestSetupPageShowsCoreToggles verifies the install page renders the Context7
// and Engram rows (the two default MCPs).
func TestSetupPageShowsCoreToggles(t *testing.T) {
	m := newSetupModel(true, true, true)
	um, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	view := um.(setupModel).View()
	for _, want := range []string{"Context7", "Engram"} {
		if !strings.Contains(view, want) {
			t.Errorf("install page view missing %q", want)
		}
	}
}
