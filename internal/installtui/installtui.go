// Package installtui implements the interactive `retoia install` flow: a
// Bubble Tea wizard over the non-interactive building blocks in
// internal/installer. It runs as a short sequence of independent Bubble Tea
// programs (never nested), each rendered inside the shared bordered "card" UI
// (internal/tui):
//
//	page 1 "install"     – extras: optionally install OpenCode, and toggle
//	                       Engram, Context7, and the OpenCode sidebar
//	                       plugins. The mandatory work (ensuring
//	                       ~/.local/bin is on PATH) is never shown as a
//	                       toggle; it just runs.
//	page 2 "server key"  – a masked gateway-key gate that can validate a new
//	                       key or keep the current one (offline-tolerant).
//	final "done" screen   – a card summarising exactly what was applied.
//
// Once every program has exited it applies the choices (register the
// provider, ensure PATH, wire the chosen MCPs) and shows the final
// confirmation screen.
package installtui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/retoia/retoia-cli/internal/config"
	"github.com/retoia/retoia-cli/internal/installer"
	"github.com/retoia/retoia-cli/internal/keystore"
	"github.com/retoia/retoia-cli/internal/mcpservers"
	"github.com/retoia/retoia-cli/internal/onboard"
)

// The install wizard's palette and box/screen renderers live in theme.go.

// cancelledMessage is printed to stdout on any clean Ctrl+C, at any page,
// before the apply step. No files are touched in that case.
const cancelledMessage = "cancelled, no changes made"

// openCodeInstallCmd is the official OpenCode installer, run on the real
// terminal via tea.ExecProcess (the TUI suspends while it runs, then resumes).
const openCodeInstallCmd = "curl -fsSL https://opencode.ai/install | bash"

// engramBrewFormula is the Homebrew tap/formula for Engram; engramReleasesURL
// is where to grab it when Homebrew is unavailable (e.g. Linux without brew).
const (
	engramBrewFormula = "gentleman-programming/tap/engram"
	engramReleasesURL = "https://github.com/Gentleman-Programming/engram/releases"
)

// openCodeInstalled reports whether the `opencode` binary is available. It
// checks PATH first, then the locations the official installer uses, because a
// just-installed opencode is often not yet on the running process's PATH (its
// dir was only added to the shell rc), which otherwise looks like a failure.
func openCodeInstalled() bool {
	if _, err := exec.LookPath("opencode"); err == nil {
		return true
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".opencode", "bin", "opencode"),
		filepath.Join(home, ".local", "bin", "opencode"),
		filepath.Join(home, "bin", "opencode"),
		"/usr/local/bin/opencode",
		"/opt/homebrew/bin/opencode",
	}
	if dir := os.Getenv("OPENCODE_INSTALL_DIR"); dir != "" {
		candidates = append(candidates, filepath.Join(dir, "opencode"))
	}
	for _, p := range candidates {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return true
		}
	}
	return false
}

// setupResult is what page 1 collects.
type setupResult struct {
	installEngram        bool // toggle installing + configuring Engram
	installContext7      bool // toggle wiring the context7 docs MCP
	installTUIPlugins    bool // toggle the OpenCode sidebar plugins (subagents/files/plan)
	openCodePresent      bool // OpenCode present when the page exited
	openCodeInstalledNow bool // OpenCode was installed during this page
	cancel               bool // Ctrl+C: abort the whole install
}

// keyResult is what page 2 collects.
type keyResult struct {
	models      []config.Model // authorized models, used to register the provider
	keepCurrent bool           // empty submit: the stored key was kept
	keyStored   bool           // a freshly entered key was validated and stored
	storedCount int            // models authorized by the freshly stored key
	offline     bool           // proceeding without a validated key (unreachable)
	storeErr    error          // non-nil when the validated key could not be persisted
	back        bool           // Esc: return to page 1
	cancel      bool           // Ctrl+C: abort the whole install
}

