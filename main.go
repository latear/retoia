// Command caldensec is the CaldenSec tooling entry point. It dispatches to
// subcommands, the first of which is `models`: a standalone, dependency-free
// TUI for assigning an OpenCode model to each Calden agent, writing the
// change surgically into opencode.json (backup + validate, never a full
// re-marshal).
package main

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/ficopix/caldensec/internal/config"
	"github.com/ficopix/caldensec/internal/gateway"
	"github.com/ficopix/caldensec/internal/installer"
	"github.com/ficopix/caldensec/internal/installtui"
	"github.com/ficopix/caldensec/internal/keystore"
	"github.com/ficopix/caldensec/internal/mcpservers"
	"github.com/ficopix/caldensec/internal/onboard"
	"github.com/ficopix/caldensec/internal/providers"
	"github.com/ficopix/caldensec/internal/sandbox"
	"github.com/ficopix/caldensec/internal/tui"
	"github.com/ficopix/caldensec/internal/updater"
	"github.com/ficopix/caldensec/internal/urlseal"
)

// version is the build version, set by GoReleaser via -ldflags "-X main.version=...".
// Local builds report "dev".
var version = "dev"

func main() {
	installtui.Version = version // show the build version in the TUI's bottom signature
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "caldensec: "+err.Error())
		os.Exit(1)
	}
}

// usage is the text printed for `caldensec help`, `caldensec -h`,
// `caldensec --help`, and no arguments at all.
const usage = `caldensec - retoia-cli tooling
Usage: caldensec <command> [flags]

Commands:
  install    Set up the provider/gateway and MCP servers (PATH, key)
  uninstall  Remove it from OpenCode (provider, MCP entries, credential)
  models     Assign an OpenCode model to each configured agent
  mcp        List, add or remove MCP servers (engram, context7)
  sandbox    Sandbox edition (one Docker container): init | open | shell | models | uninstall
  key        Show or reset the stored gateway server key
  update     Update caldensec to the latest signed release via the gateway
  version    Print the caldensec version
  seal       Seal a gateway URL for distribution (needs $CALDENSEC_UNLOCK)
  seal-update  Seal the update endpoint URL for distribution (needs $CALDENSEC_UNLOCK)
  seal-template  Seal the current agent→model assignment as the install template

Run 'caldensec <command> --help' for a command's flags.
`

// run dispatches to the requested subcommand, printing help to stdout. It
// never calls os.Exit itself; main() is the only place that translates an
// error into a process exit code, which keeps run fully testable.
func run(args []string) error {
	return runCLI(os.Stdout, args)
}

// runCLI is run with an explicit destination for help/usage output, so it can
// be tested. An explicit help request (or no arguments) is a success and its
// usage goes to out (stdout); an unknown command returns an error for main()
// to report on stderr, and prints no usage here.
func runCLI(out io.Writer, args []string) error {
	if len(args) == 0 {
		fmt.Fprint(out, usage)
		return nil
	}

	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprint(out, usage)
		return nil
	case "install":
		return cmdInstall(args[1:])
	case "uninstall":
		return cmdUninstall(args[1:])
	case "models":
		return cmdModels(args[1:])
	case "mcp":
		return cmdMCP(os.Stdout, args[1:])
	case "sandbox":
		return cmdSandbox(os.Stdout, args[1:])
	case "key":
		return cmdKey(args[1:])
	case "update":
		return cmdUpdate(out, args[1:])
	case "version", "--version", "-v":
		fmt.Fprintln(out, version+" — \x1b[38;5;33m♥\x1b[0m by fico")
		return nil
	case "seal":
		return cmdSeal(os.Stdout, args[1:])
	case "seal-update":
		return cmdSealUpdate(os.Stdout, args[1:])
	case "seal-template":
		return cmdSealTemplate(os.Stdout, args[1:])
	default:
		return fmt.Errorf("unknown command %q (run 'caldensec help')", args[0])
	}
}

// cmdInstall implements the `caldensec install` subcommand: an interactive
// setup that ensures ~/.local/bin is on the PATH, configures the
// provider/gateway and the Engram/Context7 MCP servers, and gates the model
// assignment behind a validated gateway server key. All the real work lives in
// internal/installer; internal/installtui is only the Bubble Tea front end.
func cmdInstall(args []string) error {
	defaultPath, err := defaultConfigPath()
	if err != nil {
		return err
	}

	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	configPath := fs.String("config", defaultPath, "path to opencode.json")
	providerID := fs.String("provider", "caldensec", "OpenCode provider id managed by this tool")
	gatewayFlag := fs.String("gateway", "", "override the CaldenSec gateway base URL (default: $CALDENSEC_BASE, else the sealed gateway opened with $CALDENSEC_UNLOCK)")
	yes := fs.Bool("yes", false, "non-interactive install (no TUI): apply with defaults; used by automation and the sandbox image")
	engram := fs.Bool("engram", true, "with --yes: install and configure the Engram MCP")
	context7 := fs.Bool("context7", true, "with --yes: wire the context7 docs MCP")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Resolve the gateway base URL. When it is sealed and no passphrase came
	// from the environment, defer the unlock to step 0 inside the TUI instead of
	// failing here: pass an empty base and onboard.UnsealBase as the unlocker.
	base, err := onboard.ResolveBase(*gatewayFlag, os.Getenv("CALDENSEC_BASE"))
	var unlock func(string) (string, error)
	switch {
	case err == nil:
		// base already known (flag, $CALDENSEC_BASE, or sealed + $CALDENSEC_UNLOCK)
	case errors.Is(err, onboard.ErrLocked):
		base, unlock = "", onboard.UnsealBase
	default:
		return err
	}

	// Non-interactive path: there is no TTY to prompt on, so a still-locked
	// gateway must be openable from the environment (or absent). The passphrase
	// for the sealed agent templates comes from $CALDENSEC_UNLOCK.
	if *yes {
		passphrase := os.Getenv("CALDENSEC_UNLOCK")
		if base == "" && passphrase != "" && onboard.Sealed() {
			if b, uerr := onboard.UnsealBase(passphrase); uerr == nil {
				base = b
			}
		}
		if err := installtui.RunHeadless(os.Stdout, *configPath, *providerID, base, passphrase,
			installtui.HeadlessOptions{Engram: *engram, Context7: *context7, TUIPlugins: true}); err != nil {
			return err
		}
		notifyIfUpdate(os.Stdout, *configPath, *providerID)
		return nil
	}

	if err := installtui.Run(*configPath, *providerID, base, unlock); err != nil {
		return err
	}
	notifyIfUpdate(os.Stdout, *configPath, *providerID)
	return nil
}

// notifyIfUpdate prints a one-line notice when a newer caldensec release is
// available. Best effort and silent on any failure (no update endpoint, offline,
// or nothing newer), so it never disrupts an install.
func notifyIfUpdate(out io.Writer, configPath, providerID string) {
	base, err := resolveUpdateBase(configPath, providerID)
	if err != nil {
		return
	}
	if v, newer := updater.CheckLatest(base, resolveGatewayKey(providerID, configPath), version); newer {
		fmt.Fprintf(out, "\nA newer caldensec is available: %s — run `caldensec update`\n", v)
	}
}

