package installtui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// sandboxExtras are the opt-in pieces of the sandbox image/config the user picks
// before the build. All default on.
type sandboxExtras struct {
	tor      bool // install tor/torsocks/proxychains4 + run the daemon
	engram   bool // the persistent-memory MCP
	context7 bool // the library-docs MCP
	plugins  bool // the OpenCode sidebar plugins
}

func defaultSandboxExtras() sandboxExtras {
	return sandboxExtras{tor: true, engram: true, context7: true, plugins: true}
}

const sandboxExtrasCount = 4

// sandboxExtrasModel is the toggle page shown before a sandbox build.
type sandboxExtrasModel struct {
	ex            sandboxExtras
	cursor        int
	cancel        bool
	quitting      bool
	width, height int
}

func (m sandboxExtrasModel) Init() tea.Cmd { return nil }

func (m *sandboxExtrasModel) toggle(i int) {
	switch i {
	case 0:
		m.ex.tor = !m.ex.tor
	case 1:
		m.ex.engram = !m.ex.engram
	case 2:
		m.ex.context7 = !m.ex.context7
	case 3:
		m.ex.plugins = !m.ex.plugins
	}
}

type extrasRow struct {
	label, desc string
	on          bool
}

func (m sandboxExtrasModel) rows() []extrasRow {
	return []extrasRow{
		{"Tor", "torsocks / proxychains4 — anonymized egress", m.ex.tor},
		{"Engram", "persistent-memory MCP", m.ex.engram},
		{"Context7", "live library/API docs MCP", m.ex.context7},
		{"Sidebar plugins", "sub-agent monitor + task panel", m.ex.plugins},
	}
}

func (m sandboxExtrasModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			m.cancel, m.quitting = true, true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case "down", "j":
			if m.cursor < sandboxExtrasCount { // last index is the Continue row
				m.cursor++
			}
			return m, nil
		case " ", "x":
			if m.cursor < sandboxExtrasCount {
				m.toggle(m.cursor)
			}
			return m, nil
		case "enter":
			if m.cursor == sandboxExtrasCount { // Continue row
				m.quitting = true
				return m, tea.Quit
			}
			m.toggle(m.cursor) // Enter on a toggle flips it, like space
			return m, nil
		}
	}
	return m, nil
}

func (m sandboxExtrasModel) View() string {
	if m.quitting {
		return ""
	}
	var b strings.Builder
	b.WriteString(mutedStyle.Render("Pick what goes into the sandbox (all on by default):") + "\n\n")
	for i, r := range m.rows() {
		cursor := "  "
		if i == m.cursor {
			cursor = accentStyle.Render("❯ ")
		}
		box := mutedStyle.Render("[ ]")
		if r.on {
			box = okStyle.Render("[x]")
		}
		label := plainStyle.Render(r.label)
		if i == m.cursor {
			label = boldAccentStyle.Render(r.label)
		}
		b.WriteString(cursor + box + " " + label + mutedStyle.Render("  "+r.desc) + "\n")
	}

	// Continue button (last, selectable row).
	contCursor := "  "
	contLabel := plainStyle.Render("Continue →")
	if m.cursor == sandboxExtrasCount {
		contCursor = accentStyle.Render("❯ ")
		contLabel = boldAccentStyle.Render("Continue →")
	}
	b.WriteString("\n" + contCursor + "    " + contLabel + "\n")

	card := renderCard(m.width, "sandbox", mutedStyle.Render("Sandbox · extras"), b.String())
	return screen(m.width, m.height, card, renderFooter("↑↓ move · space toggle · ↵ select · esc cancel"))
}

// runSandboxExtras shows the toggle page and returns the chosen extras (and
// whether the user cancelled).
func runSandboxExtras() (sandboxExtras, bool, error) {
	final, err := tea.NewProgram(sandboxExtrasModel{ex: defaultSandboxExtras()}, tea.WithAltScreen()).Run()
	if err != nil {
		return sandboxExtras{}, false, fmt.Errorf("running sandbox extras page: %w", err)
	}
	m := final.(sandboxExtrasModel)
	return m.ex, m.cancel, nil
}
