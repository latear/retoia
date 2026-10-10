package installtui

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/retoia/retoia-cli/internal/installer"
)

// setupRow identifies one actionable row on page 1. rowOpenCode is present only
// while OpenCode is missing; rowNext is the explicit "continue" button.
type setupRow int

const (
	rowOpenCode setupRow = iota // "[ ] Install OpenCode" (only when missing)
	rowEngram                   // "[x] Engram (persistent memory)"
	rowContext7                 // "[x] Context7 (library docs)"
	rowTUI                      // "[ ] Sidebar plugins (sub-agents + todo panels)"
	rowNext                     // "Next →" (proceed to the server-key page)
)

// setupIntro is the dim note above the rows: it tells the user the mandatory
// work (PATH) is automatic and only the extras below are choices.
const setupIntro = "Sets up PATH automatically. Choose the extras below."

// setupBreadcrumb is the muted sub-line under the card header.
var setupBreadcrumb = mutedStyle.Render("Step 1 of 2 · extras")

// openCodeInstallDoneMsg is delivered when the async OpenCode installer returns,
// so the page can re-check PATH and update its status line without ever leaving
// the wizard.
type openCodeInstallDoneMsg struct{ err error }

// installOpenCodeCmd runs the official OpenCode installer in the background
// (captured output, no TTY suspend), so the wizard never drops to the shell.
func installOpenCodeCmd() tea.Cmd {
	return func() tea.Msg {
		err := exec.Command("bash", "-lc", openCodeInstallCmd).Run()
		return openCodeInstallDoneMsg{err: err}
	}
}

// setupModel is the Bubble Tea model for page 1: choose the extras.
type setupModel struct {
	width  int
	height int

	openCodeInitial bool // OpenCode present when the page opened
	openCodePresent bool // OpenCode present right now
	installedNow    bool // OpenCode was installed during this page
	installErr      bool // an install attempt ran but OpenCode is still missing
	installing      bool // OpenCode installer is running (async, TUI stays up)
	blocked         bool // user pressed Next without the required OpenCode

	installEngram     bool // toggle installing + configuring Engram (persistent memory)
	installContext7   bool // toggle wiring the context7 docs MCP (library docs)
	engramPresent     bool // engram binary already on the machine (detected)
	installTUIPlugins bool // toggle the OpenCode sidebar plugins (npm; fetched at start)
	lowMemory         bool // host has little RAM → plugins default off (shown as a hint)

	rows   []setupRow
	cursor int

	done     bool // Enter on a non-action row: proceed to page 2
	cancel   bool // Ctrl+C: abort
	quitting bool
}

// newSetupModel builds page 1, seeding the extras toggles from the caller (so
// returning here via Esc preserves them) and detecting OpenCode.
func newSetupModel(installEngram, installContext7, installTUIPlugins bool) setupModel {
	present := openCodeInstalled()
	_, engramPresent := installer.ResolveEngramPath()
	m := setupModel{
		openCodeInitial:   present,
		openCodePresent:   present,
		installEngram:     installEngram || engramPresent, // detected → ticked (configure it)
		installContext7:   installContext7,
		engramPresent:     engramPresent,
		installTUIPlugins: installTUIPlugins,
		lowMemory:         lowMemoryHost(),
	}
	m.rebuildRows()
	return m
}

// hostMemGB returns total system memory in GiB, or 0 when it cannot be read.
func hostMemGB() float64 {
	switch runtime.GOOS {
	case "linux":
		data, err := os.ReadFile("/proc/meminfo")
		if err != nil {
			return 0
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "MemTotal:") {
				if f := strings.Fields(line); len(f) >= 2 {
					if kb, e := strconv.ParseFloat(f[1], 64); e == nil {
						return kb / (1024 * 1024)
					}
				}
			}
		}
	case "darwin":
		if out, err := exec.Command("sysctl", "-n", "hw.memsize").Output(); err == nil {
			if b, e := strconv.ParseFloat(strings.TrimSpace(string(out)), 64); e == nil {
				return b / (1024 * 1024 * 1024)
			}
		}
	}
	return 0
}

