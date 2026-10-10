package installtui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestKeyStoreWarning(t *testing.T) {
	if keyStoreWarning(nil) != "" {
		t.Error("nil error should yield no warning")
	}
	w := keyStoreWarning(errors.New("keychain denied"))
	for _, want := range []string{"NOT be saved", "keychain denied", "RETOIA_API_KEY"} {
		if !strings.Contains(w, want) {
			t.Errorf("warning missing %q: %q", want, w)
		}
	}
}

func TestSummaryShowsKeyStoreError(t *testing.T) {
	m := summaryModel{res: applyResult{keyStoreErr: errors.New("keychain denied")}}
	um, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 50})
	view := um.(summaryModel).View()
	if !strings.Contains(view, "NOT saved") {
		t.Errorf("summary should flag the key store failure; got:\n%s", view)
	}
}
