package tui

import "github.com/charmbracelet/lipgloss"

// BannerArt is the Reto IA wordmark shown at the top of the app's screens.
const BannerArt = ` ██████╗ █████╗ ██╗     ██████╗ ███████╗███╗   ██╗███████╗███████╗ ██████╗
██╔════╝██╔══██╗██║     ██╔══██╗██╔════╝████╗  ██║██╔════╝██╔════╝██╔════╝
██║     ███████║██║     ██║  ██║█████╗  ██╔██╗ ██║███████╗█████╗  ██║
██║     ██╔══██║██║     ██║  ██║██╔══╝  ██║╚██╗██║╚════██║██╔══╝  ██║
╚██████╗██║  ██║███████╗██████╔╝███████╗██║ ╚████║███████║███████╗╚██████╗
 ╚═════╝╚═╝  ╚═╝╚══════╝╚═════╝ ╚══════╝╚═╝  ╚═══╝╚══════╝╚══════╝ ╚═════╝`

// Banner returns the Reto IA wordmark in red, or "" when the terminal is
// narrower than the art (so it never wraps or overflows). Callers center it
// above the dialog box.
func Banner(width int) string {
	if width < lipgloss.Width(BannerArt) {
		return ""
	}
	return lipgloss.NewStyle().Bold(true).Foreground(colorRed).Render(BannerArt)
}