// cmdUninstall reverses `caldensec install`: it removes the caldensec
// provider, the Engram/Context7 MCP entries and the OpenCode credential from
// OpenCode. With --purge it also clears the stored gateway key and removes
// the installed binary. Custom providers you added and your shell PATH line
// are left untouched.
func cmdUninstall(args []string) error {
	defaultPath, err := defaultConfigPath()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	configPath := fs.String("config", defaultPath, "path to opencode.json")
	providerID := fs.String("provider", "caldensec", "OpenCode provider id to remove")
	yes := fs.Bool("yes", false, "do not ask for confirmation")
	purge := fs.Bool("purge", false, "also clear the stored key and remove the caldensec binary")
	if err := fs.Parse(args); err != nil {
		return err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	authPath := defaultAuthPath()

	// Config keys to remove: the provider and the MCP entries this tool wires.
	paths := []string{"provider." + *providerID, "mcp.engram", "mcp.context7"}

	if !*yes {
		fmt.Printf("This removes provider %q, the Engram + Context7 MCP entries,\n"+
			"the sidebar plugins and the OpenCode credential from:\n  %s\n", *providerID, *configPath)
		if *purge {
			fmt.Println("  …and clears the stored key and removes the caldensec binary.")
		}
		if !confirm("Continue?") {
			fmt.Println("cancelled, nothing removed")
			return nil
		}
	}

	removed, err := installer.DeleteConfigPaths(*configPath, paths)
	if err != nil {
		return fmt.Errorf("cleaning opencode.json: %w", err)
	}
	fmt.Printf("opencode.json: removed %d entr%s\n", removed, plural(removed))

	// TUI sidebar plugins (in tui.json, a sibling of opencode.json).
	tuiPath := filepath.Join(filepath.Dir(*configPath), "tui.json")
	if n, terr := installer.RemoveTUIPlugins(tuiPath, installer.CaldenTUIPlugins); terr != nil {
		fmt.Println("note: could not clean tui.json:", terr)
	} else if n > 0 {
		fmt.Printf("tui.json: removed %d sidebar plugin%s\n", n, map[bool]string{true: "", false: "s"}[n == 1])
	}

	// CaldenSec OpenCode theme (themes/calden.json + the tui.json selection).
	if terr := installer.RemoveCaldenTheme(filepath.Dir(*configPath)); terr != nil {
		fmt.Println("note: could not remove the Calden theme:", terr)
	} else {
		fmt.Println("theme: removed calden (reverted to OpenCode default)")
	}

	// Backups left by the surgical writers (<file>.bak-calden*).
	var backups int
	for _, base := range []string{*configPath, tuiPath} {
		if matches, _ := filepath.Glob(base + ".bak-calden*"); matches != nil {
			for _, m := range matches {
				if os.Remove(m) == nil {
					backups++
				}
			}
		}
	}
	if backups > 0 {
		fmt.Printf("backups: removed %d\n", backups)
	}

	if err := installer.RemoveOpenCodeCredential(authPath, *providerID); err != nil {
		return fmt.Errorf("removing credential: %w", err)
	}
	fmt.Println("OpenCode credential: removed")

	if *purge {
		if err := keystore.Delete(keyAccount); err != nil {
			fmt.Println("note: could not clear the stored key:", err)
		} else {
			fmt.Println("stored key: cleared")
		}
		_ = keystore.Delete(updateBaseAccount) // stored update endpoint URL
		bin := filepath.Join(home, ".local", "bin", "caldensec")
		if err := os.Remove(bin); err == nil {
			fmt.Println("binary: removed " + bin)
		}
		fmt.Println("note: your PATH line in the shell rc was left as-is.")
	}
	fmt.Println("Done. Custom providers you added were left untouched.")
	return nil
}

// resolvePassphrase returns the install passphrase: $CALDENSEC_UNLOCK when set
// (for automation/CI), otherwise it prompts on the terminal WITHOUT echoing, so
// the passphrase never has to live in an environment variable or shell history.
// The no-echo toggle is best-effort (Unix `stty`); if it is unavailable the read
// still works, just echoed.
func resolvePassphrase(prompt string) (string, error) {
	if v := os.Getenv("CALDENSEC_UNLOCK"); v != "" {
		return v, nil
	}
	fmt.Fprint(os.Stderr, prompt)
	stty := func(arg string) {
		c := exec.Command("stty", arg)
		c.Stdin = os.Stdin
		_ = c.Run()
	}
	stty("-echo")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	stty("echo")
	fmt.Fprintln(os.Stderr)
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("reading passphrase: %w", err)
	}
	pass := strings.TrimSpace(line)
	if pass == "" {
		return "", fmt.Errorf("no passphrase provided")
	}
	return pass, nil
}

// confirm asks a yes/no question on stdin, defaulting to no.
func confirm(prompt string) bool {
	fmt.Print(prompt + " [y/N] ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

// plural returns the entry/entries suffix for n.
func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

const mcpUsage = `caldensec mcp - manage MCP servers wired into OpenCode

Usage:
  caldensec mcp list                         show configured servers and the catalog
  caldensec mcp add <catalog-id>             add a curated server (wires it into opencode.json)
  caldensec mcp add <name> -- <cmd> [args…]  add a custom local stdio server
  caldensec mcp add <name> --remote <url>    add a custom remote server
  caldensec mcp setup <catalog-id>           run a server's install steps (e.g. build its Docker backend), then wire it
  caldensec mcp remove <name>                remove a server

'setup' flags: --dir <workdir> (default ~/.caldensec/mcp), --dry-run (print the
plan only), --yes (skip the confirmation).`

// mcpFlags holds the recognized `caldensec mcp` flags.
type mcpFlags struct {
	config string
	remote string
	env    []string
}

// parseMCPArgs splits args into recognized flags and positionals. Unlike the
// stdlib flag package it accepts flags in any position (the natural
// `mcp add shodan --config X` reads left to right), supports --flag=value and
// --flag value, and treats everything after a lone "--" as literal positionals,
// so a custom local command keeps its own flags (`add t -- uvx pkg --x`).
func parseMCPArgs(args []string, allowRemote, allowEnv bool) (mcpFlags, []string, error) {
	var f mcpFlags
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "--") {
			pos = append(pos, a)
			continue
		}
		name, val, hasVal := a[2:], "", false
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			name, val, hasVal = name[:eq], name[eq+1:], true
		}
		take := func() (string, error) {
			if hasVal {
				return val, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("flag --%s needs a value", name)
			}
			i++
			return args[i], nil
		}
		var err error
		switch name {
		case "config":
			f.config, err = take()
		case "remote":
			if !allowRemote {
				return f, nil, fmt.Errorf("--remote is not valid here")
			}
			f.remote, err = take()
		case "env":
			if !allowEnv {
				return f, nil, fmt.Errorf("--env is not valid here")
			}
			var v string
			if v, err = take(); err == nil {
				f.env = append(f.env, v)
			}
		default:
			return f, nil, fmt.Errorf("unknown flag %q", a)
		}
		if err != nil {
			return f, nil, err
		}
	}
	return f, pos, nil
}

