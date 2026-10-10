package installtui

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tidwall/gjson"

	"github.com/latear/retoia/internal/config"
	"github.com/latear/retoia/internal/installer"
	"github.com/latear/retoia/internal/keystore"
	"github.com/latear/retoia/internal/mcpservers"
	"github.com/latear/retoia/internal/onboard"
	"github.com/latear/retoia/internal/sandbox"
)

// mcpRuntimes are the launchers available inside the sandbox container, so a
// host MCP server using one of them is worth inheriting; a docker-based or
// absolute-path command is not (it would not run in the container).
var mcpRuntimes = map[string]bool{
	"npx": true, "bunx": true, "uvx": true, "uv": true,
	"node": true, "python": true, "python3": true,
}

// inheritCompatibleMCP copies the compatible MCP servers from the host config at
// hostConfig into the staged config at stageCfg: remote servers, and local
// servers whose command starts with a runtime present in the container. The
// config MCP (needs the retoia binary) and anything else are skipped. Best
// effort: errors are ignored so a broken host entry never fails the sandbox.
func inheritCompatibleMCP(stageCfg, hostConfig string) {
	data, err := os.ReadFile(hostConfig)
	if err != nil {
		return
	}
	gjson.GetBytes(data, "mcp").ForEach(func(k, v gjson.Result) bool {
		name := k.String()
		if url := v.Get("url").String(); v.Get("type").String() == "remote" && url != "" {
			_ = installer.ConfigureMCP(stageCfg, name, installer.MCPSpec{Type: "remote", URL: url, Headers: gjsonStringMap(v.Get("headers"))})
			return true
		}
		cmd := v.Get("command").Array()
		if len(cmd) == 0 || !mcpRuntimes[cmd[0].String()] {
			return true // junk / docker / absolute-path binary → not runnable in the container
		}
		command := make([]string, 0, len(cmd))
		for _, c := range cmd {
			command = append(command, c.String())
		}
		_ = installer.ConfigureMCP(stageCfg, name, installer.MCPSpec{Type: "local", Command: command, Environment: gjsonStringMap(v.Get("environment"))})
		return true
	})
}

// gjsonStringMap turns a gjson object into a map[string]string (nil when absent).
func gjsonStringMap(r gjson.Result) map[string]string {
	if !r.Exists() {
		return nil
	}
	m := map[string]string{}
	r.ForEach(func(k, v gjson.Result) bool { m[k.String()] = v.String(); return true })
	if len(m) == 0 {
		return nil
	}
	return m
}

// sandboxState is the phase of the sandbox setup page.
type sandboxState int

const (
	sbChecking   sandboxState = iota // running `docker info`
	sbNeedDocker                     // no daemon: offer setup / retry
	sbInstalling                     // running the OS-appropriate Docker setup
)

type dockerCheckedMsg struct{ ready bool }
type dockerSetupDoneMsg struct{ err error }

// sandboxModel drives the in-TUI part of the sandbox edition: detect Docker,
// and when it is missing offer an OS-appropriate setup (Colima on macOS,
// get.docker.com on Linux) with a retry — all without leaving the wizard. The
// long image build runs after this model returns, with its logs streamed.
type sandboxModel struct {
	state    sandboxState
	plan     sandbox.SetupPlan
	errLine  string
	proceed  bool // Docker is ready → caller should build & run
	cancel   bool
	quitting bool
	width    int
	height   int
}

func (m sandboxModel) Init() tea.Cmd { return checkDockerCmd() }

func checkDockerCmd() tea.Cmd {
	return func() tea.Msg { return dockerCheckedMsg{ready: sandbox.DockerReady()} }
}

func dockerSetupCmd(auto string) tea.Cmd {
	return func() tea.Msg {
		err := exec.Command("bash", "-lc", auto).Run()
		return dockerSetupDoneMsg{err: err}
	}
}

