package installtui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/retoia/retoia-cli/internal/urlseal"
)

// unlockPrompt is the instruction shown on the step-0 page.
const unlockPrompt = "Enter the Reto IA install passphrase to unlock the gateway.\nLeave it empty and press ↵ to continue without a gateway."

// unlockState is the phase of the step-0 unlock page.
type unlockState int

const (
	unlockAsk      unlockState = iota // accepting a masked passphrase
	unlockChecking                    // opening the sealed URL
)

// unlockedMsg carries the outcome of an unlock attempt back into Update.
type unlockedMsg struct {
	base string
	err  error
}

// unlockModel is the Bubble Tea model for the optional step-0 page: it appears
// only when the gateway URL is sealed and no passphrase came from the
// environment. It asks for the passphrase (masked) and, on success, yields the
// decrypted gateway base URL for the rest of the install.
type unlockModel struct {
	unlock func(string) (string, error)
	input  textinput.Model

	state   unlockState
	errLine string

	base     string // result: the unlocked gateway base URL
	pass     string // the passphrase that unlocked it (to also open the template)
	skip     bool   // Esc: continue without a gateway (install agents/skills/MCPs only)
	cancel   bool   // Ctrl+C: abort the whole install
	quitting bool
	width    int
	height   int
}

// newUnlockModel builds the step-0 page with a focused, masked input. unlock is
// injected (onboard.UnsealBase in production) so this stays testable.
func newUnlockModel(unlock func(string) (string, error)) unlockModel {
	ti := textinput.New()
	ti.Placeholder = "install passphrase"
	ti.EchoMode = textinput.EchoPassword
	ti.Focus()
	return unlockModel{unlock: unlock, input: ti, state: unlockAsk}
}

func (m unlockModel) Init() tea.Cmd { return textinput.Blink }

func (m unlockModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if w := cardContentWidth(msg.Width) - 2; w > 0 {
			m.input.Width = w
		}
		return m, nil

	case unlockedMsg:
		if msg.err == nil {
			m.base = msg.base
			m.quitting = true
			return m, tea.Quit
		}
		if errors.Is(msg.err, urlseal.ErrWrongPassphrase) {
			m.errLine = "Wrong passphrase. Try again."
		} else {
			m.errLine = "Could not unlock the gateway: " + msg.err.Error()
		}
		m.state = unlockAsk
		m.input.SetValue("")
		m.input.Focus()
		return m, textinput.Blink

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			m.cancel = true
			m.quitting = true
			return m, tea.Quit
		case tea.KeyEsc:
			// Continue without a gateway: install agents, skills and MCPs; the
			// gateway and model assignment are left for later (with a reminder).
			m.skip = true
			m.quitting = true
			return m, tea.Quit
		case tea.KeyEnter:
			if m.state == unlockAsk {
				pass := strings.TrimSpace(m.input.Value())
				if pass == "" {
					// No passphrase entered → continue without a gateway (same as
					// Esc): install agents, skills and MCPs, assign models later.
					m.skip = true
					m.quitting = true
					return m, tea.Quit
				}
				m.state = unlockChecking
				m.errLine = ""
				m.pass = pass // remember it to also open the sealed template
				return m, unlockCmd(m.unlock, pass)
			}
		}
	}

	// Forward keystrokes to the input only while asking, so they cannot mutate
	// the field mid-check.
	if m.state == unlockAsk {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m unlockModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder
	b.WriteString(unlockPrompt)
	b.WriteString("\n\n" + m.input.View())

	switch m.state {
	case unlockChecking:
		b.WriteString("\n\n" + mutedStyle.Render("Unlocking…"))
	default:
		if m.errLine != "" {
			b.WriteString("\n\n" + warnStyle.Render(m.errLine))
		}
	}

	breadcrumb := mutedStyle.Render("Step 0 · unlock the gateway")
	card := renderCard(m.width, "unlock", breadcrumb, b.String())
	return screen(m.width, m.height, card, renderFooter("↵ unlock (empty = continue without gateway) · esc skip · ^c quit"))
}

// unlockCmd runs the injected unlock function off the update loop.
func unlockCmd(unlock func(string) (string, error), pass string) tea.Cmd {
	return func() tea.Msg {
		base, err := unlock(pass)
		return unlockedMsg{base: base, err: err}
	}
}

// runUnlockPage runs step 0 and returns the unlocked base URL (or a cancel).
func runUnlockPage(unlock func(string) (string, error)) (unlockModel, error) {
	final, err := tea.NewProgram(newUnlockModel(unlock), tea.WithAltScreen()).Run()
	if err != nil {
		return unlockModel{}, fmt.Errorf("running unlock page: %w", err)
	}
	um, ok := final.(unlockModel)
	if !ok {
		return unlockModel{}, fmt.Errorf("unlock page returned an unexpected model type %T", final)
	}
	return um, nil
}
