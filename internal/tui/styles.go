package tui

import (
	"io"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Console palette shared across the whole app: black background, red wordmark,
// white body text, green accents/selection/border — a Linux-console dialog
// look. Colors are fixed (not adaptive) because the UI forces a black field.
var (
	colorBlack  = lipgloss.Color("0")
	colorRed    = lipgloss.Color("9")  // the CaldenSec wordmark
	colorAccent = lipgloss.Color("10") // green: selection, active breadcrumb, border
	colorWhite  = lipgloss.Color("15") // body text
	colorMuted  = lipgloss.Color("245")
)

var (
	// cardStyle is the centered dialog box: green rounded border on black.
	cardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorAccent).
			Background(colorBlack).
			Foreground(colorWhite).
			Padding(0, 1)

	titleAppStyle = lipgloss.NewStyle().Bold(true).Foreground(colorRed)
	titleSubStyle = lipgloss.NewStyle().Foreground(colorWhite)

	breadcrumbActiveStyle  = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	breadcrumbDoneStyle    = lipgloss.NewStyle().Foreground(colorWhite)
	breadcrumbPendingStyle = lipgloss.NewStyle().Foreground(colorMuted)

	footerStyle = lipgloss.NewStyle().Foreground(colorMuted)

	// sectionHeaderStyle renders a non-selectable section header row: bold
	// green, regardless of cursor position (see pickerDelegate.Render).
	sectionHeaderStyle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true).
				PaddingLeft(2)

	selectedPrefixStyle = lipgloss.NewStyle().Foreground(colorAccent)
	selectedItemStyle   = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	normalItemStyle     = lipgloss.NewStyle().Foreground(colorWhite)
	mutedItemStyle      = lipgloss.NewStyle().Foreground(colorMuted)

	// Status styles shared by the menu / add-custom screens.
	mutedStyle = lipgloss.NewStyle().Foreground(colorMuted)
	okStyle    = lipgloss.NewStyle().Foreground(colorAccent)
	warnStyle  = lipgloss.NewStyle().Foreground(colorRed)
)

// minCardWidth and cardMargin bound how the card is sized against the
// terminal width: the card is (terminal width - cardMargin), never smaller
// than minCardWidth, so it stays legible at narrow widths.
const (
	minCardWidth = 34
	maxCardWidth = 64
	cardMargin   = 4
	// cardHPadding is the total horizontal padding of cardStyle (Padding(0,1)).
	cardHPadding = 2
	// maxListRows caps the visible rows of an embedded list so the dialog stays
	// a fixed, compact height and the list scrolls/paginates internally instead
	// of stretching the box to the full terminal height.
	maxListRows = 12
)

// cardOuterWidth returns the total rendered width of the card (border and
// padding included) for a given terminal width, capped at maxCardWidth so the
// box keeps side margins and stays centered on wide terminals.
func cardOuterWidth(termWidth int) int {
	w := termWidth - cardMargin
	if w > maxCardWidth {
		w = maxCardWidth
	}
	if w < minCardWidth {
		w = minCardWidth
	}
	return w
}

// cardContentWidth returns the width available to the card's content (the
// header, breadcrumb, and embedded list) once the border and padding are
// subtracted from cardOuterWidth.
func cardContentWidth(termWidth int) int {
	h, _ := cardStyle.GetFrameSize()
	w := cardOuterWidth(termWidth) - h
	if w < 1 {
		w = 1
	}
	return w
}

// renderCard renders the bordered card: a bold app title plus a muted
// subtitle, the breadcrumb line, a blank spacer, then body (the active
// list's own View), all sized to termWidth.
func renderCard(termWidth int, subtitle, breadcrumb, body string) string {
	header := titleAppStyle.Render("CaldenSec") + titleSubStyle.Render(" · "+subtitle)
	content := header + "\n" + breadcrumb + "\n\n" + body
	// lipgloss Width includes horizontal padding, so add it back: the text area
	// must equal cardContentWidth (what the embedded list is sized to), or a
	// full-width row would overflow the padding and wrap onto a second line.
	return cardStyle.Width(cardContentWidth(termWidth) + cardHPadding).Render(content)
}

// renderFooter renders the dim help line shown below the card.
func renderFooter(text string) string {
	return footerStyle.Render(text)
}

// renderScreen centers content (the card plus its footer) horizontally and
// vertically on a black field the size of the terminal — the classic
// centered-dialog-on-black console look.
func renderScreen(width, height int, content string) string {
	inner := lipgloss.NewStyle().Background(colorBlack).Render(content)
	if width <= 0 {
		return inner
	}
	if height <= 0 {
		return lipgloss.PlaceHorizontal(width, lipgloss.Center, inner, lipgloss.WithWhitespaceBackground(colorBlack))
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, inner, lipgloss.WithWhitespaceBackground(colorBlack))
}

// pickerDelegate is a list.ItemDelegate for the card UI: the selected row
// gets an accent "❯ " prefix, normal rows a plain 2-space indent, and any
// description is shown muted on the same line, column-aligned to the
// widest visible title. Section headers bypass this entirely and always
// render bold+muted (see sectionHeaderItem), since they are never
// selectable.
type pickerDelegate struct {
	list.DefaultDelegate
}

// newPickerDelegate builds the delegate used by all three picker lists.
func newPickerDelegate() pickerDelegate {
	return pickerDelegate{DefaultDelegate: list.NewDefaultDelegate()}
}

// Height reports a single row per item: descriptions render inline rather
// than on their own line, so no second line needs to be reserved.
func (d pickerDelegate) Height() int { return 1 }

// Render draws one row. It only understands sectionHeaderItem and
// list.DefaultItem (agentItem/categoryItem/modelItem all satisfy the
// latter); anything else renders nothing, matching the embedded delegate's
// own behavior.
func (d pickerDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	if hi, ok := item.(sectionHeaderItem); ok {
		_, _ = io.WriteString(w, sectionHeaderStyle.Render(hi.label))
		return
	}

	di, ok := item.(list.DefaultItem)
	if !ok {
		return
	}
	width := m.Width()
	if width <= 0 {
		return
	}

	prefix := "  "
	titleStyle := normalItemStyle
	if index == m.Index() && m.FilterState() != list.Filtering {
		prefix = selectedPrefixStyle.Render("❯") + " "
		titleStyle = selectedItemStyle
	}

	line := titleStyle.Render(padTitle(di.Title(), titleColumn(m.VisibleItems())))
	if desc := di.Description(); desc != "" {
		line += mutedItemStyle.Render(desc)
	}
	_, _ = io.WriteString(w, ansi.Truncate(prefix+line, width, "…"))
}

// titleColumn returns the width titles should be padded to before their
// description, so descriptions line up in a column. Capped so a single
// unusually long title cannot blow out the whole list; beyond that cap
// descriptions simply follow one space after the title.
func titleColumn(items []list.Item) int {
	const maxCol = 24
	widest := 0
	for _, it := range items {
		di, ok := it.(list.DefaultItem)
		if !ok || di.Description() == "" {
			continue
		}
		if n := utf8.RuneCountInString(di.Title()); n > widest {
			widest = n
		}
	}
	if widest == 0 || widest > maxCol {
		return 0
	}
	return widest + 2
}

func padTitle(title string, col int) string {
	if col == 0 {
		return title
	}
	if n := utf8.RuneCountInString(title); n < col {
		return title + strings.Repeat(" ", col-n)
	}
	return title
}