// mcpConfigPath returns the flag value or the default opencode.json path.
func mcpConfigPath(f mcpFlags) (string, error) {
	if f.config != "" {
		return f.config, nil
	}
	return defaultConfigPath()
}

// cmdMCP implements `caldensec mcp`: list, add, or remove MCP servers. MCP
// servers extend the agents with tools (e.g. library docs via context7), wired
// the same way Engram is. It writes surgically into opencode.json (backup+validate).
func cmdMCP(out io.Writer, args []string) error {
	action := "list"
	if len(args) > 0 {
		action = args[0]
		args = args[1:]
	}
	switch action {
	case "list", "ls":
		return cmdMCPList(out, args)
	case "add":
		return cmdMCPAdd(out, args)
	case "setup":
		return cmdMCPSetup(out, args)
	case "remove", "rm":
		return cmdMCPRemove(out, args)
	case "help", "-h", "--help":
		fmt.Fprintln(out, mcpUsage)
		return nil
	default:
		return fmt.Errorf("unknown 'mcp' action %q (run 'caldensec mcp help')", action)
	}
}

// cmdMCPList prints the currently configured MCP servers and the curated
// catalog of recommended servers.
func cmdMCPList(out io.Writer, args []string) error {
	f, _, err := parseMCPArgs(args, false, false)
	if err != nil {
		return err
	}
	configPath, err := mcpConfigPath(f)
	if err != nil {
		return err
	}

	configured, err := installer.ConfiguredMCP(configPath)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Configured MCP servers (%s):\n", configPath)
	if len(configured) == 0 {
		fmt.Fprintln(out, "  (none)")
	}
	for _, n := range configured {
		fmt.Fprintln(out, "  • "+n)
	}

	fmt.Fprintln(out, "\nCatalog (add with 'caldensec mcp add <id>'):")
	for _, s := range mcpservers.Catalog() {
		suffix := ""
		switch {
		case s.Manual():
			suffix = "  [manual — see repo]"
		case s.NeedsSetup():
			suffix = "  [needs setup]"
		case len(s.Env) > 0:
			suffix = "  [needs " + strings.Join(s.Env, ", ") + "]"
		}
		fmt.Fprintf(out, "  • %-14s %s (%s)%s\n", s.ID, s.Description, s.Category, suffix)
	}
	return nil
}

// cmdMCPAdd adds a curated catalog server by id, or a custom server: a local
// stdio server (name followed by `-- <cmd> args…`) or a remote server
// (`<name> --remote <url>`).
func cmdMCPAdd(out io.Writer, args []string) error {
	f, rest, err := parseMCPArgs(args, true, true)
	if err != nil {
		return err
	}
	configPath, err := mcpConfigPath(f)
	if err != nil {
		return err
	}
	if len(rest) == 0 {
		return fmt.Errorf("need a catalog id or a server name (run 'caldensec mcp help')")
	}
	name := rest[0]

	// Custom remote server.
	if f.remote != "" {
		if err := installer.ConfigureMCP(configPath, name, installer.MCPSpec{Type: "remote", URL: f.remote}); err != nil {
			return err
		}
		fmt.Fprintf(out, "added remote MCP server %q → %s\n", name, f.remote)
		return nil
	}

	// Custom local server: `<name> -- <cmd> args…`.
	if len(rest) > 1 {
		env, eerr := parseEnv(f.env)
		if eerr != nil {
			return eerr
		}
		if err := installer.ConfigureMCP(configPath, name, installer.MCPSpec{Type: "local", Command: rest[1:], Environment: env}); err != nil {
			return err
		}
		fmt.Fprintf(out, "added local MCP server %q → %s\n", name, strings.Join(rest[1:], " "))
		return nil
	}

	// Catalog server by id.
	s, ok := mcpservers.Find(name)
	if !ok {
		return fmt.Errorf("no catalog server %q; run 'caldensec mcp list', or add a custom one with '-- <cmd>' or '--remote <url>'", name)
	}
	// Manual server: cannot be auto-wired; point at its repo and setup note.
	if s.Manual() {
		fmt.Fprintf(out, "%s cannot be wired automatically.\n  Repo:  %s\n", s.Name, s.RepoURL)
		if s.SetupNote != "" {
			fmt.Fprintln(out, "  Note:  "+s.SetupNote)
		}
		return nil
	}

	// Wireable server: build the spec (local command or remote URL) and write it.
	var spec installer.MCPSpec
	if s.URL != "" {
		spec = installer.MCPSpec{Type: "remote", URL: s.URL}
	} else {
		// Env vars are written as {env:VAR} references so secrets stay out of
		// opencode.json (no catalog entry uses an API key, but keep it safe).
		var env map[string]string
		if len(s.Env) > 0 {
			env = make(map[string]string, len(s.Env))
			for _, k := range s.Env {
				env[k] = "{env:" + k + "}"
			}
		}
		spec = installer.MCPSpec{Type: "local", Command: s.Command, Environment: env}
	}
	if err := installer.ConfigureMCP(configPath, s.ID, spec); err != nil {
		return err
	}
	fmt.Fprintf(out, "added MCP server %q → %s\n", s.ID, mcpTarget(s))

	// Setup steps must run before the wired command works. The install wizard
	// runs these for you; here we print them so the CLI path is self-contained.
	if s.NeedsSetup() {
		fmt.Fprintln(out, "\nRun this setup once before using it:")
		if s.SetupNote != "" {
			fmt.Fprintln(out, "  ("+s.SetupNote+")")
		}
		for _, step := range s.Setup {
			fmt.Fprintln(out, "  $ "+step)
		}
	}
	if len(s.Env) > 0 {
		fmt.Fprintf(out, "set these in your environment before use: %s\n", strings.Join(s.Env, ", "))
	}
	return nil
}

// mcpTarget renders a catalog server's wired target for a status line.
func mcpTarget(s mcpservers.Server) string {
	if s.URL != "" {
		return s.URL
	}
	return strings.Join(s.Command, " ")
}

// flagValue reads the value of a --flag (either "--flag=value" or "--flag value")
// at index i, returning the value and the index to continue from.
func flagValue(a string, args []string, i int) (val string, next int, err error) {
	if eq := strings.IndexByte(a, '='); eq >= 0 {
		return a[eq+1:], i, nil
	}
	if i+1 >= len(args) {
		return "", i, fmt.Errorf("flag %q needs a value", a)
	}
	return args[i+1], i + 1, nil
}

