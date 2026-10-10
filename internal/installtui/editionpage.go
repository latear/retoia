package installtui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// edition is the install target chosen on the step-0 selector.
type edition int

const (
	editionLocal   edition = iota // install into the host's opencode.json
	editionSandbox                // scaffold a single isolated Docker container
)

type editionOption struct {
	label string
	desc  string
}

var editionOptions = []editionOption{
	{"Local", "Install into this host's OpenCode (~/.config/opencode). Fastest, but touches your existing OpenCode."},
	{"Sandbox (Docker)", "Scaffold one isolated container with OpenCode + the Retoia suite + Kali tools inside. Your host OpenCode is never touched."},
}

// editionModel is the Bubble Tea model for the step-0 edition selector.
type editionModel struct {
	cursor   int
	choice   edition
	chosen   bool
	cancel   bool
	quitting bool
	width    int
	height   int
}

func (m editionModel) Init() tea.Cmd { return nil }

func (m editionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
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
			if m.cursor < len(editionOptions)-1 {
				m.cursor++
			}
		case "enter":
			m.choice = edition(m.cursor)
			m.chosen = true
			m.quitting = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m editionModel) View() string {
	if m.quitting {
		return ""
	}
	var b strings.Builder
	b.WriteString("Where do you want to install Reto IA?\n\n")
	for i, opt := range editionOptions {
		marker := mutedStyle.Render("  ")
		label := opt.label
		if i == m.cursor {
			marker = accentStyle.Render("▸ ")
			label = boldAccentStyle.Render(label)
		}
		b.WriteString(marker + label + "\n")
		b.WriteString("    " + mutedStyle.Render(opt.desc) + "\n")
		if i < len(editionOptions)-1 {
			b.WriteString("\n")
		}
	}
	breadcrumb := mutedStyle.Render("Step 0 · edition")
	card := renderCard(m.width, "edition", breadcrumb, b.String())
	return screen(m.width, m.height, card, renderFooter("↑/↓ move · ↵ select · esc quit"))
}

// runEditionPage runs the step-0 edition selector.
func runEditionPage() (editionModel, error) {
	final, err := tea.NewProgram(editionModel{}, tea.WithAltScreen()).Run()
	if err != nil {
		return editionModel{}, fmt.Errorf("running edition page: %w", err)
	}
	em, ok := final.(editionModel)
	if !ok {
		return editionModel{}, fmt.Errorf("edition page returned an unexpected model type %T", final)
	}
	return em, nil
}
