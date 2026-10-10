package tui

import "github.com/charmbracelet/bubbles/list"

// This file exposes thin exported wrappers around the package-private card
// helpers so sibling packages (e.g. internal/installtui) can reuse the exact
// same bordered "card" UI without duplicating the palette or sizing math.
// None of these change any existing behavior; they only forward.

// RenderCard is the exported wrapper around renderCard.
func RenderCard(termWidth int, subtitle, breadcrumb, body string) string {
	return renderCard(termWidth, subtitle, breadcrumb, body)
}

// RenderFooter is the exported wrapper around renderFooter.
func RenderFooter(text string) string {
	return renderFooter(text)
}

// NewPickerDelegate returns the shared card list delegate as a
// list.ItemDelegate, so other packages can build list.Model values with the
// same row styling used by the models picker.
func NewPickerDelegate() list.ItemDelegate {
	return newPickerDelegate()
}

// CardContentWidth is the exported wrapper around cardContentWidth.
func CardContentWidth(termWidth int) int {
	return cardContentWidth(termWidth)
}

// CardFrameSize returns the card's border+padding frame size (horizontal,
// vertical), so a caller can size an embedded list exactly as the models TUI
// does in its own WindowSizeMsg handling.
func CardFrameSize() (int, int) {
	return cardStyle.GetFrameSize()
}

// Screen centers content (a card plus its footer) on a black field the size of
// the terminal — the shared centered-dialog-on-black look.
func Screen(width, height int, content string) string {
	return renderScreen(width, height, content)
}
