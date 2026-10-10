package installtui

import (
	"fmt"
	"os"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/latear/retoia/internal/config"
)

// TestPreview renders each wizard page's View() with sample data to stdout so a
// developer can eyeball the frames without a live gateway. It is a dev tool,
// not an assertion: it is skipped unless CALDEN_PREVIEW is set.
//
//	NO_COLOR=1 CALDEN_PREVIEW=1 go test ./internal/installtui/ -run Preview -v
func TestPreview(t *testing.T) {
	if os.Getenv("CALDEN_PREVIEW") == "" {
		t.Skip("set CALDEN_PREVIEW=1 to render the install wizard frames")
	}

	const w = 72

	frame := func(title, body string) {
		fmt.Printf("\n=== %s ===\n%s\n", title, body)
	}

	// PAGE 1 — OpenCode missing (install row shown).
	{
		m := setupModel{openCodePresent: false, installEngram: true, installContext7: true, width: w}
		m.rebuildRows()
		frame("PAGE 1 · install (OpenCode missing)", m.View())
	}

	// PAGE 1 — OpenCode present (just installed).
	{
		m := setupModel{
			openCodeInitial: false, openCodePresent: true, installedNow: true,
			installEngram: true, installContext7: true, width: w,
		}
		m.rebuildRows()
		m.cursor = 1 // land on the Context7 row
		frame("PAGE 1 · install (OpenCode just installed)", m.View())
	}

	// PAGE 2 — server key, with a stored current key.
	{
		ti := textinput.New()
		ti.Placeholder = "sk-... (blank = keep current)"
		ti.EchoMode = textinput.EchoPassword
		ti.Focus()
		ti.Width = 60
		km := keyModel{
			base: "https://gateway.example/v1", input: ti,
			current: "sk-live-abcd1234efgh", hasCurrent: true, state: keyAsk, width: w,
		}
		frame("PAGE 2 · server key (has current)", km.View())
	}

	// PAGE 2 — server key, rejected-key error.
	{
		ti := textinput.New()
		ti.EchoMode = textinput.EchoPassword
		ti.Focus()
		ti.Width = 60
		km := keyModel{
			base: "https://gateway.example/v1", input: ti, state: keyAsk, width: w,
			errLine: "That key was rejected by the gateway. Check it and try again.",
		}
		frame("PAGE 2 · server key (rejected)", km.View())
	}

	// PER-AGENT PICKER — category step, sized to the terminal.
	{
		models := []config.Model{
			{ID: "auto", Name: "auto"},
			{ID: "model-a", Name: "model-a"},
			{ID: "model-b", Name: "model-b"},
			{ID: "esfuerzo-alto", Name: "esfuerzo-alto"},
			{ID: "cloud-c", Name: "cloud-c"},
		}
		groups := config.GroupModelsByCategory(models)
		pm := newAgentPicker("Retoia", "model-a", groups)
		updated, _ := pm.Update(tea.WindowSizeMsg{Width: w, Height: 20})
		pm = updated.(agentPickerModel)
		frame("PER-AGENT PICKER · category step", pm.View())
	}

	// FINAL SCREEN — applied summary.
	{
		res := applyResult{
			configPath:           "~/.config/opencode/opencode.json",
			openCodePresent:      true,
			openCodeInstalledNow: true,
			pathAdded:            true,
			pathLine:             `export PATH="/home/u/.local/bin":$PATH`,
			shellRC:              "~/.zshrc",
			binDir:               "/home/u/.local/bin",
			context7Configured:   true,
			engramConfigured:     true,
			engramPath:           "/home/u/.local/bin/engram",
			keyStored:            true,
			storedCount:          12,
		}
		frame("FINAL SCREEN · done", summaryModel{res: res, width: w}.View())
	}
}