// applyResult is the record of everything apply() did, rendered by the final
// confirmation screen.
type applyResult struct {
	configPath string

	pathAdded bool
	pathLine  string
	shellRC   string
	binDir    string

	openCodePresent      bool
	openCodeInstalledNow bool

	baseURL    string // gateway base URL written into the provider block
	baseURLSet bool   // provider.<id>.options.baseURL was set
	noGateway  bool   // no gateway configured: reminder shown

	credentialSet        bool   // the server key was written into OpenCode's auth store
	credentialProvider   string // provider id the credential was written under
	credentialSkipReason string // set when no key was available to write

	context7Configured bool // the context7 docs MCP was wired into opencode.json

	tuiPluginsAdded []string // OpenCode TUI sidebar plugins added to tui.json
	themeInstalled  bool     // the Reto IA red theme was shipped + selected
	logoInstalled   bool     // the Reto IA home_logo TUI plugin was shipped + registered
	tuiPluginsPath  string   // path to the tui.json that was written

	engramChosen       bool   // the user ticked the Engram option
	engramConfigured   bool   // the mcp.engram block was written to opencode.json
	engramPath         string // resolved engram binary path
	engramInstalledNow bool   // engram was installed during this run (via brew)
	engramSkipReason   string // set when chosen but engram could not be installed/found

	keepCurrent bool
	keyStored   bool
	storedCount int
	offline     bool
	keyStoreErr error // non-nil when the validated key could not be persisted
}

// Run drives the full interactive install against the opencode.json at
// configPath (provider providerID) and the gateway at base. It returns nil on
// a clean cancel, printing cancelledMessage; no files are written before the
// apply step, so a cancel is always a true no-op.
func Run(configPath, providerID, base string) error {
	// STEP 0 — edition. Local installs into this host's OpenCode; Sandbox builds
	// an isolated Docker container (repo-less) and never touches the host.
	em, err := runEditionPage()
	if err != nil {
		return err
	}
	if em.cancel {
		fmt.Println(cancelledMessage)
		return nil
	}
	if em.choice == editionSandbox {
		return runSandbox(configPath, base)
	}

	// PAGE 1 — extras. Engram and Context7 default on; sidebar plugins default
	// on unless the host has little RAM.
	setup, err := runSetupPage(true, true, !lowMemoryHost())
	if err != nil {
		return err
	}
	if setup.cancel {
		fmt.Println(cancelledMessage)
		return nil
	}

	// PAGE 2 — server key. Skipped entirely when there is no gateway: the install proceeds offline so
	// the extras still get installed, with a reminder at the end.
	var key keyResult
	if base == "" {
		key = keyResult{offline: true}
	} else {
		// Esc goes back to page 1 (preserving its toggles), then returns here.
		for {
			key, err = runKeyPage(base)
			if err != nil {
				return err
			}
			if key.cancel {
				fmt.Println(cancelledMessage)
				return nil
			}
			if key.back {
				setup, err = runSetupPage(setup.installEngram, setup.installContext7, setup.installTUIPlugins)
				if err != nil {
					return err
				}
				if setup.cancel {
					fmt.Println(cancelledMessage)
					return nil
				}
				continue
			}
			break
		}
	}

	// APPLY — the first point that touches any file.
	result, err := apply(configPath, providerID, base, setup, key)
	if err != nil {
		return err
	}

	// FINAL confirmation screen.
	return runSummary(result)
}