// lowMemoryThresholdGB is the RAM below which the npm-fetched sidebar plugins
// default to off (they are visual only, and fetching them is heavy on a small
// box). Unknown memory (0) is treated as NOT low, keeping the default-on path.
const lowMemoryThresholdGB = 4.0

// lowMemoryHost reports whether this machine has little RAM.
func lowMemoryHost() bool {
	m := hostMemGB()
	return m > 0 && m < lowMemoryThresholdGB
}

// rebuildRows recomputes the actionable rows: the OpenCode-install row appears
// only while OpenCode is missing. The cursor is clamped into range.
func (m *setupModel) rebuildRows() {
	m.rows = m.rows[:0]
	if !m.openCodePresent {
		m.rows = append(m.rows, rowOpenCode)
	}
	m.rows = append(m.rows, rowEngram, rowContext7, rowTUI, rowNext)
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m setupModel) Init() tea.Cmd { return nil }

func (m setupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case openCodeInstallDoneMsg:
		// The installer finished (ran async, without suspending the TUI);
		// re-check PATH and reflect it, staying on the page.
		m.installing = false
		present := openCodeInstalled()
		m.openCodePresent = present
		if present {
			m.installedNow = !m.openCodeInitial
			m.installErr = false
		} else {
			m.installErr = true
		}
		m.rebuildRows()
		return m, nil

	case tea.KeyMsg:
		// While OpenCode is installing, ignore navigation/toggle keys (only
		// Ctrl+C aborts) so the page stays put until the installer returns.
		if m.installing {
			if msg.String() == "ctrl+c" {
				m.cancel = true
				m.quitting = true
				return m, tea.Quit
			}
			return m, nil
		}
		m.blocked = false // any key clears the "OpenCode required" notice
		switch msg.String() {
		case "ctrl+c":
			m.cancel = true
			m.quitting = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.rows)-1 {
				m.cursor++
			}
		case " ":
			m.toggle(m.rows[m.cursor])
		case "enter":
			switch m.rows[m.cursor] {
			case rowOpenCode:
				// Run the official installer asynchronously, WITHOUT suspending
				// the TUI, so the wizard never drops back to the shell. The page
				// shows an "installing…" state and updates when it returns.
				m.installing = true
				m.installErr = false
				return m, installOpenCodeCmd()
			case rowNext:
				if !m.openCodePresent {
					// OpenCode is required; block and point the user at it.
					m.blocked = true
					m.cursor = 0 // the OpenCode row is first when missing
					return m, nil
				}
				m.done = true
				m.quitting = true
				return m, tea.Quit
			default:
				// Enter on a toggle row also toggles it (forgiving); use Next to
				// continue.
				m.toggle(m.rows[m.cursor])
			}
		}
	}
	return m, nil
}

// toggle flips the checkbox for a toggle row; other rows are a no-op.
func (m *setupModel) toggle(r setupRow) {
	switch r {
	case rowEngram:
		m.installEngram = !m.installEngram
	case rowContext7:
		m.installContext7 = !m.installContext7
	case rowTUI:
		m.installTUIPlugins = !m.installTUIPlugins
	}
}

// renderRow renders one actionable row with an accent cursor when selected.
func (m setupModel) renderRow(kind setupRow, label string) string {
	cursor := "  "
	if m.rows[m.cursor] == kind {
		cursor = accentStyle.Render("❯") + " "
	}
	return cursor + label
}