// cmdMCPSetup runs a catalog server's install steps (the "wizard": build its
// Docker backend, etc.) on the local host, then wires the server into
// opencode.json. It checks prerequisites first and, unless --yes is given, asks
// before running anything. --dry-run prints the plan without executing.
func cmdMCPSetup(out io.Writer, args []string) error {
	var configPath, workDir string
	var yes, dryRun bool
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--yes" || a == "-y":
			yes = true
		case a == "--dry-run":
			dryRun = true
		case a == "--config" || strings.HasPrefix(a, "--config="):
			v, ni, err := flagValue(a, args, i)
			if err != nil {
				return err
			}
			configPath, i = v, ni
		case a == "--dir" || strings.HasPrefix(a, "--dir="):
			v, ni, err := flagValue(a, args, i)
			if err != nil {
				return err
			}
			workDir, i = v, ni
		case strings.HasPrefix(a, "-"):
			return fmt.Errorf("unknown flag %q", a)
		default:
			pos = append(pos, a)
		}
	}
	if len(pos) == 0 {
		return fmt.Errorf("need a catalog id (run 'caldensec mcp list')")
	}
	s, ok := mcpservers.Find(pos[0])
	if !ok {
		return fmt.Errorf("no catalog server %q (run 'caldensec mcp list')", pos[0])
	}
	if !s.NeedsSetup() {
		if s.Wireable() {
			return fmt.Errorf("%s needs no setup; add it directly with 'caldensec mcp add %s'", s.ID, s.ID)
		}
		return fmt.Errorf("%s cannot be set up automatically; see %s", s.ID, s.RepoURL)
	}

	if configPath == "" {
		p, err := defaultConfigPath()
		if err != nil {
			return err
		}
		configPath = p
	}
	if workDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		workDir = filepath.Join(home, ".caldensec", "mcp")
	}

	// Prerequisite check.
	var missing []string
	for _, bin := range s.Prereqs {
		if _, err := exec.LookPath(bin); err != nil {
			missing = append(missing, bin)
		}
	}

	// Plan.
	fmt.Fprintf(out, "Setup for %s (%s)\n", s.Name, s.ID)
	fmt.Fprintf(out, "  working dir: %s\n", workDir)
	if len(s.Prereqs) > 0 {
		status := okMark(len(missing) == 0)
		fmt.Fprintf(out, "  prerequisites: %s (%s)\n", strings.Join(s.Prereqs, ", "), status)
		if len(missing) > 0 {
			fmt.Fprintf(out, "    missing: %s\n", strings.Join(missing, ", "))
		}
	}
	fmt.Fprintln(out, "  steps:")
	for _, step := range s.Setup {
		fmt.Fprintln(out, "    $ "+step)
	}
	fmt.Fprintf(out, "  then wire: mcp.%s → %s\n", s.ID, mcpTarget(s))

	if dryRun {
		return nil
	}
	if len(missing) > 0 {
		return fmt.Errorf("install the missing prerequisite(s) first: %s", strings.Join(missing, ", "))
	}
	if !yes && !confirm("\nRun these steps now?") {
		fmt.Fprintln(out, "cancelled, nothing run")
		return nil
	}

	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return fmt.Errorf("creating work dir %q: %w", workDir, err)
	}
	for _, step := range s.Setup {
		fmt.Fprintln(out, "\n$ "+step)
		cmd := exec.Command("sh", "-c", step)
		cmd.Dir = workDir
		cmd.Stdout, cmd.Stderr = out, out
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("setup step failed (%s): %w", step, err)
		}
	}

	// Wire it now that the backend is up.
	var env map[string]string
	if len(s.Env) > 0 {
		env = make(map[string]string, len(s.Env))
		for _, k := range s.Env {
			env[k] = "{env:" + k + "}"
		}
	}
	spec := installer.MCPSpec{Type: "local", Command: s.Command, Environment: env}
	if s.URL != "" {
		spec = installer.MCPSpec{Type: "remote", URL: s.URL}
	}
	if err := installer.ConfigureMCP(configPath, s.ID, spec); err != nil {
		return err
	}
	fmt.Fprintf(out, "\ndone — %s is set up and wired (mcp.%s). Restart OpenCode to load it.\n", s.Name, s.ID)
	return nil
}

// okMark returns a check or cross for a boolean status line.
func okMark(ok bool) string {
	if ok {
		return "ok"
	}
	return "missing"
}

// cmdSandbox scaffolds the sandbox edition: a single Docker container (Kali
// base) with OpenCode + the Calden suite installed inside, so the host OpenCode
// is never touched. It writes the artifacts (Dockerfile, compose, entrypoint)
// into the target dir and prints the build/run steps; the docker build itself is
// left to the user (this tool does not run Docker).
func cmdSandbox(out io.Writer, args []string) error {
	action := "init"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		action = args[0]
		args = args[1:]
	}
	switch action {
	case "init":
		return cmdSandboxInit(out, args)
	case "open":
		return cmdSandboxAttach(args, true) // opencode (forced pty)
	case "shell":
		return cmdSandboxAttach(args, false) // bash
	case "models":
		return cmdSandboxModels(out, args)
	case "rebuild":
		return cmdSandboxRebuild(out, args)
	case "uninstall", "down":
		return cmdSandboxUninstall(out, args)
	default:
		return fmt.Errorf("unknown 'sandbox' action %q (want init, open, shell, models, rebuild or uninstall)", action)
	}
}

// cmdSandboxRebuild rebuilds and recreates the sandbox container with no TUI and
// plain progress output — a terminal-agnostic path that avoids the install
// wizard's full-screen handoff (which can render as a blank over some SSH
// terminals). It refreshes the Docker scaffold in place and keeps the already
// baked Calden config (a full config refresh needs the install passphrase).
func cmdSandboxRebuild(out io.Writer, args []string) error {
	fs := flag.NewFlagSet("sandbox rebuild", flag.ContinueOnError)
	dir := fs.String("dir", ".", "directory holding the sandbox artifacts")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(*dir, "compose.sandbox.yaml")); err != nil {
		return fmt.Errorf("no sandbox in %s — run `caldensec install` and pick Sandbox first", *dir)
	}
	if _, err := os.Stat(filepath.Join(*dir, "opencode-config")); err != nil {
		return fmt.Errorf("no opencode-config/ in %s — run `caldensec install` and pick Sandbox to generate it", *dir)
	}
	if !sandbox.DockerReady() {
		return fmt.Errorf("Docker is not running — start it (macOS: `colima start`), then retry")
	}
	// Refresh the Docker scaffold (Dockerfile/compose/entrypoint) in place; keep
	// the baked Calden config as-is.
	if _, err := sandbox.Write(*dir); err != nil {
		return fmt.Errorf("refreshing the sandbox scaffold: %w", err)
	}
	serverKey := onboard.ResolveServerKey(os.Getenv("CALDENSEC_API_KEY"), keystore.Get)
	name, bargs := sandbox.UpCommand()
	bargs = append(bargs, "--force-recreate")
	fmt.Fprintln(out, "Rebuilding the sandbox (plain output; a few minutes on a cold cache)…")
	cmd := exec.Command(name, bargs...)
	cmd.Dir = *dir
	cmd.Env = append(os.Environ(), "CALDENSEC_API_KEY="+serverKey, "CALDENSEC_VERSION="+version)
	cmd.Env = append(cmd.Env, sandbox.BuildEnv()...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("rebuilding the sandbox (%s %s): %w", name, strings.Join(bargs, " "), err)
	}
	fmt.Fprintln(out, "\nSandbox rebuilt. Open it with: caldensec sandbox open")
	return nil
}