// apply points the provider at the gateway base URL, writes the server key
// into OpenCode's credential store, ensures PATH, and optionally configures
// Engram, Context7 and the TUI sidebar plugins, recording everything it did
// into an applyResult for the final screen. It performs no TUI rendering.
func apply(configPath, providerID, base string, setup setupResult, key keyResult) (applyResult, error) {
	res := applyResult{
		configPath:           configPath,
		openCodePresent:      setup.openCodePresent,
		openCodeInstalledNow: setup.openCodeInstalledNow,
		keepCurrent:          key.keepCurrent,
		keyStored:            key.keyStored,
		storedCount:          key.storedCount,
		offline:              key.offline,
		keyStoreErr:          key.storeErr,
	}

	// A fresh machine has no opencode.json yet; create a minimal one so the
	// surgical writers below have a file to edit.
	if err := installer.EnsureConfig(configPath); err != nil {
		return res, fmt.Errorf("preparing config: %w", err)
	}

	// Register the gateway provider — only when there is a gateway. When the key
	// validated, write its base URL AND the authorized models, so OpenCode knows
	// the model ids to route (a fresh config has no provider block). With no
	// validated models, at least set the base URL. With no gateway at all (none
	// configured), skip the provider entirely and flag a reminder.
	switch {
	case base == "":
		res.noGateway = true
	case len(key.models) > 0:
		if err := installer.AddProvider(configPath, providerID, base, key.models); err != nil {
			return res, fmt.Errorf("registering gateway provider: %w", err)
		}
		res.baseURL = base
		res.baseURLSet = true
		// Seed the single principal agent on the default model.
		if modelID := defaultModelID(key.models); modelID != "" {
			if err := installer.AddAgent(configPath, installer.PrincipalAgentName, "primary",
				"Reto IA — principal agent.", providerID, modelID); err != nil {
				return res, fmt.Errorf("seeding principal agent: %w", err)
			}
		}
	default:
		if err := installer.SetProviderBaseURL(configPath, providerID, base); err != nil {
			return res, fmt.Errorf("setting provider base URL: %w", err)
		}
		res.baseURL = base
		res.baseURLSet = true
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return res, fmt.Errorf("resolving home directory: %w", err)
	}

	// Credential: write the effective server key (env override, else the key
	// just stored/kept on page 2) into OpenCode's auth store, so OpenCode can
	// authenticate against the gateway at inference time. The raw key never
	// touches opencode.json — it goes only into auth.json (0600).
	serverKey := onboard.ResolveServerKey(os.Getenv("RETOIA_API_KEY"), keystore.Get)
	if serverKey != "" {
		authPath := resolveAuthPath(home)
		if err := installer.SetOpenCodeCredential(authPath, providerID, serverKey); err != nil {
			return res, fmt.Errorf("writing OpenCode credential: %w", err)
		}
		res.credentialSet = true
		res.credentialProvider = providerID
	} else {
		res.credentialSkipReason = "no server key available (set RETOIA_API_KEY or store one via the key gate)"
	}

	// PATH (mandatory).
	res.binDir = filepath.Join(home, ".local", "bin")
	res.shellRC = resolveShellRC(home)
	added, line, perr := installer.EnsurePATH(res.shellRC, res.binDir)
	if perr != nil {
		return res, fmt.Errorf("ensuring PATH: %w", perr)
	}
	res.pathAdded = added
	res.pathLine = line

	// Engram (only if chosen). Install it when missing (Homebrew when available),
	// then register it as a local MCP server in opencode.json. A missing binary
	// or a failed install degrades to a skip reason instead of failing the whole
	// install; only a config-write failure is fatal.
	if setup.installEngram {
		res.engramChosen = true
		path, found := installer.ResolveEngramPath()
		if !found {
			// Prefer the official release binary: it works on Linux and macOS with
			// no Homebrew. Fall back to brew only if the download fails.
			if p, derr := installer.InstallEngramRelease(); derr == nil {
				path, found, res.engramInstalledNow = p, true, true
			} else if _, berr := exec.LookPath("brew"); berr == nil {
				cmd := exec.Command("brew", "install", engramBrewFormula)
				cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
				if runErr := cmd.Run(); runErr == nil {
					if path, found = installer.ResolveEngramPath(); found {
						res.engramInstalledNow = true
					}
				}
			}
			if !found {
				res.engramSkipReason = "could not install engram automatically; get it from " + engramReleasesURL
			}
		}
		if found {
			if err := installer.ConfigureEngramMCP(configPath, path); err != nil {
				return res, fmt.Errorf("configuring Engram MCP: %w", err)
			}
			res.engramConfigured = true
			res.engramPath = path
		}
	}

	// context7 docs MCP (remote) — one of the two core MCPs, wired by default.
	if setup.installContext7 {
		if s, ok := mcpservers.Lookup("context7"); ok {
			if err := installer.ConfigureMCP(configPath, s.ID, installer.MCPSpec{
				Type: "remote",
				URL:  s.URL,
			}); err != nil {
				return res, fmt.Errorf("configuring context7 MCP: %w", err)
			}
			res.context7Configured = true
		}
	}

	// TUI sidebar plugins (only if chosen): merged into tui.json (a sibling of
	// opencode.json), never clobbering the user's existing plugins. Gives the
	// sidebar panels for sub-agents, files and the task/plan backlog.
	if setup.installTUIPlugins {
		tuiPath := filepath.Join(filepath.Dir(configPath), "tui.json")
		add, terr := installer.ConfigureTUIPlugins(tuiPath, installer.RetoiaTUIPlugins)
		if terr != nil {
			return res, fmt.Errorf("configuring TUI plugins: %w", terr)
		}
		res.tuiPluginsAdded = add
		res.tuiPluginsPath = tuiPath
	}

	// Reto IA OpenCode theme (red/dark, matching the logo). Always installed —
	// it is just a local JSON file plus one tui.json key (no npm, no cost).
	configDir := filepath.Dir(configPath)
	if _, terr := installer.WriteRetoiaTheme(configDir); terr != nil {
		return res, fmt.Errorf("writing Retoia theme: %w", terr)
	}
	if terr := installer.SetTUITheme(filepath.Join(configDir, "tui.json"), installer.RetoiaThemeName); terr != nil {
		return res, fmt.Errorf("selecting Retoia theme: %w", terr)
	}
	res.themeInstalled = true

	// Reto IA startup logo: a tiny TUI plugin that renders the wordmark in the
	// home_logo slot (OpenCode's own logo is compiled in and not themeable). Best
	// effort — a failure here never blocks the install.
	if logoPath, lerr := installer.WriteRetoiaLogoPlugin(configDir); lerr == nil {
		if _, perr := installer.ConfigureTUIPlugins(filepath.Join(configDir, "tui.json"), []string{logoPath}); perr == nil {
			res.logoInstalled = true
		}
	}

	return res, nil
}