func (m setupModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder
	b.WriteString(mutedStyle.Render(setupIntro))
	b.WriteString("\n\n")

	// OpenCode is REQUIRED. When present it shows a done check (not a toggle);
	// while installing, an in-progress line; when missing, an action row.
	switch {
	case m.installing:
		b.WriteString("  " + accentStyle.Render("⟳ Installing OpenCode… ") + mutedStyle.Render("(this can take a minute)") + "\n")
	case m.openCodePresent:
		note := "(detected)"
		if m.installedNow {
			note = "(just installed)"
		}
		b.WriteString("  " + okStyle.Render("✓ OpenCode ") + mutedStyle.Render(note) + "\n")
	default:
		b.WriteString(m.renderRow(rowOpenCode, warnStyle.Render("• Install OpenCode")+mutedStyle.Render("  (required · ↵ to install)")) + "\n")
		if m.installErr {
			b.WriteString("  " + warnStyle.Render("Still not found — try again or install it manually.") + "\n")
		}
	}
	b.WriteString("\n")

	// Engram (persistent memory) toggle — ticked automatically when detected.
	engNote := "(persistent memory)"
	if m.engramPresent {
		engNote = "(detected)"
	}
	b.WriteString(m.renderRow(rowEngram, checkbox(m.installEngram)+" Engram "+mutedStyle.Render(engNote)) + "\n")

	// Context7 docs MCP (remote): current library/API docs for the agent.
	b.WriteString(m.renderRow(rowContext7, checkbox(m.installContext7)+" Context7 "+mutedStyle.Render("(library docs for the agent · remote)")) + "\n")

	// Sidebar plugins toggle: visual-only panels (sub-agents + todo) OpenCode
	// fetches from npm at first start. The underlying features are native, so
	// skipping these only drops the panels — and speeds up the first launch on a
	// small box (where it defaults off).
	tuiNote := "(sub-agents + todo panels · npm, slow first start)"
	if m.lowMemory {
		tuiNote = "(npm fetch · off by default — low RAM detected)"
	}
	b.WriteString(m.renderRow(rowTUI, checkbox(m.installTUIPlugins)+" Sidebar plugins "+mutedStyle.Render(tuiNote)) + "\n\n")

	// Explicit continue button. Gated on the required OpenCode.
	var nextLabel string
	switch {
	case !m.openCodePresent:
		nextLabel = mutedStyle.Render("Next →  (install OpenCode first)")
	case m.rows[m.cursor] == rowNext:
		nextLabel = boldAccentStyle.Render("Next →")
	default:
		nextLabel = "Next →"
	}
	b.WriteString(m.renderRow(rowNext, nextLabel))
	if m.blocked {
		b.WriteString("\n  " + warnStyle.Render("OpenCode is required to continue — install it first."))
	}

	card := renderCard(m.width, "install", setupBreadcrumb, windowLines(b.String(), availableBodyRows(m.height)))
	return screen(m.width, m.height, card, renderFooter("↑↓ move · space toggle · ↵ install/next · ^c quit"))
}

// checkbox renders a [x]/[ ] marker.
func checkbox(on bool) string {
	if on {
		return "[x]"
	}
	return "[ ]"
}

// runSetupPage runs page 1 and returns the collected choices. The seed values
// pre-set the toggles (used when returning here from page 2 via Esc).
func runSetupPage(installEngram, installContext7, installTUIPlugins bool) (setupResult, error) {
	final, err := tea.NewProgram(newSetupModel(installEngram, installContext7, installTUIPlugins), tea.WithAltScreen()).Run()
	if err != nil {
		return setupResult{}, fmt.Errorf("running install setup page: %w", err)
	}
	sm, ok := final.(setupModel)
	if !ok {
		return setupResult{}, fmt.Errorf("install setup page returned an unexpected model type %T", final)
	}
	if sm.cancel {
		return setupResult{cancel: true}, nil
	}
	return setupResult{
		installEngram:        sm.installEngram,
		installContext7:      sm.installContext7,
		installTUIPlugins:    sm.installTUIPlugins,
		openCodePresent:      sm.openCodePresent,
		openCodeInstalledNow: sm.installedNow,
	}, nil
}
