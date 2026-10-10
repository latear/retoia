package installtui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/retoia/retoia-cli/internal/config"
	"github.com/retoia/retoia-cli/internal/gateway"
	"github.com/retoia/retoia-cli/internal/keystore"
)

// gatewayAccount is the keystore account under which the validated server key
// is remembered (the same account internal/onboard uses).
const gatewayAccount = "gateway"

// keyPrompt is the instruction shown above the masked input.
const keyPrompt = "Enter your Reto IA gateway server key. Leave empty to keep the current one."

// keyState is the phase of the server-key page.
type keyState int

const (
	keyAsk        keyState = iota // accepting a masked key
	keyValidating                 // waiting on gateway.ValidateKey
)

// keyValidatedMsg carries a gateway.ValidateKey outcome back into Update.
// fromCurrent marks a best-effort validation of the stored key (empty submit),
// whose failure is tolerated instead of surfaced as an error.
type keyValidatedMsg struct {
	models      []config.Model
	err         error
	key         string
	fromCurrent bool
}

// keyModel is the Bubble Tea model for page 2: the server-key gate.
type keyModel struct {
	base       string
	input      textinput.Model
	current    string // the stored key, if any
	hasCurrent bool

	state   keyState
	errLine string

	// results
	models      []config.Model
	keepCurrent bool
	keyStored   bool
	storedCount int
	offline     bool
	storeErr    error // non-nil when persisting the validated key failed
	back        bool  // Esc: return to page 1
	cancel      bool  // Ctrl+C: abort
	quitting    bool
	width       int
	height      int
}

// keyStoreWarning returns a user-facing notice when the validated gateway key
// could not be persisted (empty when err is nil), so a save failure is surfaced
// rather than leaving the user to discover later that nothing was stored.
func keyStoreWarning(err error) string {
	if err == nil {
		return ""
	}
	return "\n⚠ The gateway key was validated but could NOT be saved (" + err.Error() + ").\n" +
		"  It won't persist — later runs and the sandbox container won't find it.\n" +
		"  Work around it: export RETOIA_API_KEY=<your key> before re-running."
}

// newKeyModel builds page 2 with a focused, masked input, reading any stored
// key so an empty submit can keep (and reuse) it.
func newKeyModel(base string) keyModel {
	ti := textinput.New()
	ti.Placeholder = "sk-... (blank = keep current)"
	ti.EchoMode = textinput.EchoPassword
	ti.Focus()

	current, _ := keystore.Get(gatewayAccount) // ErrNotFound yields ""
	return keyModel{
		base:       base,
		input:      ti,
		current:    current,
		hasCurrent: current != "",
		state:      keyAsk,
	}
}

func (m keyModel) Init() tea.Cmd { return textinput.Blink }

func (m keyModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if w := cardContentWidth(msg.Width) - 2; w > 0 {
			m.input.Width = w
		}
		return m, nil

	case keyValidatedMsg:
		if msg.fromCurrent {
			// Empty submit with a stored key: keep it regardless of outcome.
			m.keepCurrent = true
			if msg.err == nil {
				m.models = msg.models
			} else {
				m.offline = true // proceed offline; pickers fall back to defaults
			}
			m.quitting = true
			return m, tea.Quit
		}
		// A freshly entered key.
		if msg.err == nil {
			// A storage failure must not block the user, but it is surfaced (the
			// key won't persist, so later runs / the sandbox container won't find
			// it) instead of being silently swallowed.
			if err := keystore.Set(gatewayAccount, msg.key); err != nil {
				m.storeErr = err
			} else {
				m.keyStored = true
			}
			m.models = msg.models
			m.storedCount = len(msg.models)
			m.quitting = true
			return m, tea.Quit
		}
		if errors.Is(msg.err, gateway.ErrInvalidKey) {
			m.errLine = "That key was rejected by the gateway. Check it and try again."
		} else {
			m.errLine = "Can't reach the gateway at " + m.base + ". Retry, or submit empty to keep the current key and continue offline."
		}
		m.state = keyAsk
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
			if m.state == keyAsk {
				m.back = true
				m.quitting = true
				return m, tea.Quit
			}
		case tea.KeyEnter:
			if m.state == keyAsk {
				key := strings.TrimSpace(m.input.Value())
				if key == "" {
					if m.hasCurrent {
						// Best-effort: validate the stored key to list models.
						m.state = keyValidating
						m.errLine = ""
						return m, validateKeyCmd(m.base, m.current, true)
					}
					// Nothing stored and nothing entered: continue with no key.
					m.keepCurrent = true
					m.offline = true
					m.quitting = true
					return m, tea.Quit
				}
				m.state = keyValidating
				m.errLine = ""
				return m, validateKeyCmd(m.base, key, false)
			}
		}
	}

	// Forward everything else to the input, but only while asking, so keystrokes
	// cannot mutate the field mid-validation.
	if m.state == keyAsk {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m keyModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder
	b.WriteString(keyPrompt)
	if m.hasCurrent {
		b.WriteString("\n" + mutedStyle.Render("Current: "+keystore.Mask(m.current)))
	}
	b.WriteString("\n\n" + m.input.View())

	switch m.state {
	case keyValidating:
		b.WriteString("\n\n" + mutedStyle.Render("Validating with the gateway…"))
	default:
		if m.errLine != "" {
			b.WriteString("\n\n" + warnStyle.Render(m.errLine))
		}
	}

	breadcrumb := mutedStyle.Render("Step 2 of 2 · unlocks your authorized models")
	card := renderCard(m.width, "server key", breadcrumb, b.String())
	return screen(m.width, m.height, card, renderFooter("↵ submit (empty = keep current) · esc back · ^c quit"))
}

// validateKeyCmd runs gateway.ValidateKey off the update loop and delivers its
// result as a keyValidatedMsg.
func validateKeyCmd(base, key string, fromCurrent bool) tea.Cmd {
	return func() tea.Msg {
		models, err := gateway.ValidateKey(base, key)
		return keyValidatedMsg{models: models, err: err, key: key, fromCurrent: fromCurrent}
	}
}

// runKeyPage runs page 2 and returns the collected key outcome.
func runKeyPage(base string) (keyResult, error) {
	final, err := tea.NewProgram(newKeyModel(base), tea.WithAltScreen()).Run()
	if err != nil {
		return keyResult{}, fmt.Errorf("running server-key page: %w", err)
	}
	km, ok := final.(keyModel)
	if !ok {
		return keyResult{}, fmt.Errorf("server-key page returned an unexpected model type %T", final)
	}
	return keyResult{
		models:      km.models,
		keepCurrent: km.keepCurrent,
		keyStored:   km.keyStored,
		storedCount: km.storedCount,
		offline:     km.offline,
		storeErr:    km.storeErr,
		back:        km.back,
		cancel:      km.cancel,
	}, nil
}
