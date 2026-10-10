// Package tui implements the Bubble Tea picker used by retoia-models: pick
// a Retoia agent (grouped into Primary / Subagents sections), then drill
// down from a model category to a specific model.
package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/retoia/retoia-cli/internal/config"
)

// step identifies which list is currently shown.
type step int

const (
	stepAgent step = iota
	stepCategory
	stepModel
)

// sectionName groups agents in the sectioned agent list.
const (
	sectionPrimary   = "Primary"
	sectionSubagents = "Subagents"
)

// agentItem adapts config.Agent to bubbles/list.Item.
type agentItem struct {
	agent config.Agent
}

func (i agentItem) Title() string { return i.agent.Name }

func (i agentItem) Description() string {
	return fmt.Sprintf("%s · current: %s", i.agent.Mode, i.agent.CurrentModel)
}

func (i agentItem) FilterValue() string { return i.agent.Name }

// sectionHeaderItem is a non-selectable row that labels a section of the
// agent list. Enter on this item is a no-op and it never becomes a Result.
type sectionHeaderItem struct {
	label string
}

func (i sectionHeaderItem) Title() string       { return sectionHeaderStyle.Render(i.label) }
func (i sectionHeaderItem) Description() string { return "" }
func (i sectionHeaderItem) FilterValue() string { return "" }

// categoryItem adapts a config.ModelGroup to bubbles/list.Item, showing the
// category name and how many models it holds.
type categoryItem struct {
	group config.ModelGroup
}

func (i categoryItem) Title() string {
	return fmt.Sprintf("%s (%d)", i.group.Category, len(i.group.Models))
}
func (i categoryItem) Description() string { return "" }
func (i categoryItem) FilterValue() string { return i.group.Category }

// modelItem adapts config.Model to bubbles/list.Item.
type modelItem struct {
	model config.Model
}

func (i modelItem) Title() string       { return i.model.ID }
func (i modelItem) Description() string { return i.model.Name }
func (i modelItem) FilterValue() string { return i.model.ID }

// Result carries the user's final choice out of the TUI. Confirmed is false
// when the user cancelled via Esc (on step 1) or Ctrl+C.
type Result struct {
	Agent     config.Agent
	Model     config.Model
	Confirmed bool
}

// model is the Bubble Tea program state.
type model struct {
	step         step
	agents       []config.Agent // live copy, updated as models are assigned
	agentList    list.Model
	categoryList list.Model
	modelList    list.Model
	groups       []config.ModelGroup
	chosenAgent  config.Agent
	chosenGroup  config.ModelGroup

	// apply persists one assignment; it is called on each model selection so the
	// user can keep assigning without leaving. applied records what was changed
	// (for the caller's summary) and applyErr aborts the loop on a write error.
	apply    func(agentName, providerID, modelID string) error
	applied  []Result
	applyErr error
	notice   string // transient confirmation shown on the agent step

	quitting bool
	width    int
	height   int
	listRows int
}

// buildAgentItems groups agents into "Primary" (Mode == "primary") and
// "Subagents" sections, each alphabetical by name, and returns the section
// headers plus agent rows in display order.
func buildAgentItems(agents []config.Agent) []list.Item {
	var primary, sub []config.Agent
	for _, a := range agents {
		if a.Mode == "primary" {
			primary = append(primary, a)
		} else {
			sub = append(sub, a)
		}
	}
	sort.Slice(primary, func(i, j int) bool { return primary[i].Name < primary[j].Name })
	sort.Slice(sub, func(i, j int) bool { return sub[i].Name < sub[j].Name })

	items := make([]list.Item, 0, len(agents)+2)
	if len(primary) > 0 {
		items = append(items, sectionHeaderItem{label: sectionPrimary})
		for _, a := range primary {
			items = append(items, agentItem{agent: a})
		}
	}
	if len(sub) > 0 {
		items = append(items, sectionHeaderItem{label: sectionSubagents})
		for _, a := range sub {
			items = append(items, agentItem{agent: a})
		}
	}
	return items
}

// firstAgentIndex returns the index of the first selectable agent row, so the
// initial cursor never rests on a non-selectable section header.
func firstAgentIndex(items []list.Item) int {
	for idx, it := range items {
		if _, ok := it.(agentItem); ok {
			return idx
		}
	}
	return 0
}