func (m sandboxModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case dockerCheckedMsg:
		if msg.ready {
			m.proceed = true
			m.quitting = true
			return m, tea.Quit
		}
		m.state = sbNeedDocker
		m.plan = sandbox.DockerSetupPlan()
		return m, nil

	case dockerSetupDoneMsg:
		// Whether or not the setup command reported success, re-check the daemon.
		if msg.err != nil {
			m.errLine = "Setup did not complete: " + msg.err.Error()
		}
		m.state = sbChecking
		return m, checkDockerCmd()

	case tea.KeyMsg:
		if m.state == sbInstalling || m.state == sbChecking {
			if msg.String() == "ctrl+c" {
				m.cancel, m.quitting = true, true
				return m, tea.Quit
			}
			return m, nil
		}
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			m.cancel, m.quitting = true, true
			return m, tea.Quit
		case "i":
			if m.plan.Auto != "" {
				m.state = sbInstalling
				m.errLine = ""
				return m, dockerSetupCmd(m.plan.Auto)
			}
		case "r":
			m.state = sbChecking
			m.errLine = ""
			return m, checkDockerCmd()
		}
	}
	return m, nil
}

func (m sandboxModel) View() string {
	if m.quitting {
		return ""
	}
	var b strings.Builder
	switch m.state {
	case sbChecking:
		b.WriteString(accentStyle.Render("Checking Docker…"))
	case sbInstalling:
		b.WriteString(accentStyle.Render("Setting up Docker… ") + mutedStyle.Render("("+m.plan.Note+", this can take a few minutes)"))
	case sbNeedDocker:
		b.WriteString(warnStyle.Render("Docker is not running.") + "\n\n")
		b.WriteString(mutedStyle.Render(m.plan.Note) + "\n")
		b.WriteString("  " + mutedStyle.Render(m.plan.Manual) + "\n\n")
		if m.plan.Auto != "" {
			b.WriteString(accentStyle.Render("i") + " set it up for me   ")
		}
		b.WriteString(accentStyle.Render("r") + " re-check   " + accentStyle.Render("esc") + " skip")
		if m.errLine != "" {
			b.WriteString("\n\n" + warnStyle.Render(m.errLine))
		}
	}
	card := renderCard(m.width, "sandbox", mutedStyle.Render("Sandbox · Docker"), b.String())
	return screen(m.width, m.height, card, renderFooter("i install · r re-check · esc skip"))
}

// existsPromptModel is shown when a sandbox is already built, so the user is told
// (instead of a silent detached rebuild) whether it is up to date and chooses
// what to do.
type existsPromptModel struct {
	running        bool
	builtVersion   string // retoia version stamped on the image ("" if unlabeled)
	currentVersion string // the running binary's version
	age            string // how long ago the image was built
	outdated       bool   // the image's version differs from the current binary
	choice         string // "open" | "rebuild" | "delete" | "cancel"
	quitting       bool
	width, height  int
}

func (m existsPromptModel) Init() tea.Cmd { return nil }

func (m existsPromptModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "o", "enter":
			m.choice, m.quitting = "open", true
			return m, tea.Quit
		case "r":
			m.choice, m.quitting = "rebuild", true
			return m, tea.Quit
		case "d":
			m.choice, m.quitting = "delete", true
			return m, tea.Quit
		case "ctrl+c", "esc", "q":
			m.choice, m.quitting = "cancel", true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m existsPromptModel) View() string {
	if m.quitting {
		return ""
	}
	status := "built"
	if m.running {
		status = "built and running"
	}
	var b strings.Builder
	b.WriteString(okStyle.Render("✓ A Reto IA sandbox is already "+status+".") + "\n")

	info := ""
	if m.builtVersion != "" {
		info = "version " + m.builtVersion
	}
	if m.age != "" {
		if info != "" {
			info += " · "
		}
		info += "built " + m.age
	}
	if info != "" {
		b.WriteString("  " + mutedStyle.Render(info) + "\n")
	}
	if m.outdated {
		b.WriteString("  " + warnStyle.Render("⚠ outdated — you have "+m.currentVersion+"; rebuild to update") + "\n")
	}

	b.WriteString("\n" + mutedStyle.Render("What do you want to do?") + "\n\n")
	b.WriteString("  " + accentStyle.Render("o") + "  open it " + mutedStyle.Render("(enter the Retoia agents)") + "\n")
	rebuildHint := "(apply the latest config)"
	if m.outdated {
		rebuildHint = "(recommended — updates to " + m.currentVersion + ")"
	}
	b.WriteString("  " + accentStyle.Render("r") + "  rebuild it " + mutedStyle.Render(rebuildHint) + "\n")
	b.WriteString("  " + accentStyle.Render("d") + "  delete it " + mutedStyle.Render("(uninstall; asks about your data)") + "\n")
	b.WriteString("  " + accentStyle.Render("esc") + "  cancel")
	card := renderCard(m.width, "sandbox", mutedStyle.Render("Sandbox · already installed"), b.String())
	return screen(m.width, m.height, card, renderFooter("o open · r rebuild · d delete · esc cancel"))
}

