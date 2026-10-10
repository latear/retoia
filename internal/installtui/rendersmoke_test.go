package installtui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/latear/retoia/internal/config"
)

// TestRenderNoPanicAcrossSizes renders the banner/scroll-affected screens at many
// terminal sizes (including tiny and the banner-width boundary) to catch a
// size-dependent panic in View/Update.
func TestRenderNoPanicAcrossSizes(t *testing.T) {
	groups := config.GroupModelsByCategory([]config.Model{
		{ID: "m0", Name: "m0", CategoryLabel: "retoia"},
		{ID: "m1", Name: "m1", CategoryLabel: "cloud"},
	})
	sizes := [][2]int{{0, 0}, {1, 1}, {20, 5}, {40, 8}, {72, 20}, {73, 20}, {80, 3}, {80, 24}, {200, 60}}

	render := func(name string, m tea.Model) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("%s panicked: %v", name, r)
			}
		}()
		for _, s := range sizes {
			um, _ := m.Update(tea.WindowSizeMsg{Width: s[0], Height: s[1]})
			_ = um.(interface{ View() string }).View()
		}
	}

	render("page1", newSetupModel(true, true, true))
	render("picker", newAgentPicker("cs-recon", "m0", groups))
}
