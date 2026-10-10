package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/retoia/retoia-cli/internal/config"
)

// AddProviderOption is one selectable provider in the "add custom" flow. A
// zero-value BaseURL AND empty ID marks the "Custom (OpenAI-compatible)" entry:
// the user types the provider id and base URL themselves.
type AddProviderOption struct {
	ID      string
	Name    string
	BaseURL string
}

// isCustom reports the free-form "enter your own" entry.
func (o AddProviderOption) isCustom() bool { return o.ID == "" && o.BaseURL == "" }

// FetchFunc fetches a provider's models given its base URL and API key.
// SaveFunc persists the provider (block + models) and its key.
type (
	FetchFunc func(baseURL, key string) ([]config.Model, error)
	SaveFunc  func(providerID, name, baseURL, key string, models []config.Model) error
)

type addState int

const (
	addPickProvider addState = iota
	addForm
	addChecking
	addDone
)

// submitMsg carries the outcome of the fetch+save step.
type submitMsg struct {
	models []config.Model
	err    error
}

type addModel struct {
	options []AddProviderOption
	fetch   FetchFunc
	save    SaveFunc

	state  addState
	cursor int // provider list
	chosen AddProviderOption
	custom bool // the chosen entry is the free-form one

	id   textinput.Model // provider id (custom only)
	base textinput.Model
	key  textinput.Model
	fpos int // focus index into the active fields

	errLine string
	count   int    // models added, for the done screen
	addedID string // provider id used

	added    string // display name added (result), "" if cancelled
	cancel   bool
	quitting bool
	width    int
	height   int
}

func newAddModel(options []AddProviderOption, fetch FetchFunc, save SaveFunc) addModel {
	id := textinput.New()
	id.Placeholder = "provider id (e.g. myapi)"
	base := textinput.New()
	base.Placeholder = "https://…/v1"
	key := textinput.New()
	key.Placeholder = "sk-…"
	key.EchoMode = textinput.EchoPassword
	return addModel{options: options, fetch: fetch, save: save, state: addPickProvider, id: id, base: base, key: key}
}

// fields returns the focusable inputs for the current form (id only when custom).
func (m *addModel) fields() []*textinput.Model {
	if m.custom {
		return []*textinput.Model{&m.id, &m.base, &m.key}
	}
	return []*textinput.Model{&m.base, &m.key}
}

// focusField focuses the field at index i and blurs the rest.
func (m *addModel) focusField(i int) tea.Cmd {
	fs := m.fields()
	if i < 0 {
		i = 0
	}
	if i >= len(fs) {
		i = len(fs) - 1
	}
	m.fpos = i
	for j, f := range fs {
		if j == i {
			f.Focus()
		} else {
			f.Blur()
		}
	}
	return textinput.Blink
}

func (m addModel) Init() tea.Cmd { return textinput.Blink }

func (m addModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if w := cardContentWidth(msg.Width) - 2; w > 0 {
			m.id.Width, m.base.Width, m.key.Width = w, w, w
		}
		return m, nil

	case submitMsg:
		if msg.err != nil {
			m.errLine = msg.err.Error()
			m.state = addForm
			return m, textinput.Blink
		}
		m.count = len(msg.models)
		m.state = addDone
		return m, nil

	case tea.KeyMsg:
		switch m.state {
		case addPickProvider:
			return m.updatePick(msg)
		case addForm:
			return m.updateForm(msg)
		case addChecking:
			return m, nil // ignore keys while fetching
		case addDone:
			m.quitting = true
			return m, tea.Quit
		}
	}

	if m.state == addForm {
		fs := m.fields()
		var cmd tea.Cmd
		*fs[m.fpos], cmd = fs[m.fpos].Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m addModel) updatePick(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "esc", "q":
		m.cancel = true
		m.quitting = true
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.options)-1 {
			m.cursor++
		}
	case "enter":
		m.chosen = m.options[m.cursor]
		m.custom = m.chosen.isCustom()
		m.id.SetValue("")
		m.base.SetValue(m.chosen.BaseURL)
		m.key.SetValue("")
		m.errLine = ""
		m.state = addForm
		// Land on the first empty field: id for custom, else the key.
		start := 0
		if !m.custom {
			start = 1 // base is prefilled
		}
		return m, m.focusField(start)
	}
	return m, nil
}