// resolveShellRC returns the shell rc file to edit: ~/.zshrc when it exists as a
// regular file, otherwise ~/.bashrc (which EnsurePATH creates if missing).
func resolveShellRC(home string) string {
	zsh := filepath.Join(home, ".zshrc")
	if fi, err := os.Stat(zsh); err == nil && !fi.IsDir() {
		return zsh
	}
	return filepath.Join(home, ".bashrc")
}

// resolveAuthPath returns OpenCode's credential store path: $OPENCODE_AUTH_PATH
// when set, else $XDG_DATA_HOME/opencode/auth.json, else the default
// ~/.local/share/opencode/auth.json. This mirrors where `opencode auth login`
// writes, so a credential written here is the one OpenCode reads.
func resolveAuthPath(home string) string {
	if v := os.Getenv("OPENCODE_AUTH_PATH"); v != "" {
		return v
	}
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "opencode", "auth.json")
	}
	return filepath.Join(home, ".local", "share", "opencode", "auth.json")
}

// defaultModelID picks the model to seed the principal agent with: "auto" when
// the gateway offers it, otherwise the first model; "" when there are none.
func defaultModelID(models []config.Model) string {
	for _, m := range models {
		if m.ID == "auto" {
			return m.ID
		}
	}
	if len(models) > 0 {
		return models[0].ID
	}
	return ""
}
