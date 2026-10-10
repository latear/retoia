// Package onboard implements the mandatory server-key gate for the
// `caldensec models` command. It runs a small Bubble Tea state machine that
// asks for the CaldenSec gateway server key, validates it against the gateway,
// and only then returns the set of authorized models to the caller.
//
// Security invariant: no model name is ever rendered, logged, or returned
// before the server key has been entered AND validated. The gate never holds
// any model data until gateway.ValidateKey succeeds, so its view cannot leak
// one even by accident.
package onboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ficopix/caldensec/internal/config"
	"github.com/ficopix/caldensec/internal/gateway"
	"github.com/ficopix/caldensec/internal/keystore"
	"github.com/ficopix/caldensec/internal/tui"
	"github.com/ficopix/caldensec/internal/urlseal"
)

// Small local palette for the gate's own bordered "card", matching the
// models picker's look (internal/tui/styles.go) without importing it: same
// accent/border/muted values, kept self-contained since this package has no
// other reason to depend on internal/tui.
var (
	gateBlack  = lipgloss.Color("0")
	gateRed    = lipgloss.Color("9")  // the CaldenSec wordmark
	gateAccent = lipgloss.Color("10") // green: border
	gateWhite  = lipgloss.Color("15") // body text
	gateMuted  = lipgloss.Color("245")
)

var (
	gateCardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(gateAccent).
			Background(gateBlack).
			Foreground(gateWhite).
			Padding(0, 1)

	gateTitleAppStyle = lipgloss.NewStyle().Bold(true).Foreground(gateRed)
	gateTitleSubStyle = lipgloss.NewStyle().Foreground(gateWhite)
	gateStatusStyle   = lipgloss.NewStyle().Foreground(gateMuted)
	gateErrorStyle    = lipgloss.NewStyle().Foreground(gateRed)
	gateFooterStyle   = lipgloss.NewStyle().Foreground(gateMuted)
)

// gateMinCardWidth and gateCardMargin bound the card's width against the
// terminal width, mirroring internal/tui's sizing so both screens feel the
// same width at a given terminal size.
const (
	gateMinCardWidth = 34
	gateMaxCardWidth = 64
	gateCardMargin   = 4
	// gateCardHPadding is the total horizontal padding of gateCardStyle.
	gateCardHPadding = 2
)

// gateCardContentWidth returns the width available inside the card (border
// and padding subtracted) for a given terminal width.
func gateCardContentWidth(termWidth int) int {
	outer := termWidth - gateCardMargin
	if outer > gateMaxCardWidth {
		outer = gateMaxCardWidth
	}
	if outer < gateMinCardWidth {
		outer = gateMinCardWidth
	}
	h, _ := gateCardStyle.GetFrameSize()
	w := outer - h
	if w < 1 {
		w = 1
	}
	return w
}

// gatewayAccount is the keystore account under which the validated server key
// is remembered, and from which it is later read back.
const gatewayAccount = "gateway"

// unlockEnv holds the passphrase that opens the sealed gateway URL baked into
// the build (see sealed.go). It is a shared secret the distributor hands to
// whoever is allowed to install.
const unlockEnv = "CALDENSEC_UNLOCK"

// ErrNoGateway is returned by ResolveBase when no gateway URL is configured at
// all: no --gateway flag, no $CALDENSEC_BASE, and no sealed URL baked in.
// ErrLocked is returned when a sealed URL is baked in but no passphrase was
// provided in $CALDENSEC_UNLOCK to open it.
var (
	ErrNoGateway = errors.New("no gateway URL configured (pass --gateway or set $CALDENSEC_BASE)")
	ErrLocked    = errors.New("gateway URL is sealed; set $CALDENSEC_UNLOCK with the install passphrase")
)

// ErrCancelled is returned by Run when the user cancels the server-key gate
// (Ctrl+C or Esc) before a key is validated. The caller treats this as a
// clean, no-op exit, not a failure.
var ErrCancelled = errors.New("onboarding cancelled")

// rejectedMessage is shown when the gateway explicitly rejects the entered key.
const rejectedMessage = "That key was rejected by the gateway. Check it and try again."

// promptLine is the instruction shown above the masked input.
const promptLine = "Enter your CaldenSec server key (sk-...) to continue."

// unreachableMessage is shown when the gateway cannot be reached. It names the
// base URL so the user can spot a wrong host, and never references any model.
func unreachableMessage(base string) string {
	return fmt.Sprintf("Can't reach the gateway at %s. Check the URL / your network.", base)
}