func cmdSandboxInit(out io.Writer, args []string) error {
	fs := flag.NewFlagSet("sandbox init", flag.ContinueOnError)
	dir := fs.String("dir", ".", "directory to write the sandbox artifacts into")
	if err := fs.Parse(args); err != nil {
		return err
	}
	written, err := sandbox.Write(*dir)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "sandbox edition scaffolded in %s:\n", *dir)
	for _, n := range written {
		fmt.Fprintln(out, "  • "+n)
	}
	return nil
}

// cmdSandboxAttach opens the running sandbox container: OpenCode (opencode=true)
// or a shell. OpenCode is run through the container's `script` so it always gets
// a pty and renders, even if the docker exec's own TTY handling is imperfect.
func cmdSandboxAttach(args []string, opencode bool) error {
	dir := "."
	for i := 0; i < len(args); i++ {
		if args[i] == "--dir" || strings.HasPrefix(args[i], "--dir=") {
			v, ni, err := flagValue(args[i], args, i)
			if err != nil {
				return err
			}
			dir, i = v, ni
		}
	}
	compose := filepath.Join(dir, "compose.sandbox.yaml")
	if _, err := os.Stat(compose); err != nil {
		return fmt.Errorf("no compose.sandbox.yaml in %q — run this from your sandbox directory (or pass --dir)", dir)
	}
	inner := []string{"compose", "-f", compose, "exec", "caldensec"}
	if opencode {
		// Force a pty inside the container so the TUI renders reliably.
		inner = append(inner, "script", "-qec", "opencode", "/dev/null")
	} else {
		inner = append(inner, "bash")
	}
	cmd := exec.Command("docker", inner...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// cmdSandboxUninstall tears the sandbox down: `docker compose down` removes the
// container and its network; --rmi also removes the caldensec-sandbox image and
// --purge deletes the generated scaffold. The mounted sandbox-work/ is always
// kept — it holds the user's work.
func cmdSandboxUninstall(out io.Writer, args []string) error {
	fs := flag.NewFlagSet("sandbox uninstall", flag.ContinueOnError)
	dir := fs.String("dir", ".", "directory holding the sandbox artifacts")
	rmi := fs.Bool("rmi", false, "also remove the caldensec-sandbox image")
	purge := fs.Bool("purge", false, "also delete the generated scaffold (keeps sandbox-work/)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	compose := filepath.Join(*dir, "compose.sandbox.yaml")
	if !sandbox.DockerReady() {
		fmt.Fprintln(out, "Docker is not running — skipping container/image removal.")
	} else if _, err := os.Stat(compose); err != nil {
		fmt.Fprintf(out, "No compose.sandbox.yaml in %q — skipping container removal.\n", *dir)
	} else {
		fmt.Fprintln(out, "Stopping and removing the sandbox container…")
		if err := runDocker(out, "compose", "-f", compose, "down"); err != nil {
			// Non-fatal: the container may already be gone or the compose file
			// broken — keep going so --rmi/--purge still run.
			fmt.Fprintln(out, "  (compose down failed — container may already be removed)")
		}
		if *rmi {
			fmt.Fprintln(out, "Removing the caldensec-sandbox image…")
			if err := runDocker(out, "rmi", "caldensec-sandbox"); err != nil {
				fmt.Fprintln(out, "  (image not present or still in use — skipped)")
			}
		}
	}

	if *purge {
		purgeSandboxArtifacts(out, *dir)
		fmt.Fprintln(out, "Kept sandbox-work/ (your mounted work dir).")
	}

	fmt.Fprintln(out, "Sandbox uninstall done.")
	return nil
}

// cmdSandboxModels opens the per-agent model picker on the sandbox's baked
// config (opencode-config/opencode.json) and then rebuilds the container so the
// new assignment is baked in. It is the sandbox counterpart of `caldensec
// models`, which otherwise targets the host config. --no-build edits only.
func cmdSandboxModels(out io.Writer, args []string) error {
	fs := flag.NewFlagSet("sandbox models", flag.ContinueOnError)
	dir := fs.String("dir", ".", "directory holding the sandbox artifacts")
	providerID := fs.String("provider", "caldensec", "OpenCode provider id managed by this tool")
	noBuild := fs.Bool("no-build", false, "edit the config only; skip the container rebuild")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg := filepath.Join(*dir, "opencode-config", "opencode.json")
	if _, err := os.Stat(cfg); err != nil {
		return fmt.Errorf("no opencode-config/opencode.json in %q — run `caldensec install` → Sandbox first (or pass --dir)", *dir)
	}

	// Reuse the standard per-agent picker, pointed at the staged config. It reads
	// the gateway baseURL already baked into that config and the key from the
	// keystore/env, so no passphrase is needed.
	if err := cmdModels([]string{"--config", cfg, "--provider", *providerID}); err != nil {
		return err
	}

	compose := filepath.Join(*dir, "compose.sandbox.yaml")
	if *noBuild {
		fmt.Fprintln(out, "Config updated. Apply it with:  docker compose -f "+compose+" up -d --build")
		return nil
	}
	if _, err := os.Stat(compose); err != nil {
		fmt.Fprintf(out, "Config updated. No compose.sandbox.yaml in %q — rebuild from your sandbox dir.\n", *dir)
		return nil
	}
	if !sandbox.DockerReady() {
		fmt.Fprintln(out, "Config updated. Docker is not running — rebuild once it is:  docker compose -f "+compose+" up -d --build")
		return nil
	}
	fmt.Fprintln(out, "Rebuilding the sandbox so the new models are baked in…")
	if err := runDocker(out, "compose", "-f", compose, "up", "-d", "--build"); err != nil {
		return fmt.Errorf("rebuilding the sandbox: %w", err)
	}
	fmt.Fprintln(out, "Done. Enter it with:  caldensec sandbox open")
	return nil
}

// runDocker runs a docker command, sending its output to out (stderr passthrough).
func runDocker(out io.Writer, args ...string) error {
	cmd := exec.Command("docker", args...)
	cmd.Stdout, cmd.Stderr = out, os.Stderr
	return cmd.Run()
}

// purgeSandboxArtifacts removes the generated sandbox scaffold under dir,
// deliberately never touching sandbox-work/. It returns the names it removed.
func purgeSandboxArtifacts(out io.Writer, dir string) []string {
	var removed []string
	for _, n := range []string{
		"Dockerfile.sandbox", "compose.sandbox.yaml", "caldensec-entrypoint.sh",
		"SANDBOX.md", ".dockerignore", "opencode-config",
	} {
		p := filepath.Join(dir, n)
		if _, err := os.Stat(p); err != nil {
			continue // not there, nothing to remove
		}
		if err := os.RemoveAll(p); err != nil {
			fmt.Fprintf(out, "  could not remove %s: %v\n", n, err)
			continue
		}
		fmt.Fprintln(out, "  removed "+n)
		removed = append(removed, n)
	}
	return removed
}

// cmdMCPRemove removes the named MCP server from opencode.json.
func cmdMCPRemove(out io.Writer, args []string) error {
	f, rest, err := parseMCPArgs(args, false, false)
	if err != nil {
		return err
	}
	configPath, err := mcpConfigPath(f)
	if err != nil {
		return err
	}
	if len(rest) == 0 {
		return fmt.Errorf("need the name of the MCP server to remove")
	}
	n, err := installer.DeleteConfigPaths(configPath, []string{"mcp." + rest[0]})
	if err != nil {
		return err
	}
	if n == 0 {
		fmt.Fprintf(out, "no MCP server %q found; nothing removed\n", rest[0])
		return nil
	}
	fmt.Fprintf(out, "removed MCP server %q\n", rest[0])
	return nil
}

// parseEnv turns KEY=VALUE strings into a map, erroring on a malformed entry.
func parseEnv(pairs []string) (map[string]string, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	env := make(map[string]string, len(pairs))
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("bad --env %q (want KEY=VALUE)", p)
		}
		env[k] = v
	}
	return env, nil
}

// cmdModels implements the `caldensec models` subcommand. The gateway server
// key is now mandatory: models are discovered gateway-first and the picker is
// gated behind a validated key. No model name is ever shown before a key has
// been entered AND validated — in the interactive flow the picker only opens
// with the validated authorized models, and --list prints nothing about models
// unless validation succeeds.
func cmdModels(args []string) error {
	defaultPath, err := defaultConfigPath()
	if err != nil {
		return err
	}

	fs := flag.NewFlagSet("models", flag.ContinueOnError)
	configPath := fs.String("config", defaultPath, "path to opencode.json")
	providerID := fs.String("provider", "caldensec", "OpenCode provider id managed by this tool")
	gatewayFlag := fs.String("gateway", "", "override the CaldenSec gateway base URL (default: $CALDENSEC_BASE, else the sealed gateway opened with $CALDENSEC_UNLOCK)")
	listOnly := fs.Bool("list", false, "print the managed agents and authorized models, then exit (needs a valid server key; no TUI, no changes)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath, *providerID)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	agents := cfg.ManagedAgents()
	if len(agents) == 0 {
		return fmt.Errorf("no agents bound to provider %q found in %s", *providerID, *configPath)
	}

	// Resolve the gateway base URL. Precedence: --gateway, $CALDENSEC_BASE, the
	// sealed URL (via $CALDENSEC_UNLOCK), and finally the baseURL OpenCode
	// already has in opencode.json — written at install once the sealed URL was
	// unlocked — so daily use needs no passphrase.
	// Resolve the gateway base URL (best effort). Precedence: --gateway,
	// $CALDENSEC_BASE, the sealed URL, then opencode.json's provider baseURL. When
	// none resolves, do NOT fail — fall through to the interactive menu, where the
	// user can enter a key or add their own OpenAI-compatible provider/models.
	base, berr := onboard.ResolveBase(*gatewayFlag, os.Getenv("CALDENSEC_BASE"))
	if berr != nil {
		base = cfg.ProviderBaseURL() // "" when there is no gateway configured at all
	}
	key := onboard.ResolveServerKey(os.Getenv("CALDENSEC_API_KEY"), keystore.Get)

	// --list is non-interactive: it requires an already-valid key and prints
	// nothing about models unless validation succeeds.
	if *listOnly {
		if base == "" || key == "" {
			return fmt.Errorf("no gateway or server key: run 'caldensec models' (without --list) to enter one or add your own models")
		}
		models, err := gateway.ValidateKey(base, key)
		if err != nil {
			return fmt.Errorf("validating server key: %w", err)
		}
		if len(models) == 0 {
			fmt.Println("server key is valid but authorizes no models")
			return nil
		}
		printListing(os.Stdout, agents, aggregateModels(cfg, *providerID, models))
		return nil
	}

	// Gather the gateway's authorized models when a gateway is reachable. None of
	// this is fatal: with no gateway, no key, or a stale key, we fall through to
	// the menu below, where the user can enter a key or add their own
	// OpenAI-compatible provider/models — never a terminal error.
	var models []config.Model
	if base != "" {
		if key != "" {
			if validated, verr := gateway.ValidateKey(base, key); verr == nil {
				models = validated
			}
		}
		if models == nil {
			// Offer interactive key entry against the gateway; a cancel or an
			// unreachable gateway just drops through to the menu.
			if validated, oerr := onboard.Run(base); oerr == nil {
				models = validated
			}
		}
	}

	// Top-level menu loop: Change models (assign, provider-aware) or Add custom
	// models (pick a provider by API, fetch its models, register it). Each model
	// assignment is applied immediately; the loop ends when the menu is quit.
	authPath := defaultAuthPath()
	assign := func(agentName, prov, modelID string) error {
		return config.SetAgentModel(*configPath, prov, agentName, modelID)
	}
	fetch := func(base, key string) ([]config.Model, error) { return gateway.FetchModels(base, key) }
	save := func(pid, _ /*name*/, baseURL, key string, ms []config.Model) error {
		if err := installer.AddProvider(*configPath, pid, baseURL, ms); err != nil {
			return err
		}
		return installer.SetOpenCodeCredential(authPath, pid, key)
	}

	var opts []tui.AddProviderOption
	for _, p := range providers.Known() {
		opts = append(opts, tui.AddProviderOption{ID: p.ID, Name: p.Name, BaseURL: p.BaseURL})
	}
	// Free-form entry: any OpenAI-compatible provider (user types id + base URL).
	opts = append(opts, tui.AddProviderOption{Name: "Custom (OpenAI-compatible)"})

	var applied []tui.Result
loop:
	for {
		choice, err := tui.RunMenu()
		if err != nil {
			return err
		}
		switch choice {
		case tui.MenuQuit:
			break loop
		case tui.MenuChange:
			// Reload so newly added custom providers show up as categories.
			cur, lerr := config.Load(*configPath, *providerID)
			if lerr != nil {
				return fmt.Errorf("loading config: %w", lerr)
			}
			all := aggregateModels(cur, *providerID, models)
			got, rerr := tui.Run(cur.ManagedAgents(), all, assign)
			if rerr != nil {
				return fmt.Errorf("running TUI: %w", rerr)
			}
			applied = append(applied, got...)
		case tui.MenuAddCustom:
			if _, aerr := tui.RunAddCustom(opts, fetch, save); aerr != nil {
				return fmt.Errorf("adding custom provider: %w", aerr)
			}
		}
	}

	if len(applied) == 0 {
		fmt.Println("no changes made")
		return nil
	}
	for _, r := range applied {
		fmt.Printf("agent %s: %s -> %s/%s\n", r.Agent.Name, r.Agent.CurrentModel, r.Model.Provider, r.Model.ID)
	}
	return nil
}

// defaultAuthPath resolves OpenCode's credential store: $OPENCODE_AUTH_PATH,
// else $XDG_DATA_HOME/opencode/auth.json, else ~/.local/share/opencode/auth.json.
func defaultAuthPath() string {
	if v := os.Getenv("OPENCODE_AUTH_PATH"); v != "" {
		return v
	}
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "opencode", "auth.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join("opencode", "auth.json")
	}
	return filepath.Join(home, ".local", "share", "opencode", "auth.json")
}

// cmdUpdate implements `caldensec update`: it fetches the signed manifest from
// the key-gated update endpoint derived from the configured gateway, verifies
// the release for this platform, and replaces the running binary. No GitHub
// access is needed — the gateway key is the gate. With --keygen it only prints a
// fresh Ed25519 signing key pair for enabling signature verification.
func cmdUpdate(out io.Writer, args []string) error {
	defaultPath, err := defaultConfigPath()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	configPath := fs.String("config", defaultPath, "path to opencode.json")
	providerID := fs.String("provider", "caldensec", "OpenCode provider id managed by this tool")
	keygen := fs.Bool("keygen", false, "generate an Ed25519 signing key pair for release signing and exit")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *keygen {
		return updateKeygen(out)
	}

	base, err := resolveUpdateBase(*configPath, *providerID)
	if err != nil {
		return err
	}
	return updater.Run(out, updater.Options{
		Base:           base,
		Key:            resolveGatewayKey(*providerID, *configPath),
		CurrentVersion: version,
	})
}

// resolveUpdateBase finds the update endpoint without a passphrase:
// $CALDENSEC_UPDATE_BASE, then the URL stored at install (sealed update base,
// unsealed into the keystore), then the gateway host from opencode.json.
func resolveUpdateBase(configPath, providerID string) (string, error) {
	if v := strings.TrimRight(os.Getenv("CALDENSEC_UPDATE_BASE"), "/"); v != "" {
		return v, nil
	}
	if v, err := keystore.Get(updateBaseAccount); err == nil && v != "" {
		return strings.TrimRight(v, "/"), nil
	}
	if b := providerBaseURL(configPath, providerID); b != "" {
		if base, err := updater.BaseFromProvider(b); err == nil {
			return base, nil
		}
	}
	// Last resort: unseal the endpoint baked into the binary with the install
	// passphrase, and remember it so a later update needs no passphrase. This
	// covers a client whose install never stored the base (no gateway staged).
	if pass := strings.TrimSpace(os.Getenv("CALDENSEC_UNLOCK")); pass != "" {
		if ub, err := onboard.UnsealUpdateBase(pass); err == nil && ub != "" {
			_ = keystore.Set(updateBaseAccount, ub)
			return strings.TrimRight(ub, "/"), nil
		}
	}
	return "", fmt.Errorf("could not locate the update endpoint: set $CALDENSEC_UPDATE_BASE=<url>, " +
		"or $CALDENSEC_UNLOCK=<passphrase> to use the endpoint sealed into this binary, " +
		"or configure the gateway in opencode.json")
}

// resolveGatewayKey finds the gateway key without prompting: $CALDENSEC_API_KEY,
// then the caldensec keystore, then OpenCode's auth.json. Returns "" if none.
func resolveGatewayKey(providerID, configPath string) string {
	if v := os.Getenv("CALDENSEC_API_KEY"); v != "" {
		return v
	}
	if v, err := keystore.Get(keyAccount); err == nil && v != "" {
		return v
	}
	if data, err := os.ReadFile(defaultAuthPath()); err == nil {
		if k := gjson.GetBytes(data, providerID+".key").String(); k != "" {
			return k
		}
	}
	return ""
}

// providerBaseURL reads provider.<id>.options.baseURL from opencode.json; "" if
// absent or unreadable.
func providerBaseURL(configPath, providerID string) string {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}
	return gjson.GetBytes(data, "provider."+providerID+".options.baseURL").String()
}

