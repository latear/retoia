package installtui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/ficopix/caldensec/internal/config"
)

// bannerFragment is a chunk of the CaldenSec wordmark that must appear on screens.
const bannerFragment = "██████╗ █████╗"

func TestBannerOnPicker(t *testing.T) {
	groups := config.GroupModelsByCategory([]config.Model{
		{ID: "m0", Name: "m0", CategoryLabel: "calden"},
	})

	// Per-agent model picker (was missing the banner).
	p := newAgentPicker("cs-recon", "m0", groups)
	pv, _ := p.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	if !strings.Contains(pv.(agentPickerModel).View(), bannerFragment) {
		t.Error("agent picker screen is missing the CaldenSec banner")
	}
}
