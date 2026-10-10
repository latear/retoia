package installtui

import "strings"

// availableBodyRows returns how many lines a card body may use before it would
// push the screen past termHeight, allowing for the card frame, the header +
// breadcrumb + spacer, the footer, and the bottom signature. An unknown height
// (<= 0) means "no limit" — don't window.
func availableBodyRows(termHeight int) int {
	if termHeight <= 0 {
		return 1 << 30
	}
	_, cardV := cardStyle.GetFrameSize()
	h := termHeight - cardV - 6
	if h < 3 {
		h = 3
	}
	return h
}

// windowLines keeps a card body within height lines so the dialog stays a fixed
// height and scrolls internally. When the body already fits it is returned
// unchanged; when it overflows it is windowed around the cursor row (the line
// holding the "❯" marker), with muted "↑ more" / "↓ more" indicators on the
// edges when there is content above/below.
func windowLines(body string, height int) string {
	lines := strings.Split(body, "\n")
	if height < 3 || len(lines) <= height {
		return body
	}
	cursor := 0
	for i, ln := range lines {
		if strings.Contains(ln, "❯") {
			cursor = i
			break
		}
	}
	start := cursor - height/2
	if maxStart := len(lines) - height; start > maxStart {
		start = maxStart
	}
	if start < 0 {
		start = 0
	}
	win := make([]string, height)
	copy(win, lines[start:start+height])
	if start > 0 {
		win[0] = mutedStyle.Render("  ↑ more")
	}
	if start+height < len(lines) {
		win[height-1] = mutedStyle.Render("  ↓ more")
	}
	return strings.Join(win, "\n")
}
