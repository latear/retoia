// Package installer holds the non-interactive building blocks used by the
// install TUI: configuring the provider/gateway and MCP servers in
// opencode.json, and ensuring ~/.local/bin is on the shell PATH. Each
// function is pure enough to test against temporary files; the TUI layer
// wires them to real paths.
package installer

import (
	"fmt"
	"os"
	"strings"
)

// EnsurePATH makes sure binDir is on the PATH as declared in the shell rc file
// at shellRC. If binDir is not already referenced there, it appends an
// `export PATH="<binDir>:$PATH"` line and reports added=true with that line.
// If binDir is already referenced, it is a no-op (added=false). A missing rc
// file is created.
func EnsurePATH(shellRC, binDir string) (added bool, line string, err error) {
	existing, err := os.ReadFile(shellRC)
	if err != nil && !os.IsNotExist(err) {
		return false, "", fmt.Errorf("reading %q: %w", shellRC, err)
	}
	if strings.Contains(string(existing), binDir) {
		return false, "", nil
	}

	line = fmt.Sprintf("export PATH=%q:$PATH", binDir)
	// Keep the appended block readable and idempotent-looking.
	block := "\n# Added by retoia-cli install\n" + line + "\n"
	content := append(existing, []byte(block)...)
	if err := os.WriteFile(shellRC, content, 0o644); err != nil {
		return false, "", fmt.Errorf("writing %q: %w", shellRC, err)
	}
	return true, line, nil
}