// runSandboxExistsPrompt asks what to do about an already-built sandbox, showing
// its version/freshness.
func runSandboxExistsPrompt(m existsPromptModel) (string, error) {
	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return "", fmt.Errorf("running sandbox prompt: %w", err)
	}
	return final.(existsPromptModel).choice, nil
}

// stagedModelCount returns how many models the staged config exposes under
// provider.retoia.models (0 means the gateway returned none — usually a
// rejected key — so every agent model would be invalid).
func stagedModelCount(cfgDir string) int {
	cfg, err := config.Load(filepath.Join(cfgDir, "opencode.json"), "retoia")
	if err != nil {
		return 0
	}
	return len(cfg.AvailableModels())
}

// restoreTerminal undoes residual full-screen (alternate-screen) state left by
// the Bubble Tea pages — exit the alt screen, show the cursor, reset attributes —
// so the plain build output that follows renders even on terminals where the
// library's own restore does not fully take (seen over SSH).
func restoreTerminal() { fmt.Print("\x1b[?1049l\x1b[?25h\x1b[0m") }

// rebuildSandboxChild runs `retoia sandbox rebuild` as a fresh child process,
// so the build runs with a clean terminal (no Bubble Tea residue) — the same
// no-TUI path that renders reliably when run on its own.
func rebuildSandboxChild(dir string) error {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		exe = "retoia"
	}
	cmd := exec.Command(exe, "sandbox", "rebuild", "--dir", dir)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// uninstallSandboxChild runs `retoia sandbox uninstall` as a fresh child process
// so its data-deletion confirm runs with a clean terminal.
func uninstallSandboxChild(dir string) error {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		exe = "retoia"
	}
	cmd := exec.Command(exe, "sandbox", "uninstall", "--dir", dir)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// attachSandbox enters the running sandbox's OpenCode (forced pty so the TUI
