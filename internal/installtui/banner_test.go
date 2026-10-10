package installtui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/ficopix/caldensec/internal/tui"
)

func TestBanner(t *testing.T) {
	artW := lipgloss.Width(tui.BannerArt)

	// Too narrow: hidden, never overflows.
	if got := banner(artW - 1); got != "" {
		t.Errorf("banner(narrow) = %q, want empty", got)
	}

	// Wide enough: contains the art and stays within the requested width.
	out := banner(artW + 20)
	if !strings.Contains(out, "██████╗") {
		t.Error("banner does not contain the wordmark")
	}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if w := lipgloss.Width(line); w > artW+20 {
			t.Errorf("banner line width %d exceeds requested %d", w, artW+20)
		}
	}
}
