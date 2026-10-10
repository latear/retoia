package installtui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSandboxExtras(t *testing.T) {
	m := sandboxExtrasModel{ex: defaultSandboxExtras()}
	if !(m.ex.engram && m.ex.context7 && m.ex.plugins) {
		t.Fatal("defaults should all be on")
	}
	// space toggles the cursor row (0 = Engram)
	um, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")})
	if um.(sandboxExtrasModel).ex.engram {
		t.Error("space should toggle Engram off")
	}
	// enter on a toggle flips it (does not finish); navigate down to the Continue
	// row and enter there finishes without cancel.
	var nav tea.Model = um
	for i := 0; i < sandboxExtrasCount; i++ {
		nav, _ = nav.(sandboxExtrasModel).Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	fm, _ := nav.(sandboxExtrasModel).Update(tea.KeyMsg{Type: tea.KeyEnter})
	if fm.(sandboxExtrasModel).cancel {
		t.Error("Continue should finish, not cancel")
	}
	if !fm.(sandboxExtrasModel).quitting {
		t.Error("Continue should finish (quitting)")
	}
	// esc cancels
	cm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !cm.(sandboxExtrasModel).cancel {
		t.Error("esc should cancel")
	}
	// view shows the rows + banner
	vm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	v := vm.(sandboxExtrasModel).View()
	for _, want := range []string{"Engram", "Context7", "Continue", bannerFragment} {
		if !strings.Contains(v, want) {
			t.Errorf("extras view missing %q", want)
		}
	}
}
