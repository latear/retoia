package installtui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestWindowLinesFitsAndScrolls(t *testing.T) {
	body := "a\nb\n❯ c\nd\ne\nf\ng"
	// Fits: returned unchanged.
	if got := windowLines(body, 20); got != body {
		t.Error("body that fits should be unchanged")
	}
	// Overflows: windowed to height, cursor visible, indicators present.
	got := windowLines(body, 5)
	lines := strings.Split(got, "\n")
	if len(lines) != 5 {
		t.Fatalf("windowed height = %d, want 5", len(lines))
	}
	if !strings.Contains(got, "❯ c") {
		t.Error("cursor row must stay visible after windowing")
	}
	if !strings.Contains(got, "more") {
		t.Error("a scroll indicator (more) should appear when content overflows")
	}
}

// TestSetupPageScrollsOnShortTerminal verifies the install card stays bounded and
// scrolls internally when the terminal is too short for all rows.
func TestSetupPageScrollsOnShortTerminal(t *testing.T) {
	m := newSetupModel(true, true, true, true)
	um, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 12})
	view := um.(setupModel).View()
	if !strings.Contains(view, "more") {
		t.Error("short terminal should window the card with a 'more' indicator")
	}
	if !strings.Contains(view, "❯") {
		t.Error("the cursor should remain visible in the windowed card")
	}
}