// updateKeygen prints a fresh Ed25519 key pair for signing releases: the private
// key is kept as a CI secret that signs each archive, and the public key is
// pasted into internal/updater's signingPubKeyB64 (then rebuilt) to enforce
// signature verification on updates.
func updateKeygen(out io.Writer) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generating key pair: %w", err)
	}
	fmt.Fprintln(out, "Ed25519 signing key pair generated.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "PRIVATE key — keep secret; use it as the CI secret that signs each release archive:")
	fmt.Fprintln(out, "  "+base64.StdEncoding.EncodeToString(priv))
	fmt.Fprintln(out)
	fmt.Fprintln(out, "PUBLIC key — paste into internal/updater/updater.go signingPubKeyB64, then rebuild:")
	fmt.Fprintln(out, "  "+base64.StdEncoding.EncodeToString(pub))
	return nil
}

// aggregateModels combines the gateway (primary provider) models with the models
// of every other provider present in opencode.json, tagging each with its
// provider and — for custom providers — its display name as the category, so
// the picker groups them like Calden/Cloud.
func aggregateModels(cfg *config.Config, primary string, gateway []config.Model) []config.Model {
	out := make([]config.Model, 0, len(gateway))
	for _, m := range gateway {
		m.Provider = primary
		out = append(out, m)
	}
	for _, pid := range cfg.ProviderIDs() {
		if pid == primary {
			continue
		}
		label := pid
		if p, ok := providers.Lookup(pid); ok {
			label = p.Name
		}
		for _, cm := range cfg.ProviderModels(pid) {
			cm.CategoryLabel = label
			out = append(out, cm)
		}
	}
	return out
}

