package installtui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/retoia/retoia-cli/internal/config"
	"github.com/retoia/retoia-cli/internal/tui"
)

// pickerStep identifies which of the two lists (category, then model) is shown.
type pickerStep int

const (
	stepCategory pickerStep = iota
	stepModel
)

// categoryItem / modelItem adapt config types to bubbles/list.Item. Both
// satisfy list.DefaultItem, which is all the shared pickerDelegate needs.
type categoryItem struct{ group config.ModelGroup }

func (i categoryItem) Title() string {
	return fmt.Sprintf("%s (%d)", i.group.Category, len(i.group.Models))
}
func (i categoryItem) Description() string { return "" }
func (i categoryItem) FilterValue() string { return i.group.Category }

type modelItem struct{ model config.Model }

func (i modelItem) Title() string       { return i.model.ID }
func (i modelItem) Description() string { return i.model.Name }
func (i modelItem) FilterValue() string { return i.model.ID }

// agentPickResult is the outcome of one agent's picker: a chosen model id, a
// skip (Esc: keep the default), or a quit (Ctrl+C: abort the whole install).
type agentPickResult struct {
	id   string
	skip bool
	quit bool
}

// agentPickerModel is the Bubble Tea model for one agent's category -> model
// drill-down, styled with the shared card UI.
type agentPickerModel struct {
	agentName    string
	defaultModel string
	groups       []config.ModelGroup

	step        pickerStep
	catList     list.Model
	modList     list.Model
	chosenGroup config.ModelGroup

	result   agentPickResult
	quitting bool
	width    int
	height   int
	listRows int
}

// newAgentPicker builds the picker for one agent, preselecting the category and
// model that match its DefaultModel when present.
func newAgentPicker(agentName, defaultModel string, groups []config.ModelGroup) agentPickerModel {
	catItems := make([]list.Item, len(groups))
	preCat := 0
	for i, g := range groups {
		catItems[i] = categoryItem{group: g}
		for _, mm := range g.Models {
			if mm.ID == defaultModel {
				preCat = i
			}
		}
	}

	catList := list.New(catItems, tui.NewPickerDelegate(), 0, 0)
	catList.Select(preCat)
	modList := list.New(nil, tui.NewPickerDelegate(), 0, 0)
	for _, l := range []*list.Model{&catList, &modList} {
		l.SetShowTitle(false)
		l.SetShowStatusBar(false)
		l.SetShowHelp(false)
	}

	return agentPickerModel{
		agentName:    agentName,
		defaultModel: defaultModel,
		groups:       groups,
		catList:      catList,
		modList:      modList,
	}
}

// enterModelStep rebuilds the model list for the chosen category, preselecting
// the agent's default model when it lives in that category.
func (m *agentPickerModel) enterModelStep(g config.ModelGroup) {
	m.chosenGroup = g
	items := make([]list.Item, len(g.Models))
	pre := 0
	for i, mm := range g.Models {
		items[i] = modelItem{model: mm}
		if mm.ID == m.defaultModel {
			pre = i
		}
	}
	m.modList.SetItems(items)
	m.modList.Select(pre)
	m.step = stepModel
}

func (m agentPickerModel) Init() tea.Cmd { return nil }

func (m agentPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		contentWidth := cardContentWidth(msg.Width)
		_, cardV := cardStyle.GetFrameSize()
		// Reserve the card frame, its header + breadcrumb + blank spacer (3),
		// and the footer line printed below the card (1).
		listHeight := msg.Height - cardV - 3 - 1
		if listHeight > maxListRows {
			listHeight = maxListRows
		}
		if listHeight < 3 {
			listHeight = 3
		}
		m.listRows = listHeight
		m.catList.SetSize(contentWidth, listHeight)
		m.modList.SetSize(contentWidth, listHeight)
		return m, nil

	case tea.KeyMsg:
		filtering := (m.step == stepCategory && m.catList.FilterState() == list.Filtering) ||
			(m.step == stepModel && m.modList.FilterState() == list.Filtering)

		if !filtering {
			switch msg.String() {
			case "ctrl+c":
				m.result = agentPickResult{quit: true}
				m.quitting = true
				return m, tea.Quit
			case "esc":
				switch m.step {
				case stepModel:
					m.step = stepCategory
					return m, nil
				default: // stepCategory: keep the default model, move on.
					m.result = agentPickResult{skip: true}
					m.quitting = true
					return m, tea.Quit
				}
			case "enter":
				switch m.step {
				case stepCategory:
					if it, ok := m.catList.SelectedItem().(categoryItem); ok {
						m.enterModelStep(it.group)
					}
					return m, nil
				default: // stepModel
					if it, ok := m.modList.SelectedItem().(modelItem); ok {
						m.result = agentPickResult{id: it.model.ID}
						m.quitting = true
						return m, tea.Quit
					}
					return m, nil
				}
			}
		}
	}

	var cmd tea.Cmd
	switch m.step {
	case stepCategory:
		m.catList, cmd = m.catList.Update(msg)
	default:
		m.modList, cmd = m.modList.Update(msg)
	}
	return m, cmd
}

// breadcrumb renders "Category › Model" with the active step accented.
func (m agentPickerModel) breadcrumb() string {
	cat := "Category"
	if m.chosenGroup.Category != "" {
		cat = m.chosenGroup.Category
	}
	segs := [2]string{cat, "Model"}

	rendered := make([]string, len(segs))
	for i, s := range segs {
		switch {
		case pickerStep(i) == m.step:
			rendered[i] = boldAccentStyle.Render(s)
		case pickerStep(i) < m.step:
			rendered[i] = plainStyle.Render(s)
		default:
			rendered[i] = mutedStyle.Render(s)
		}
	}
	return strings.Join(rendered, " › ")
}

func (m agentPickerModel) footerText() string {
	if (m.step == stepCategory && m.catList.FilterState() == list.Filtering) ||
		(m.step == stepModel && m.modList.FilterState() == list.Filtering) {
		return "type to filter · esc clear"
	}
	if m.step == stepCategory {
		return "↑↓ move · ↵ select · esc keep default · ^c quit"
	}
	return "↑↓ move · ↵ select · esc back · ^c quit"
}

func (m agentPickerModel) View() string {
	if m.quitting {
		return ""
	}
	var body string
	switch m.step {
	case stepCategory:
		body = m.catList.View()
	default:
		body = m.modList.View()
	}
	// Fixed body height so the box does not stretch between the category and
	// model steps; the list scrolls internally.
	if m.listRows > 0 {
		body = lipgloss.NewStyle().Height(m.listRows).Render(body)
	}
	card := renderCard(m.width, "agent: "+m.agentName, m.breadcrumb(), body)
	return screen(m.width, m.height, card, renderFooter(m.footerText()))
}