// renders), from the compose file in dir.
func attachSandbox(dir string) error {
	cmd := exec.Command("docker", "compose", "-f", filepath.Join(dir, "compose.sandbox.yaml"),
		"exec", "retoia", "script", "-qec", "opencode", "/dev/null")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// runSandbox builds the repo-less sandbox: it generates the Retoia config on the
// host (with this retoia binary), scaffolds the Docker artifacts in the CWD,
// then — in the TUI — ensures Docker is available (Colima on macOS) and builds &
// starts the container. No retoia repo or release is needed: the image is
// assembled from public pieces plus the baked config.
func runSandbox(hostConfig, base string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	cfgDir := filepath.Join(cwd, "opencode-config")

	// If a sandbox is already built, tell the user and let them open or rebuild it,
	// instead of a silent detached rebuild that just returns to the shell.
	// The scaffold must still be there too: an image left over from a deleted
	// sandbox must not offer a rebuild that would fail with "no sandbox".
	if sandbox.DockerReady() && sandbox.ImageExists() && scaffoldPresent(cwd) {
		built := sandbox.ImageVersion()
		choice, perr := runSandboxExistsPrompt(existsPromptModel{
			running:        sandbox.ContainerUp(),
			builtVersion:   built,
			currentVersion: Version,
			age:            sandbox.ImageAge(),
			outdated:       built != "" && Version != "" && built != Version,
		})
		if perr != nil {
			return perr
		}
		switch choice {
		case "open":
			return attachSandbox(cwd)
		case "delete":
			return uninstallSandboxChild(cwd)
		case "rebuild":
			// Pick extras (MCPs + plugins), then ask for the gateway key —
			// same as a fresh install — re-stage, and rebuild. (Keyring calls are
			// time-bounded now, so these full-screen pages no longer hang over SSH.)
			ex, exCancelled, eerr := runSandboxExtras()
			if eerr != nil {
				return eerr
			}
			if exCancelled {
				fmt.Println(cancelledMessage)
				return nil
			}
			if base != "" {
				key, kerr := runKeyPage(base)
				if kerr != nil {
					return kerr
				}
				if key.cancel {
					fmt.Println(cancelledMessage)
					return nil
				}
				restoreTerminal()
				if w := keyStoreWarning(key.storeErr); w != "" {
					fmt.Println(w)
				}
				serverKey := onboard.ResolveServerKey(os.Getenv("RETOIA_API_KEY"), keystore.Get)
				fmt.Println("Refreshing the sandbox config against the gateway…")
				if serr := stageSandboxConfig(cfgDir, hostConfig, base, serverKey, ex); serr != nil {
					return fmt.Errorf("refreshing the sandbox config: %w", serr)
				}
				if stagedModelCount(cfgDir) == 0 {
					fmt.Println("⚠ The gateway returned no models — the key was likely rejected (HTTP 401).")
					fmt.Println("  Enter a valid key on the key page, or run with RETOIA_API_KEY=<valid key>.")
				}
			} else {
				restoreTerminal()
				fmt.Println("No gateway configured — rebuilding with the existing config (agents may lack valid models).")
			}
			// Rebuild in a fresh child process (plain output renders everywhere).
			return rebuildSandboxChild(cwd)
		default: // cancel / empty
			fmt.Println("Cancelled — the existing sandbox is untouched.")
			return nil
		}
	}

	// 0a. Extras: which MCPs/plugins go into the image.
	ex, exCancelled, eerr := runSandboxExtras()
	if eerr != nil {
		return eerr
	}
	if exCancelled {
		fmt.Println(cancelledMessage)
		return nil
	}

	// 0b. With a gateway, prompt for and validate the server key (stored, so
	// staging gets the right models and the container the key). Without a
	// gateway, the sandbox proceeds with no models configured.
	var keyStoreErr error
	if base != "" {
		key, kerr := runKeyPage(base)
		if kerr != nil {
			return kerr
		}
		if key.cancel {
			fmt.Println(cancelledMessage)
			return nil
		}
		keyStoreErr = key.storeErr
	}

	// Resolve the effective gateway key once (env, else the key just stored in
	// the OS keyring). Passed explicitly into staging so the config bake can
	// validate against the gateway and fetch the models, and passed to the
	// container at runtime — never baked into the image.
	serverKey := onboard.ResolveServerKey(os.Getenv("RETOIA_API_KEY"), keystore.Get)

	// 1. Generate the Retoia config on the host, into ./opencode-config (the
	// image bakes it). The container has no retoia binary, so no config MCP.
	if err := stageSandboxConfig(cfgDir, hostConfig, base, serverKey, ex); err != nil {
		return fmt.Errorf("generating the sandbox config: %w", err)
	}

	// 2. Scaffold the Docker artifacts next to it.
	written, werr := sandbox.Write(cwd, ex.persist)
	if werr != nil {
		return fmt.Errorf("scaffolding sandbox: %w", werr)
	}
	fmt.Println("Sandbox files ready:", strings.Join(written, ", ")+", opencode-config/")

	// 3. Ensure Docker (in the TUI), then build & run.
	final, rerr := tea.NewProgram(sandboxModel{}, tea.WithAltScreen()).Run()
	if rerr != nil {
		return fmt.Errorf("running sandbox page: %w", rerr)
	}
	sm := final.(sandboxModel)
	if sm.cancel || !sm.proceed {
		fmt.Println("Docker not ready — the sandbox is scaffolded; build it when Docker is up:")
		fmt.Println("  RETOIA_API_KEY=<key> docker compose -f compose.sandbox.yaml up -d --build")
		return nil
	}

	// A full-screen page just ran; make sure the terminal is restored so the plain
	// build output below renders (over SSH the library's own restore can fall short).
	restoreTerminal()
	if w := keyStoreWarning(keyStoreErr); w != "" {
		fmt.Println(w)
	}

	// Warn if the Docker VM is small — the image build OOM-kills (exit 137) on the
	// 2 GB Colima default.
	if mem := sandbox.DockerMemGB(); mem > 0 && mem < 4 {
		fmt.Printf("\nWARNING: the Docker VM has only %.1f GB — the build may be OOM-killed (exit 137).\n", mem)
		fmt.Println("If it fails, raise it (Colima): colima stop && colima start --cpus 4 --memory 6")
	}

	// Docker is ready: build & start the container, streaming the (long) output.
	name, args := sandbox.UpCommand()
	fmt.Println("\nBuilding and starting the sandbox (first build pulls Debian + installs OpenCode; a few minutes)…")
	cmd := exec.Command(name, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(),
		"RETOIA_API_KEY="+serverKey, "RETOIA_VERSION="+Version)
	cmd.Env = append(cmd.Env, sandbox.BuildEnv()...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("building the sandbox (%s %s): %w", name, strings.Join(args, " "), err)
	}
	fmt.Println("\nSandbox is up. Opening the sandbox…")
	if aerr := attachSandbox(cwd); aerr != nil {
		fmt.Println("Could not open it automatically. Open the Retoia agents in it with:")
		fmt.Println("  retoia sandbox open      (or `retoia sandbox shell` for a shell)")
	}
	fmt.Println("Your host OpenCode was not touched.")
	return nil
}

// scaffoldPresent reports whether dir still holds the generated sandbox scaffold
// (compose file and baked config), i.e. a rebuild has something to work from.
func scaffoldPresent(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "compose.sandbox.yaml")); err != nil {
		return false
	}
	st, err := os.Stat(filepath.Join(dir, "opencode-config"))
	return err == nil && st.IsDir()
}