// ResolveBase returns the gateway base URL, applying precedence: the --gateway
// flag wins, then $CALDENSEC_BASE, then the sealed URL baked into the build
// (opened with the passphrase in $CALDENSEC_UNLOCK). It returns ErrNoGateway
// when nothing is configured, ErrLocked when a sealed URL needs a passphrase
// that was not provided, and wraps urlseal.ErrWrongPassphrase when the
// passphrase is wrong.
func ResolveBase(flagVal, envVal string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	if envVal != "" {
		return envVal, nil
	}
	return UnsealBase(os.Getenv(unlockEnv))
}

// Sealed reports whether a sealed gateway URL is baked into this build. When it
// is, callers with no --gateway/$CALDENSEC_BASE must obtain a passphrase (from
// $CALDENSEC_UNLOCK or an interactive prompt) and call UnsealBase.
func Sealed() bool { return sealedBase != "" }

// UnsealBase opens the baked-in sealed gateway URL with passphrase. It returns
// ErrNoGateway when nothing is sealed, ErrLocked for an empty passphrase, and
// wraps urlseal.ErrWrongPassphrase when the passphrase is wrong.
func UnsealBase(passphrase string) (string, error) {
	if sealedBase == "" {
		return "", ErrNoGateway
	}
	if passphrase == "" {
		return "", ErrLocked
	}
	url, err := urlseal.Open(sealedBase, passphrase)
	if err != nil {
		return "", fmt.Errorf("opening sealed gateway URL: %w", err)
	}
	return url, nil
}

// UnsealUpdateBase opens the baked-in sealed update endpoint URL with passphrase.
// An empty slot (or empty passphrase) returns "" with no error — the update base
// is optional (it falls back to $CALDENSEC_UPDATE_BASE, then the gateway host).
func UnsealUpdateBase(passphrase string) (string, error) {
	if sealedUpdateBase == "" || passphrase == "" {
		return "", nil
	}
	url, err := urlseal.Open(sealedUpdateBase, passphrase)
	if err != nil {
		return "", fmt.Errorf("opening sealed update URL: %w", err)
	}
	return url, nil
}

// UnsealAgents opens the baked-in suggested agent→model templates (calden,
// cloud, and both — the combined full-access mix) with passphrase. Each is
// (nil) when its slot is empty. It returns ErrLocked for an empty passphrase
// when a slot is present, and wraps urlseal.ErrWrongPassphrase on a bad one.
func UnsealAgents(passphrase string) (calden, cloud, both map[string]string, err error) {
	if calden, err = unsealAgentSlot(sealedAgentsCalden, passphrase); err != nil {
		return nil, nil, nil, err
	}
	if cloud, err = unsealAgentSlot(sealedAgentsCloud, passphrase); err != nil {
		return nil, nil, nil, err
	}
	if both, err = unsealAgentSlot(sealedAgentsBoth, passphrase); err != nil {
		return nil, nil, nil, err
	}
	return calden, cloud, both, nil
}

func unsealAgentSlot(blob, passphrase string) (map[string]string, error) {
	if blob == "" {
		return nil, nil
	}
	if passphrase == "" {
		return nil, ErrLocked
	}
	raw, err := urlseal.Open(blob, passphrase)
	if err != nil {
		return nil, fmt.Errorf("opening sealed template: %w", err)
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, fmt.Errorf("parsing sealed template: %w", err)
	}
	return m, nil
}

// ResolveServerKey returns the server key to use without prompting: the env
// value wins when non-empty, otherwise the stored key is read via fromStore.
// Any store error (including keystore.ErrNotFound) yields an empty string, so
// the caller can decide to launch the interactive gate. fromStore is injected
// so this stays testable without a real keyring.
func ResolveServerKey(env string, fromStore func(string) (string, error)) string {
	if env != "" {
		return env
	}
	key, err := fromStore(gatewayAccount)
	if err != nil {
		return ""
	}
	return key
}

// onboardState is the phase of the server-key gate.
type onboardState int

const (
	stateAskKey onboardState = iota
	stateValidating
)

// validationResultMsg carries the outcome of a gateway.ValidateKey call back
// into the Bubble Tea update loop.
type validationResultMsg struct {
	models []config.Model
	err    error
}

// gateModel is the Bubble Tea model for the server-key gate.
type gateModel struct {
	base         string
	input        textinput.Model
	state        onboardState
	errLine      string
	submittedKey string

	// models is populated only after a successful validation, immediately
	// before the program quits. It is never rendered by View.
	models   []config.Model
	runErr   error // set to ErrCancelled on a clean cancel
	quitting bool
	width    int
	height   int
}

