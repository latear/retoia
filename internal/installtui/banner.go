package installtui

import "github.com/retoia/retoia-cli/internal/tui"

// banner returns the Reto IA wordmark (red) for the given terminal width, or
// "" when it is too narrow. The art itself lives once in internal/tui so every
// screen shares it.
func banner(width int) string { return tui.Banner(width) }
