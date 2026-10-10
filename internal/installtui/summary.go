package installtui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// summaryModel renders the final confirmation card: a summary of exactly what
// the install applied. It waits for Enter (or Ctrl+C) to exit.
type summaryModel struct {
	res      applyResult
	width    int
	height   int
	quitting bool
}

func (m summaryModel) Init() tea.Cmd { return nil }

func (m summaryModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "enter", "ctrl+c", "q", "esc":
			m.quitting = true
			return m, tea.Quit
		}
	}
	return m, nil
}

// bullet renders one summary line: an ok/muted marker, a bold label, and value.
func bullet(ok bool, label, value string) string {
	mark := mutedStyle.Render("•")
	if ok {
		mark = okStyle.Render("✓")
	}
	return "  " + mark + " " + boldAccentStyle.Render(label) + "  " + value
}

// nextStep renders one "Next steps" row: an accented command aligned to a fixed
// width, followed by a muted description.
func nextStep(cmd, desc string) string {
	return "  " + accentStyle.Render(fmt.Sprintf("%-18s", cmd)) + mutedStyle.Render(desc) + "\n"
}

func (m summaryModel) View() string {
	if m.quitting {
		return ""
	}
	r := m.res

	var b strings.Builder

	// OpenCode.
	switch {
	case r.openCodeInstalledNow:
		b.WriteString(bullet(true, "OpenCode", okStyle.Render("installed just now")))
	case r.openCodePresent:
		b.WriteString(bullet(true, "OpenCode", "already installed"))
	default:
		b.WriteString(bullet(false, "OpenCode", warnStyle.Render("not installed")))
	}
	b.WriteString("\n")

	// PATH.
	if r.pathAdded {
		b.WriteString(bullet(true, "PATH", r.binDir+mutedStyle.Render(" added to "+r.shellRC)))
		b.WriteString("\n      " + mutedStyle.Render("open a new shell (or source the file) for it to take effect"))
	} else {
		b.WriteString(bullet(true, "PATH", r.binDir+mutedStyle.Render(" already present")))
	}
	b.WriteString("\n")

	// Server key.
	switch {
	case r.keyStoreErr != nil:
		b.WriteString(bullet(false, "Server key", warnStyle.Render(fmt.Sprintf("validated but NOT saved (%s)", r.keyStoreErr))))
		b.WriteString("\n      " + mutedStyle.Render("it won't persist — export RETOIA_API_KEY=<key> before re-running"))
	case r.keyStored:
		b.WriteString(bullet(true, "Server key", fmt.Sprintf("stored (%d models authorized)", r.storedCount)))
	case r.keepCurrent && r.offline:
		b.WriteString(bullet(false, "Server key", warnStyle.Render("pending — no validated key; agents use their default model")))
	case r.keepCurrent:
		b.WriteString(bullet(true, "Server key", "kept the current key"))
	default:
		b.WriteString(bullet(false, "Server key", warnStyle.Render("pending")))
	}
	b.WriteString("\n")

	// Gateway (provider base URL).
	switch {
	case r.baseURLSet:
		b.WriteString(bullet(true, "Gateway", r.baseURL+mutedStyle.Render(" set as the provider base URL")))
	case r.noGateway:
		b.WriteString(bullet(false, "Gateway", warnStyle.Render("skipped — no key; extras installed without a gateway")))
	default:
		b.WriteString(bullet(false, "Gateway", warnStyle.Render("base URL not set")))
	}
	b.WriteString("\n")

	// OpenCode credential.
	switch {
	case r.credentialSet:
		b.WriteString(bullet(true, "Credential", mutedStyle.Render("written to OpenCode auth store for provider ")+r.credentialProvider))
	default:
		b.WriteString(bullet(false, "Credential", warnStyle.Render(r.credentialSkipReason)))
	}
	b.WriteString("\n")

	// context7 docs MCP.
	if r.context7Configured {
		b.WriteString(bullet(true, "Context7", mutedStyle.Render("wired (mcp.context7 → library docs)")))
		b.WriteString("\n")
	}

	// Reto IA OpenCode theme.
	if r.themeInstalled {
		b.WriteString(bullet(true, "Theme", mutedStyle.Render("retoia (red) selected in tui.json")))
		b.WriteString("\n")
	}

	// Reto IA startup logo (home_logo TUI plugin).
	if r.logoInstalled {
		b.WriteString(bullet(true, "Logo", mutedStyle.Render("Reto IA wordmark on the OpenCode home screen")))
		b.WriteString("\n")
	}

	// Reto onboarding docs.
	if r.onboardingSkipReason != "" {
		b.WriteString(bullet(false, "Onboarding", warnStyle.Render(r.onboardingSkipReason)))
	} else {
		v := "RETO.md rules + AGENTS.md protocol"
		if !r.onboardingWroteReto {
			v += mutedStyle.Render(" (existing RETO.md kept)")
		}
		b.WriteString(bullet(true, "Onboarding", v))
	}
	b.WriteString("\n")

	// OpenCode sidebar plugins.
	if len(r.tuiPluginsAdded) > 0 {
		b.WriteString(bullet(true, "Sidebar", fmt.Sprintf("added %d plugin(s)", len(r.tuiPluginsAdded))+mutedStyle.Render(" → "+r.tuiPluginsPath)))
		b.WriteString("\n")
	}

	// Engram (persistent-memory MCP server).
	switch {
	case r.engramConfigured:
		v := "registered in opencode.json " + mutedStyle.Render("(mcp.engram → "+r.engramPath+")")
		if r.engramInstalledNow {
			v = "installed + " + v
		}
		b.WriteString(bullet(true, "Engram", v))
	case r.engramChosen:
		b.WriteString(bullet(false, "Engram", warnStyle.Render(r.engramSkipReason)))
	default:
		b.WriteString(bullet(false, "Engram", mutedStyle.Render("skipped (not selected)")))
	}

	// Impeccable design skill (installed via npx).
	switch {
	case r.impeccableInstalled:
		b.WriteString("\n" + bullet(true, "Impeccable", "installed "+mutedStyle.Render("(npx skills add pbakaus/impeccable)")))
	case r.impeccableChosen:
		b.WriteString("\n" + bullet(false, "Impeccable", warnStyle.Render(r.impeccableSkipReason)))
	default:
		b.WriteString("\n" + bullet(false, "Impeccable", mutedStyle.Render("skipped (not selected)")))
	}

	// Next steps: how to configure the agent models and load the changes.
	b.WriteString("\n\n" + boldAccentStyle.Render("Next steps") + "\n")
	if r.noGateway {
		// No gateway yet: spell out exactly what is still pending.
		b.WriteString("  " + warnStyle.Render("No gateway configured. Still pending:") + "\n")
		b.WriteString(nextStep("set the gateway", "re-run install, or set RETOIA_BASE + a key"))
		b.WriteString(nextStep("assign models", "retoia models"))
	}
	b.WriteString(nextStep("retoia models", "assign a model to each configured agent"))
	b.WriteString(nextStep("retoia key", "check or reset the stored gateway key"))
	b.WriteString(nextStep("restart OpenCode", "load the provider and MCP servers"))

	breadcrumb := okStyle.Render("Done — everything below was applied")
	card := renderCard(m.width, "done", breadcrumb, b.String())
	return screen(m.width, m.height, card, renderFooter("↵ finish"))
}

// runSummary shows the final confirmation screen and blocks until Enter.
func runSummary(res applyResult) error {
	_, err := tea.NewProgram(summaryModel{res: res}, tea.WithAltScreen()).Run()
	if err != nil {
		return fmt.Errorf("running install summary: %w", err)
	}
	return nil
}