// newModel builds the initial gate model with a focused, masked input.
func newModel(base string) gateModel {
	ti := textinput.New()
	ti.Placeholder = "sk-..."
	ti.EchoMode = textinput.EchoPassword
	ti.Focus()
	return gateModel{base: base, input: ti, state: stateAskKey}
}

// Init satisfies tea.Model; it starts the input cursor blinking.
func (m gateModel) Init() tea.Cmd { return textinput.Blink }

// Update advances the state machine. In ask-key it accepts a masked key and,
// on Enter, moves to validating; Ctrl+C or Esc cancels. In validating it waits
// for the gateway result: success remembers the key and quits with the
// authorized models, any failure returns to ask-key with a distinct message
// and never surfaces model data.
func (m gateModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if w := gateCardContentWidth(msg.Width) - 2; w > 0 {
			m.input.Width = w
		}
		return m, nil

	case validationResultMsg:
		if msg.err == nil {
			// Remember the validated key for next time. A storage error must
			// not block the user from proceeding, so it is deliberately ignored.
			_ = keystore.Set(gatewayAccount, m.submittedKey)
			m.models = msg.models
			m.quitting = true
			return m, tea.Quit
		}
		if errors.Is(msg.err, gateway.ErrInvalidKey) {
			m.errLine = rejectedMessage
		} else {
			m.errLine = unreachableMessage(m.base)
		}
		m.state = stateAskKey
		m.input.SetValue("")
		m.input.Focus()
		return m, textinput.Blink

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			m.runErr = ErrCancelled
			m.quitting = true
			return m, tea.Quit
		case tea.KeyEsc:
			if m.state == stateAskKey {
				m.runErr = ErrCancelled
				m.quitting = true
				return m, tea.Quit
			}
		case tea.KeyEnter:
			if m.state == stateAskKey {
				key := strings.TrimSpace(m.input.Value())
				if key == "" {
					m.errLine = "Please enter a server key."
					return m, nil
				}
				m.submittedKey = key
				m.state = stateValidating
				m.errLine = ""
				return m, validateCmd(m.base, key)
			}
		}
	}

	// Forward everything else to the text input, but only while asking for the
	// key so keystrokes cannot mutate the field mid-validation.
	if m.state == stateAskKey {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

// View renders the gate's own bordered card: the same look as the models
// picker (rounded border, "CaldenSec · " title), but with no breadcrumb and
// no model data whatsoever. It has no access to model data and therefore
// cannot display any model name — see the security invariant in the
// package doc comment.
func (m gateModel) View() string {
	if m.quitting {
		return ""
	}

	header := gateTitleAppStyle.Render("CaldenSec") + gateTitleSubStyle.Render(" · server key")

	var b strings.Builder
	b.WriteString(header + "\n\n")
	b.WriteString(promptLine + "\n\n")
	b.WriteString(m.input.View())

	switch m.state {
	case stateValidating:
		b.WriteString("\n\n" + gateStatusStyle.Render("Validating with the gateway..."))
	default:
		if m.errLine != "" {
			b.WriteString("\n\n" + gateErrorStyle.Render(m.errLine))
		}
	}

	card := gateCardStyle.Width(gateCardContentWidth(m.width) + gateCardHPadding).Render(b.String())
	footer := gateFooterStyle.Render("↵ submit · esc cancel")
	return tui.Screen(m.width, m.height, card+"\n"+footer)
}

// validateCmd runs gateway.ValidateKey off the update loop and delivers its
// result as a validationResultMsg.
func validateCmd(base, key string) tea.Cmd {
	return func() tea.Msg {
		models, err := gateway.ValidateKey(base, key)
		return validationResultMsg{models: models, err: err}
	}
}

// Run launches the interactive server-key gate against base and blocks until
// the key is validated or the user cancels. On success it returns the
// authorized models (already persisted for reuse). On a clean cancel it
// returns ErrCancelled. It never returns model data on failure.
func Run(base string) ([]config.Model, error) {
	p := tea.NewProgram(newModel(base), tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("running server-key gate: %w", err)
	}

	gm, ok := final.(gateModel)
	if !ok {
		return nil, fmt.Errorf("server-key gate returned an unexpected model type")
	}
	if gm.runErr != nil {
		return nil, gm.runErr
	}
	return gm.models, nil
}