// confirmDataModel asks whether to also delete the persistent sandbox data.
type confirmDataModel struct {
	deleteData, cancelled bool
	quitting              bool
	width, height         int
}

func (m confirmDataModel) Init() tea.Cmd { return nil }

func (m confirmDataModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "y", "Y":
			m.deleteData, m.quitting = true, true
			return m, tea.Quit
		case "n", "N", "enter":
			m.quitting = true
			return m, tea.Quit
		case "ctrl+c", "esc", "q":
			m.cancelled, m.quitting = true, true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m confirmDataModel) View() string {
	if m.quitting {
		return ""
	}
	var b strings.Builder
	b.WriteString(warnStyle.Render("¿Borrar también los datos persistentes del sandbox (sandbox-work/ y sandbox-data/)?") + "\n")
	b.WriteString(mutedStyle.Render("Se pierden tu trabajo y la memoria.") + "\n\n")
	b.WriteString("  " + accentStyle.Render("y") + "  sí, borrar todo\n")
	b.WriteString("  " + accentStyle.Render("n") + "  no, conservar mis datos " + mutedStyle.Render("(esc)"))
	card := renderCard(m.width, "sandbox", mutedStyle.Render("Sandbox · desinstalar"), b.String())
	return screen(m.width, m.height, card, renderFooter("y sí · n/esc no"))
}

// ConfirmDataDeletion asks whether to also delete the persistent sandbox data
// (sandbox-work/ + sandbox-data/). Cancelling (esc/ctrl+c) keeps the data.
func ConfirmDataDeletion() (deleteData bool, cancelled bool, err error) {
	final, rerr := tea.NewProgram(confirmDataModel{}, tea.WithAltScreen()).Run()
	if rerr != nil {
		return false, false, fmt.Errorf("running data confirm: %w", rerr)
	}
	restoreTerminal()
	fm := final.(confirmDataModel)
	return fm.deleteData, fm.cancelled, nil
}

