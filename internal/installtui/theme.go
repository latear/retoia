package installtui

import "github.com/charmbracelet/lipgloss"

// Console palette for the install wizard: black background, red wordmark, white
// body text, red accents/selection/borders (matching the Reto IA logo), with
// green reserved for success checks. Colors are fixed (not adaptive) because the
// wizard forces a black field.
var (
	colBlack = lipgloss.Color("0")
	colRed   = lipgloss.Color("#0000FF") // Late.Ar blue (#0000FF) — the Reto IA wordmark + accents
	colGreen = lipgloss.Color("10")      // bright green — success checks only
	colWhite = lipgloss.Color("15")      // body text
	colGray  = lipgloss.Color("245")
)

var (
	accentStyle     = lipgloss.NewStyle().Foreground(colRed)
	boldAccentStyle = lipgloss.NewStyle().Bold(true).Foreground(colRed)
	mutedStyle      = lipgloss.NewStyle().Foreground(colGray)
	plainStyle      = lipgloss.NewStyle().Foreground(colWhite)
	okStyle         = lipgloss.NewStyle().Foreground(colGreen) // ✓ success stays green (semantic)
	warnStyle       = lipgloss.NewStyle().Foreground(colRed)
	redStyle        = lipgloss.NewStyle().Foreground(colRed)
	whiteStyle      = lipgloss.NewStyle().Foreground(colWhite)

	// cardStyle is the centered dialog box: red rounded border on black.
	cardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colRed).
			Background(colBlack).
			Foreground(colWhite).
			Padding(0, 1)
)

// maxCardWidth caps the dialog width so it never spans the whole screen: on a
// wide terminal it keeps side margins and stays centered under the wordmark.
// minCardWidth keeps it legible on narrow terminals.
const (
	maxCardWidth = 64
	minCardWidth = 34
	cardMargin   = 4
	// cardHPadding is the total horizontal padding of cardStyle (Padding(0,1)).
	cardHPadding = 2
	// maxListRows caps an embedded list's visible rows so the box keeps a fixed
	// height and the list scrolls internally rather than stretching the screen.
	maxListRows = 12
)

// cardContentWidth returns the width available inside the box (border + padding
// removed), capped at maxCardWidth.
func cardContentWidth(termWidth int) int {
	outer := termWidth - cardMargin
	if outer > maxCardWidth {
		outer = maxCardWidth
	}
	if outer < minCardWidth {
		outer = minCardWidth
	}
	h, _ := cardStyle.GetFrameSize()
	w := outer - h
	if w < 1 {
		w = 1
	}
	return w
}

// renderCard renders the dialog box: a red "Reto IA" wordmark, a white
// subtitle, the breadcrumb, a blank spacer, then body.
func renderCard(termWidth int, subtitle, breadcrumb, body string) string {
	header := redStyle.Bold(true).Render("Reto IA") + whiteStyle.Render(" · "+subtitle)
	content := header + "\n" + breadcrumb + "\n\n" + body
	// Add back the horizontal padding lipgloss folds into Width, so the text
	// area equals cardContentWidth and full-width rows never wrap.
	return cardStyle.Width(cardContentWidth(termWidth) + cardHPadding).Render(content)
}

// renderFooter renders the dim help line shown below the box.
func renderFooter(text string) string { return mutedStyle.Render(text) }

// Version is the build version shown in the bottom signature; main sets it.
var Version = "dev"

// heartBlue colors the signature's pixel heart (ANSI 256 blue, matching the CLI).
var heartBlue = lipgloss.NewStyle().Foreground(lipgloss.Color("33"))

// signature is the centered credit line shown at the very bottom of every screen.
func signature() string {
	return mutedStyle.Render(Version+" — ") + heartBlue.Render("♥") + mutedStyle.Render(" by Late.Ar")
}

// screen stacks the banner, the box, and the footer centered
// relative to each other, then centers the whole block on a black field the
// size of the terminal — the classic centered-dialog-on-black look.
func screen(width, height int, card, footer string) string {
	parts := make([]string, 0, 4)
	// The Reto IA wordmark is shown on every screen (it self-hides only when the
	// terminal is too narrow to fit it).
	if b := banner(width); b != "" {
		parts = append(parts, b, "")
	}
	parts = append(parts, card)
	if footer != "" {
		parts = append(parts, "", footer)
	}
	parts = append(parts, "", signature()) // centered credit, below everything
	inner := lipgloss.NewStyle().Background(colBlack).
		Render(lipgloss.JoinVertical(lipgloss.Center, parts...))

	if width <= 0 {
		return inner
	}
	if height <= 0 {
		return lipgloss.PlaceHorizontal(width, lipgloss.Center, inner, lipgloss.WithWhitespaceBackground(colBlack))
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, inner, lipgloss.WithWhitespaceBackground(colBlack))
}