// keyAccount is the keystore account under which the gateway server key is
// stored (matches internal/onboard).
const keyAccount = "gateway"

// cmdKey implements `caldensec key`: with no flag it reports where the server
// key resolves from; with --reset it clears the stored key.
func cmdKey(args []string) error {
	fs := flag.NewFlagSet("key", flag.ContinueOnError)
	reset := fs.Bool("reset", false, "delete the stored gateway server key (keychain + fallback file)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	env := os.Getenv("CALDENSEC_API_KEY")

	if *reset {
		if err := keystore.Delete(keyAccount); err != nil {
			return fmt.Errorf("clearing stored key: %w", err)
		}
		fmt.Println("Stored gateway server key cleared.")
		if env != "" {
			fmt.Println("Note: $CALDENSEC_API_KEY is set in your environment and still takes precedence.")
			fmt.Println("  Drop it with:  unset CALDENSEC_API_KEY")
		}
		return nil
	}

	stored := ""
	if v, err := keystore.Get(keyAccount); err == nil {
		stored = v
	}
	fmt.Print(keyStatus(env, stored))
	return nil
}

// cmdSeal implements `caldensec seal <gateway-url>`: a distributor-only helper
// that encrypts the gateway URL under the passphrase in $CALDENSEC_UNLOCK and
// prints a ready-to-paste `var sealedBase = "..."` for internal/onboard/sealed.go.
// The passphrase is then shared out-of-band with whoever may install.
func cmdSeal(out io.Writer, args []string) error {
	fs := flag.NewFlagSet("seal", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: caldensec seal <gateway-url>  (prompts for the passphrase, or set $CALDENSEC_UNLOCK)")
	}

	pass, err := resolvePassphrase("Passphrase to seal the gateway URL: ")
	if err != nil {
		return err
	}

	blob, err := urlseal.Seal(fs.Arg(0), pass)
	if err != nil {
		return fmt.Errorf("sealing URL: %w", err)
	}

	fmt.Fprintln(out, "Sealed. Paste this into internal/onboard/sealed.go, then rebuild:")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "var sealedBase = %q\n", blob)
	return nil
}