// New builds the initial TUI model from the managed agents and available
// models resolved from opencode.json. apply persists one assignment and is
// called on each model selection so the user can keep assigning; it may be nil
// (then selections are only recorded, not written).
func New(agents []config.Agent, models []config.Model, apply func(agentName, providerID, modelID string) error) model {
	live := append([]config.Agent(nil), agents...) // own copy; updated in place
	agentItems := buildAgentItems(live)
	agentList := list.New(agentItems, newPickerDelegate(), 0, 0)
	agentList.Select(firstAgentIndex(agentItems))

	categoryList := list.New(nil, newPickerDelegate(), 0, 0)
	modelList := list.New(nil, newPickerDelegate(), 0, 0)

	for _, l := range []*list.Model{&agentList, &categoryList, &modelList} {
		l.SetShowTitle(false)
		l.SetShowStatusBar(false)
		l.SetShowHelp(false)
	}

	return model{
		step:         stepAgent,
		agents:       live,
		agentList:    agentList,
		categoryList: categoryList,
		modelList:    modelList,
		groups:       config.GroupModelsByCategory(models),
		apply:        apply,
	}
}

// setAgentModel updates the in-memory agent's current model and refreshes the
// agent list so the change is visible, keeping the changed agent selected.
func (m *model) setAgentModel(name, modelID string) {
	for i := range m.agents {
		if m.agents[i].Name == name {
			m.agents[i].CurrentModel = modelID
		}
	}
	items := buildAgentItems(m.agents)
	m.agentList.SetItems(items)
	for idx, it := range items {
		if ai, ok := it.(agentItem); ok && ai.agent.Name == name {
			m.agentList.Select(idx)
			break
		}
	}
}

// enterCategoryStep rebuilds the category list for the chosen agent and
// preselects the category containing that agent's current model.
func (m *model) enterCategoryStep() {
	items := make([]list.Item, len(m.groups))
	preselect := 0
	for idx, g := range m.groups {
		items[idx] = categoryItem{group: g}
		for _, mm := range g.Models {
			if mm.ID == m.chosenAgent.CurrentModel {
				preselect = idx
			}
		}
	}
	m.categoryList.SetItems(items)
	m.categoryList.Select(preselect)
	m.step = stepCategory
}

// enterModelStep rebuilds the model list for the chosen category and
// preselects the agent's current model, when present in that category.
func (m *model) enterModelStep(group config.ModelGroup) {
	m.chosenGroup = group
	items := make([]list.Item, len(group.Models))
	preselect := 0
	for idx, mm := range group.Models {
		items[idx] = modelItem{model: mm}
		if mm.ID == m.chosenAgent.CurrentModel {
			preselect = idx
		}
	}
	m.modelList.SetItems(items)
	m.modelList.Select(preselect)
	m.step = stepModel
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		contentWidth := cardContentWidth(msg.Width)
		_, cardV := cardStyle.GetFrameSize()
		// Reserve the card's own border/padding, its header + breadcrumb +
		// blank spacer lines (3), and the footer line printed below the
		// card (1).
		listHeight := msg.Height - cardV - 3 - 1
		if listHeight > maxListRows {
			listHeight = maxListRows
		}
		if listHeight < 3 {
			listHeight = 3
		}
		m.listRows = listHeight
		m.agentList.SetSize(contentWidth, listHeight)
		m.categoryList.SetSize(contentWidth, listHeight)
		m.modelList.SetSize(contentWidth, listHeight)
		return m, nil

	case tea.KeyMsg:
		// Do not intercept keys while the user is typing into the active
		// list's own filter input.
		filtering := (m.step == stepAgent && m.agentList.FilterState() == list.Filtering) ||
			(m.step == stepCategory && m.categoryList.FilterState() == list.Filtering) ||
			(m.step == stepModel && m.modelList.FilterState() == list.Filtering)

		if !filtering {
			switch msg.String() {
			case "ctrl+c":
				m.quitting = true
				return m, tea.Quit
			case "esc":
				switch m.step {
				case stepModel:
					m.step = stepCategory
					return m, nil
				case stepCategory:
					m.step = stepAgent
					return m, nil
				default:
					m.quitting = true
					return m, tea.Quit
				}
			case "enter":
				switch m.step {
				case stepAgent:
					it, ok := m.agentList.SelectedItem().(agentItem)
					if !ok {
						// Selected row is a section header: no-op.
						return m, nil
					}
					m.chosenAgent = it.agent
					m.enterCategoryStep()
					return m, nil
				case stepCategory:
					if it, ok := m.categoryList.SelectedItem().(categoryItem); ok {
						m.enterModelStep(it.group)
					}
					return m, nil
				default: // stepModel
					if it, ok := m.modelList.SelectedItem().(modelItem); ok {
						// Apply the assignment now (chosenAgent.CurrentModel is the
						// previous model, kept in the Result for the caller's summary).
						if m.apply != nil {
							if err := m.apply(m.chosenAgent.Name, it.model.Provider, it.model.ID); err != nil {
								m.applyErr = err
								m.quitting = true
								return m, tea.Quit
							}
						}
						m.applied = append(m.applied, Result{Agent: m.chosenAgent, Model: it.model, Confirmed: true})
						m.notice = fmt.Sprintf("✓ saved · %s → %s", m.chosenAgent.Name, it.model.ID)
						m.setAgentModel(m.chosenAgent.Name, it.model.ID)
						// Back to the agent list to keep assigning.
						m.chosenAgent = config.Agent{}
						m.chosenGroup = config.ModelGroup{}
						m.step = stepAgent
						return m, nil
					}
					return m, nil
				}
			}
		}
	}

	var cmd tea.Cmd
	switch m.step {
	case stepAgent:
		m.agentList, cmd = m.agentList.Update(msg)
	case stepCategory:
		m.categoryList, cmd = m.categoryList.Update(msg)
	default:
		m.modelList, cmd = m.modelList.Update(msg)
	}
	return m, cmd
}