func (m addModel) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		m.cancel = true
		m.quitting = true
		return m, tea.Quit
	case tea.KeyEsc:
		m.state = addPickProvider // back to the provider list
		return m, nil
	case tea.KeyTab, tea.KeyDown:
		return m, m.focusField((m.fpos + 1) % len(m.fields()))
	case tea.KeyShiftTab, tea.KeyUp:
		n := len(m.fields())
		return m, m.focusField((m.fpos - 1 + n) % n)
	case tea.KeyEnter:
		pid := m.chosen.ID
		name := m.chosen.Name
		if m.custom {
			pid = sanitizeProviderID(m.id.Value())
			name = pid
			if pid == "" {
				m.errLine = "Enter a provider id (letters, digits, - or _)."
				return m, nil
			}
		}
		base := strings.TrimSpace(m.base.Value())
		key := strings.TrimSpace(m.key.Value())
		if base == "" || key == "" {
			m.errLine = "Enter both the base URL and the API key."
			return m, nil
		}
		m.addedID = pid
		m.added = name
		m.errLine = ""
		m.state = addChecking
		return m, submitCmd(m.fetch, m.save, pid, name, base, key)
	}

	fs := m.fields()
	var cmd tea.Cmd
	*fs[m.fpos], cmd = fs[m.fpos].Update(msg)
	return m, cmd
}

// sanitizeProviderID keeps a safe single-token id (lowercase alnum, - and _).
func sanitizeProviderID(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func submitCmd(fetch FetchFunc, save SaveFunc, id, name, base, key string) tea.Cmd {
	return func() tea.Msg {
		models, err := fetch(base, key)
		if err != nil {
			return submitMsg{err: err}
		}
		if len(models) == 0 {
			return submitMsg{err: fmt.Errorf("the key is valid but returned no models")}
		}
		if err := save(id, name, base, key, models); err != nil {
			return submitMsg{err: err}
		}
		return submitMsg{models: models}
	}
}

func (m addModel) View() string {
	if m.quitting {
		return ""
	}
	var body, footer, crumb string
	switch m.state {
	case addPickProvider:
		crumb = "Add custom · pick a provider"
		for i, o := range m.options {
			cur := "  "
			label := normalItemStyle.Render(o.Name)
			if i == m.cursor {
				cur = selectedPrefixStyle.Render("❯") + " "
				label = selectedItemStyle.Render(o.Name)
			}
			hint := o.BaseURL
			if o.isCustom() {
				hint = "enter your own id + base URL"
			}
			body += cur + label + mutedItemStyle.Render("  "+hint) + "\n"
		}
		footer = "↑↓ move · ↵ choose · esc back"
	case addForm:
		crumb = "Add custom · " + m.chosen.Name
		if m.custom {
			body = "Provider id\n" + m.id.View() + "\n\n"
		}
		body += "Base URL\n" + m.base.View() + "\n\nAPI key\n" + m.key.View()
		if m.errLine != "" {
			body += "\n\n" + warnStyle.Render(m.errLine)
		}
		footer = "↵ fetch models · tab switch field · esc back"
	case addChecking:
		crumb = "Add custom · " + m.chosen.Name
		body = mutedStyle.Render("Fetching models…")
		footer = "please wait"
	case addDone:
		crumb = "Add custom · done"
		body = okStyle.Render(fmt.Sprintf("✓ added %s — %d models", m.added, m.count)) +
			mutedStyle.Render("\n\nThey now show as their own category in \"Change models\".")
		footer = "↵ back to menu"
	}
	card := renderCard(m.width, "models", breadcrumbActiveStyle.Render(crumb), body)
	stacked := card + "\n\n" + renderFooter(footer)
	if b := Banner(m.width); b != "" {
		stacked = b + "\n\n" + stacked
	}
	return renderScreen(m.width, m.height, stacked)
}

// RunAddCustom shows the add-custom-provider flow. It returns the name of the
// provider added (empty if the user cancelled without adding one).
func RunAddCustom(options []AddProviderOption, fetch FetchFunc, save SaveFunc) (string, error) {
	final, err := tea.NewProgram(newAddModel(options, fetch, save), tea.WithAltScreen()).Run()
	if err != nil {
		return "", fmt.Errorf("running add-custom flow: %w", err)
	}
	am, ok := final.(addModel)
	if !ok {
		return "", fmt.Errorf("unexpected add-custom model type %T", final)
	}
	return am.added, nil
}