// updateBaseAccount is the keystore account holding the (unsealed) update
// endpoint URL, written at install so `caldensec update` needs no passphrase.
const updateBaseAccount = "update-base"

// cmdSealUpdate implements `caldensec seal-update <url>`: seals the update
// endpoint URL with the install passphrase and prints a ready-to-paste
// `var sealedUpdateBase = "..."` for internal/onboard/sealed.go.
func cmdSealUpdate(out io.Writer, args []string) error {
	fs := flag.NewFlagSet("seal-update", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: caldensec seal-update <update-url>  (prompts for the passphrase, or set $CALDENSEC_UNLOCK)")
	}
	pass, err := resolvePassphrase("Passphrase to seal the update URL: ")
	if err != nil {
		return err
	}
	blob, err := urlseal.Seal(fs.Arg(0), pass)
	if err != nil {
		return fmt.Errorf("sealing URL: %w", err)
	}
	fmt.Fprintln(out, "Sealed. Paste this into internal/onboard/sealed.go, then rebuild:")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "var sealedUpdateBase = %q\n", blob)
	return nil
}

// cmdSealTemplate reads the current opencode.json, captures each managed
// agent's assigned model, seals that agent→model map (with $CALDENSEC_UNLOCK),
// and prints a ready-to-paste `var sealedTemplate = "..."`. On install, after
// unlocking, agents get these models automatically — with the real names sealed,
// never in the repo. Set up the agents the way you want (caldensec models) first.
func cmdSealTemplate(out io.Writer, args []string) error {
	defaultPath, err := defaultConfigPath()
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("seal-template", flag.ContinueOnError)
	configPath := fs.String("config", defaultPath, "path to opencode.json")
	providerID := fs.String("provider", "caldensec", "OpenCode provider id")
	slot := fs.String("slot", "calden", "which template slot to seal: calden, cloud or both")
	fromJSON := fs.String("from-json", "", `agent→model JSON to seal (e.g. '{"my-agent":"..."}'); default: read the current assignments`)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *slot != "calden" && *slot != "cloud" && *slot != "both" {
		return fmt.Errorf("--slot must be calden, cloud or both, got %q", *slot)
	}
	pass, err := resolvePassphrase("CaldenSec install passphrase: ")
	if err != nil {
		return err
	}

	// The template map: from --from-json, or captured from the current config.
	tmpl := map[string]string{}
	if *fromJSON != "" {
		if err := json.Unmarshal([]byte(*fromJSON), &tmpl); err != nil {
			return fmt.Errorf("parsing --from-json: %w", err)
		}
	} else {
		cfg, err := config.Load(*configPath, *providerID)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		for _, a := range cfg.ManagedAgents() {
			if a.CurrentModel != "" {
				tmpl[a.Name] = a.CurrentModel
			}
		}
	}
	if len(tmpl) == 0 {
		return fmt.Errorf("no agent→model assignments (pass --from-json or assign models first with 'caldensec models')")
	}

	raw, err := json.Marshal(tmpl)
	if err != nil {
		return fmt.Errorf("encoding template: %w", err)
	}
	blob, err := urlseal.Seal(string(raw), pass)
	if err != nil {
		return fmt.Errorf("sealing template: %w", err)
	}

	varName := "sealedAgentsCalden"
	switch *slot {
	case "cloud":
		varName = "sealedAgentsCloud"
	case "both":
		varName = "sealedAgentsBoth"
	}
	fmt.Fprintf(out, "Sealed %d assignments into the %s slot:\n", len(tmpl), *slot)
	for name, m := range tmpl {
		fmt.Fprintf(out, "  %-22s → %s\n", name, m)
	}
	fmt.Fprintln(out, "\nPaste this into internal/onboard/sealed.go, then rebuild:")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "var %s = %q\n", varName, blob)
	return nil
}

// keyStatus renders where the server key resolves from, given the environment
// value and the stored value. The environment variable takes precedence over
// the stored key, matching onboard.ResolveServerKey.
func keyStatus(env, stored string) string {
	switch {
	case env != "":
		return fmt.Sprintf("Server key: using $CALDENSEC_API_KEY (env) - %s\n"+
			"This shadows the stored key. Drop the override with 'unset CALDENSEC_API_KEY';\n"+
			"clear the stored one with 'caldensec key --reset'.\n", keystore.Mask(env))
	case stored != "":
		return fmt.Sprintf("Server key: stored in the keychain - %s\n"+
			"Replace it by running 'caldensec install' (or 'caldensec models') and entering a new one,\n"+
			"or clear it with 'caldensec key --reset'.\n", keystore.Mask(stored))
	default:
		return "Server key: none stored. 'caldensec install' or 'caldensec models' will prompt for one.\n"
	}
}

// printListing writes a non-interactive summary of the managed agents (grouped
// into Primary / Subagents) and the available models (grouped by category) to
// w. It is the --list smoke test: it never opens the TUI and never writes.
func printListing(w io.Writer, agents []config.Agent, models []config.Model) {
	var primary, sub []config.Agent
	for _, a := range agents {
		if a.Mode == "primary" {
			primary = append(primary, a)
		} else {
			sub = append(sub, a)
		}
	}
	sort.Slice(primary, func(i, j int) bool { return primary[i].Name < primary[j].Name })
	sort.Slice(sub, func(i, j int) bool { return sub[i].Name < sub[j].Name })

	fmt.Fprintln(w, "Agents:")
	printAgentSection(w, "Primary", primary)
	printAgentSection(w, "Subagents", sub)

	fmt.Fprintf(w, "\nModels (%d):\n", len(models))
	for _, group := range config.GroupModelsByCategory(models) {
		fmt.Fprintf(w, "  %s:\n", group.Category)
		for _, m := range group.Models {
			fmt.Fprintf(w, "    %s\n", m.ID)
		}
	}
}

// printAgentSection prints one labeled agent section, if it has any agents.
func printAgentSection(w io.Writer, label string, agents []config.Agent) {
	if len(agents) == 0 {
		return
	}
	fmt.Fprintf(w, "  %s:\n", label)
	for _, a := range agents {
		fmt.Fprintf(w, "    %-22s -> %s\n", a.Name, a.CurrentModel)
	}
}

// defaultConfigPath resolves ~/.config/opencode/opencode.json using the
// current user's home directory. This is the only place a user path is
// derived; it is never hardcoded.
func defaultConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, ".config", "opencode", "opencode.json"), nil
}
