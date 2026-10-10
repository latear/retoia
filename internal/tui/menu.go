package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// MenuChoice is the top-level choice of `caldensec models`.
type MenuChoice int

const (
	MenuQuit      MenuChoice = iota // Esc/Ctrl+C, or nothing chosen
	MenuChange                      // assign models to agents
	MenuAddCustom                   // add a custom model provider
)

type menuItem struct {
	choice MenuChoice
	label  string
	desc   string
}

type menuModel struct {
	items    []menuItem
	cursor   int
	choice   MenuChoice
	quitting bool
	width    int
	height   int
}

func newMenuModel() menuModel {
	return menuModel{
		items: []menuItem{
			{MenuChange, "Change models", "assign a model to each Calden agent"},
			{MenuAddCustom, "Add custom models", "add a provider by API (OpenAI, z.ai, OpenRouter, Kimi…)"},
		},
		choice: MenuQuit,
	}
}

func (m menuModel) Init() tea.Cmd { return nil }

func (m menuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			m.choice = MenuQuit
			m.quitting = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "enter":
			m.choice = m.items[m.cursor].choice
			m.quitting = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m menuModel) View() string {
	if m.quitting {
		return ""
	}
	var body string
	for i, it := range m.items {
		cursor := "  "
		label := normalItemStyle.Render(it.label)
		if i == m.cursor {
			cursor = selectedPrefixStyle.Render("❯") + " "
			label = selectedItemStyle.Render(it.label)
		}
		body += cursor + label + mutedItemStyle.Render("  — "+it.desc)
		if i < len(m.items)-1 {
			body += "\n\n"
		}
	}
	card := renderCard(m.width, "models", breadcrumbActiveStyle.Render("What do you want to do?"), body)
	stacked := card + "\n\n" + renderFooter("↑↓ move · ↵ select · esc quit")
	if b := Banner(m.width); b != "" {
		stacked = b + "\n\n" + stacked
	}
	return renderScreen(m.width, m.height, stacked)
}

// RunMenu shows the top-level menu and returns the user's choice (MenuQuit on
// Esc/Ctrl+C).
func RunMenu() (MenuChoice, error) {
	final, err := tea.NewProgram(newMenuModel(), tea.WithAltScreen()).Run()
	if err != nil {
		return MenuQuit, fmt.Errorf("running menu: %w", err)
	}
	mm, ok := final.(menuModel)
	if !ok {
		return MenuQuit, fmt.Errorf("unexpected menu model type %T", final)
	}
	return mm.choice, nil
}
