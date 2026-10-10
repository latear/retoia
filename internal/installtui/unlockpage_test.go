package installtui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ficopix/caldensec/internal/urlseal"
)

func TestUnlockPage_Success(t *testing.T) {
	m := newUnlockModel(func(pass string) (string, error) {
		return "https://gateway.example/v1", nil
	})
	updated, _ := m.Update(unlockedMsg{base: "https://gateway.example/v1"})
	um := updated.(unlockModel)
	if !um.quitting {
		t.Error("model did not quit after a successful unlock")
	}
	if um.base != "https://gateway.example/v1" {
		t.Errorf("base = %q, want the unlocked URL", um.base)
	}
	if um.cancel {
		t.Error("cancel should be false on success")
	}
}

func TestUnlockPage_WrongPassphraseRetries(t *testing.T) {
	m := newUnlockModel(func(string) (string, error) { return "", urlseal.ErrWrongPassphrase })
	m.state = unlockChecking
	updated, _ := m.Update(unlockedMsg{err: urlseal.ErrWrongPassphrase})
	um := updated.(unlockModel)
	if um.quitting {
		t.Error("model should not quit on a wrong passphrase")
	}
	if um.state != unlockAsk {
		t.Error("model should return to the ask state to retry")
	}
	if um.errLine == "" {
		t.Error("expected an error line after a wrong passphrase")
	}
}

func TestUnlockPage_ViewShowsPrompt(t *testing.T) {
	m := newUnlockModel(func(string) (string, error) { return "", nil })
	m.width = 72
	if !strings.Contains(m.View(), "passphrase") {
		t.Error("unlock view does not mention the passphrase")
	}
}

// TestUnlockPage_EmptyEnterSkips covers the step-0 UX: pressing Enter with no
// passphrase continues without a gateway (skip), rather than blocking.
func TestUnlockPage_EmptyEnterSkips(t *testing.T) {
	m := newUnlockModel(func(string) (string, error) {
		t.Fatal("unlock must not be called on an empty submit")
		return "", nil
	})
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	um := updated.(unlockModel)
	if !um.skip || !um.quitting {
		t.Errorf("empty Enter: skip=%v quitting=%v, want both true", um.skip, um.quitting)
	}
	if um.cancel {
		t.Error("empty Enter should skip, not cancel")
	}
}

// TestUnlockPage_EscSkips covers Esc also skipping (continue without gateway).
func TestUnlockPage_EscSkips(t *testing.T) {
	m := newUnlockModel(func(string) (string, error) { return "", nil })
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	um := updated.(unlockModel)
	if !um.skip || um.cancel {
		t.Errorf("Esc: skip=%v cancel=%v, want skip true, cancel false", um.skip, um.cancel)
	}
}