// stageSandboxConfig generates the Retoia OpenCode config (opencode.json + skills
// + tui.json) into cfgDir, by running the headless install against a throwaway
// HOME and copying the produced config dir out. The gateway credential is NOT
// included (the container writes it at start from the environment).
func stageSandboxConfig(cfgDir, hostConfig, base, serverKey string, ex sandboxExtras) error {
	stageHome, err := os.MkdirTemp("", "retoia-sandbox-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stageHome)

	// Redirect HOME and clear the OpenCode path overrides so everything lands
	// under the throwaway home, never touching the real host. Pass the key via
	// RETOIA_API_KEY so the model validation does not depend on keyring
	// access under the redirected HOME (which can come back empty → no models).
	env := map[string]string{
		"HOME":                stageHome,
		"OPENCODE_AUTH_PATH":  "",
		"OPENCODE_SKILLS_DIR": "",
		"OPENCODE_SKILLS_SRC": "",
	}
	if serverKey != "" {
		env["RETOIA_API_KEY"] = serverKey
	}
	restore := withEnv(env)
	defer restore()

	stageCfg := filepath.Join(stageHome, ".config", "opencode", "opencode.json")
	if err := os.MkdirAll(filepath.Dir(stageCfg), 0o755); err != nil {
		return err
	}
	if err := RunHeadless(io.Discard, stageCfg, "retoia", base,
		HeadlessOptions{TUIPlugins: ex.plugins}); err != nil {
		return err
	}
	// Engram MCP: the image installs the engram binary; its SQLite lives in the
	// container (~/.engram), so memory is per-container and dies with it.
	if ex.engram {
		_ = installer.ConfigureMCP(stageCfg, "engram", installer.MCPSpec{Type: "local", Command: []string{"engram", "mcp", "--tools=agent"}})
	}
	// context7 MCP (library docs) — a hosted remote, one of the two core MCPs.
	if ex.context7 {
		if s, ok := mcpservers.Lookup("context7"); ok {
			_ = installer.ConfigureMCP(stageCfg, s.ID, installer.MCPSpec{Type: "remote", URL: s.URL})
		}
	}
	// Inherit the compatible MCP servers from the host config (npx/uvx-style or
	// remote). Best effort — a missing/odd host config just means no inherited MCPs.
	inheritCompatibleMCP(stageCfg, hostConfig)
	// RETO.md is user-edited: carry an existing one across the rebuild.
	prevReto, _ := os.ReadFile(filepath.Join(cfgDir, "RETO.md"))
	if err := os.RemoveAll(cfgDir); err != nil {
		return err
	}
	if err := copyDir(filepath.Join(stageHome, ".config", "opencode"), cfgDir); err != nil {
		return err
	}
	if len(prevReto) > 0 {
		if err := os.WriteFile(filepath.Join(cfgDir, "RETO.md"), prevReto, 0o644); err != nil {
			return err
		}
	}
	// Fix baked absolute paths: the staging wrote the logo plugin (and registered
	// it) under the throwaway stage HOME, which does not exist inside the
	// container. Rewrite that prefix to where opencode-config is baked
	// (/root/.config/opencode) so OpenCode can actually load the plugin.
	tuiJSON := filepath.Join(cfgDir, "tui.json")
	if data, rerr := os.ReadFile(tuiJSON); rerr == nil {
		stagePrefix := filepath.Join(stageHome, ".config", "opencode")
		if fixed := strings.ReplaceAll(string(data), stagePrefix, "/root/.config/opencode"); fixed != string(data) {
			if werr := os.WriteFile(tuiJSON, []byte(fixed), 0o644); werr != nil {
				return werr
			}
		}
	}
	return nil
}

// withEnv sets env vars (empty value = unset) and returns a func that restores
// the previous values.
func withEnv(vars map[string]string) func() {
	prev := make(map[string]*string, len(vars))
	for k := range vars {
		if v, ok := os.LookupEnv(k); ok {
			cp := v
			prev[k] = &cp
		} else {
			prev[k] = nil
		}
	}
	for k, v := range vars {
		if v == "" {
			os.Unsetenv(k)
		} else {
			os.Setenv(k, v)
		}
	}
	return func() {
		for k, v := range prev {
			if v == nil {
				os.Unsetenv(k)
			} else {
				os.Setenv(k, *v)
			}
		}
	}
}

// copyDir recursively copies the directory tree at src to dst.
func copyDir(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if strings.Contains(e.Name(), ".bak-retoia") {
				continue // don't bake the surgical-writer backups into the image
			}
			if err := copyDir(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}