// breadcrumb renders the "Agent › Category › Model" line. The current step's
// segment is bold+accent; earlier steps show the value already chosen for
// them; later steps show their generic placeholder, muted.
func (m model) breadcrumb() string {
	segments := [3]string{"Agent", "Category", "Model"}
	if m.chosenAgent.Name != "" {
		segments[stepAgent] = m.chosenAgent.Name
	}
	if m.chosenGroup.Category != "" {
		segments[stepCategory] = m.chosenGroup.Category
	}

	rendered := make([]string, len(segments))
	for i, s := range segments {
		switch {
		case step(i) == m.step:
			rendered[i] = breadcrumbActiveStyle.Render(s)
		case step(i) < m.step:
			rendered[i] = breadcrumbDoneStyle.Render(s)
		default:
			rendered[i] = breadcrumbPendingStyle.Render(s)
		}
	}
	return strings.Join(rendered, " › ")
}

// filtering reports whether the active step's list is currently accepting
// filter input, mirroring the guard in Update.
func (m model) filtering() bool {
	switch m.step {
	case stepAgent:
		return m.agentList.FilterState() == list.Filtering
	case stepCategory:
		return m.categoryList.FilterState() == list.Filtering
	default:
		return m.modelList.FilterState() == list.Filtering
	}
}

// footerText returns the dim help line shown below the card, specific to the
// active step (and to whether the user is mid-filter).
func (m model) footerText() string {
	if m.filtering() {
		return "type to filter · esc clear"
	}
	if m.step == stepAgent {
		base := "↑↓ move · ↵ select · esc quit"
		if m.notice != "" {
			return m.notice + "   ·   " + base
		}
		return base
	}
	return "↑↓ move · ↵ select · esc back · ^c quit"
}

func (m model) View() string {
	if m.quitting {
		return ""
	}

	var listView string
	switch m.step {
	case stepAgent:
		listView = m.agentList.View()
	case stepCategory:
		listView = m.categoryList.View()
	default:
		listView = m.modelList.View()
	}
	// Pad the list to a fixed number of rows so the box keeps the same height on
	// every step (agent/category/model) instead of stretching with the count.
	if m.listRows > 0 {
		listView = lipgloss.NewStyle().Height(m.listRows).Render(listView)
	}

	card := renderCard(m.width, "models", m.breadcrumb(), listView)
	stacked := lipgloss.JoinVertical(lipgloss.Center, card, "", renderFooter(m.footerText()))
	if b := Banner(m.width); b != "" {
		stacked = lipgloss.JoinVertical(lipgloss.Center, b, "", card, "", renderFooter(m.footerText()))
	}
	return renderScreen(m.width, m.height, stacked)
}

// Run starts the Bubble Tea program and blocks until the user quits. apply is
// invoked on each model selection to persist that assignment, so the user keeps
// assigning until they exit with Esc/Ctrl+C. It returns the list of applied
// changes (in the order made) and an error for a program or apply failure.
func Run(agents []config.Agent, models []config.Model, apply func(agentName, providerID, modelID string) error) ([]Result, error) {
	if len(agents) == 0 {
		return nil, fmt.Errorf("no managed agents to show")
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("no available models to show")
	}

	p := tea.NewProgram(New(agents, models, apply), tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("running TUI program: %w", err)
	}

	fm, ok := finalModel.(model)
	if !ok {
		return nil, fmt.Errorf("unexpected TUI final model type %T", finalModel)
	}
	if fm.applyErr != nil {
		return fm.applied, fmt.Errorf("assigning model: %w", fm.applyErr)
	}
	return fm.applied, nil
}
