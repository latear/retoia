package installtui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// key sends one key to the model and returns the updated model.
func key(m editionModel, s string) editionModel {
	var msg tea.KeyMsg
	switch s {
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	next, _ := m.Update(msg)
	return next.(editionModel)
}

func TestEditionDefaultsToLocal(t *testing.T) {
	m := key(editionModel{}, "enter")
	if !m.chosen || m.choice != editionLocal {
		t.Errorf("default enter should choose Local; chosen=%v choice=%v", m.chosen, m.choice)
	}
}

func TestEditionSelectSandbox(t *testing.T) {
	m := key(key(editionModel{}, "down"), "enter")
	if !m.chosen || m.choice != editionSandbox {
		t.Errorf("down then enter should choose Sandbox; chosen=%v choice=%v", m.chosen, m.choice)
	}
}

func TestEditionCursorClamps(t *testing.T) {
	// Up at the top stays at Local; down past the end stays at Sandbox.
	m := key(editionModel{}, "up")
	if m.cursor != 0 {
		t.Errorf("cursor went above 0: %d", m.cursor)
	}
	m = key(key(key(editionModel{}, "down"), "down"), "down")
	if m.cursor != len(editionOptions)-1 {
		t.Errorf("cursor went past the end: %d", m.cursor)
	}
}

func TestEditionCancel(t *testing.T) {
	m := key(editionModel{}, "esc")
	if !m.cancel {
		t.Error("esc should cancel")
	}
}
