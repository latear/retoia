package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// tuiBackupSuffix names the backup taken before writing tui.json.
const tuiBackupSuffix = ".bak-retoia-tui"

// RetoiaThemeName is the OpenCode theme the suite ships and selects in tui.json.
const RetoiaThemeName = "retoia"

// retoiaThemeJSON is the Reto IA OpenCode theme, in the Late.Ar palette: a flat,
// minimalist dark look on a black background where the sidebar, the editor, and
// the input box share ONE uniform background (nothing reads as a separate "box"),
// and the only structure is the blue border lines. Text is white; the Late.Ar
// blue (#0000FF) is reserved for the lines, the accent/focus, and the wordmark.
// Error/warning/success stay as functional semantic colors (not brand accents).
const retoiaThemeJSON = `{
  "$schema": "https://opencode.ai/theme.json",
  "theme": {
    "primary": "#0000FF",
    "secondary": "#9AA0A6",
    "accent": "#0000FF",
    "background": "#000000",
    "backgroundPanel": "#000000",
    "backgroundElement": "#000000",
    "text": "#FFFFFF",
    "textMuted": "#9AA0A6",
    "border": "#0000FF",
    "borderActive": "#0000FF",
    "borderSubtle": "#1C1C22",
    "error": "#FF5C5C",
    "warning": "#B8862B",
    "success": "#4FA96A",
    "info": "#9AA0A6"
  }
}
`

// WriteRetoiaTheme writes the "retoia" theme into <configDir>/themes/retoia.json
// (creating the dir), so "theme": "retoia" in tui.json resolves to it. Returns
// the written path.
func WriteRetoiaTheme(configDir string) (string, error) {
	dir := filepath.Join(configDir, "themes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating themes dir %q: %w", dir, err)
	}
	path := filepath.Join(dir, RetoiaThemeName+".json")
	if err := os.WriteFile(path, []byte(retoiaThemeJSON), 0o644); err != nil {
		return "", fmt.Errorf("writing theme %q: %w", path, err)
	}
	return path, nil
}

// RetoiaLogoPluginName is the base name of the home-screen logo TUI plugin file.
const RetoiaLogoPluginName = "retoia-logo.tsx"

// retoiaLogoPluginTSX is an OpenCode TUI plugin that renders the Reto IA
// wordmark in the home_logo slot. OpenCode's own startup logo is compiled into
// its binary and not themeable; a plugin's home_logo slot is the supported way
// to replace it (the same mechanism gentle-ai's logo uses).
const retoiaLogoPluginTSX = `// @ts-nocheck
/** @jsxImportSource @opentui/solid */
import type { TuiPlugin } from "@opencode-ai/plugin/tui"
import { useTerminalDimensions } from "@opentui/solid"
import { createMemo } from "solid-js"

const id = "retoia-logo"

const art = [
  "██████╗ ███████╗████████╗ ██████╗     ██╗ █████╗ ",
  "██╔══██╗██╔════╝╚══██╔══╝██╔═══██╗    ██║██╔══██╗",
  "██████╔╝█████╗     ██║   ██║   ██║    ██║███████║",
  "██╔══██╗██╔══╝     ██║   ██║   ██║    ██║██╔══██║",
  "██║  ██║███████╗   ██║   ╚██████╔╝    ██║██║  ██║",
  "╚═╝  ╚═╝╚══════╝   ╚═╝    ╚═════╝     ╚═╝╚═╝  ╚═╝",
]

// Pad every line to the widest one so centering each line keeps the block
// aligned (the ANSI-shadow rows differ in length, which otherwise staggers it).
const artW = Math.max(...art.map((l) => Array.from(l).length))
const artFull = art.map((l) => l + " ".repeat(artW - Array.from(l).length))

const compactArt = ["▞ Reto IA ▚"]

const Logo = () => {
  const dim = useTerminalDimensions()
  const lines = createMemo(() => (dim().width >= artW ? artFull : compactArt))
  return (
    <box flexDirection="column" alignItems="center">
      {lines().map((line) => (
        <text fg="blue">{line}</text>
      ))}
    </box>
  )
}

const tui: TuiPlugin = async (api) => {
  api.slots.register({
    id,
    order: 100,
    slots: {
      home_logo() {
        return <Logo />
      },
    },
  })
}

const plugin = { id: "retoia-logo", tui }
export default plugin
`

// WriteRetoiaLogoPlugin writes the Reto IA home-screen logo TUI plugin into
// <configDir>/tui-plugins/retoia-logo.tsx and returns its path, for registration
// in tui.json's "plugin" list.
func WriteRetoiaLogoPlugin(configDir string) (string, error) {
	dir := filepath.Join(configDir, "tui-plugins")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating tui-plugins dir %q: %w", dir, err)
	}
	path := filepath.Join(dir, RetoiaLogoPluginName)
	if err := os.WriteFile(path, []byte(retoiaLogoPluginTSX), 0o644); err != nil {
		return "", fmt.Errorf("writing logo plugin %q: %w", path, err)
	}
	return path, nil
}

// RemoveRetoiaTheme deletes the shipped theme file and, when tui.json still
// selects "retoia", clears that key so OpenCode falls back to its default. No-op
// for anything it did not set.
func RemoveRetoiaTheme(configDir string) error {
	themePath := filepath.Join(configDir, "themes", RetoiaThemeName+".json")
	if err := os.Remove(themePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing theme %q: %w", themePath, err)
	}
	tuiPath := filepath.Join(configDir, "tui.json")
	data, err := os.ReadFile(tuiPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !gjson.ValidBytes(data) || gjson.GetBytes(data, "theme").String() != RetoiaThemeName {
		return nil // not ours to clear
	}
	updated, err := sjson.DeleteBytes(data, "theme")
	if err != nil {
		return fmt.Errorf("clearing tui theme: %w", err)
	}
	return os.WriteFile(tuiPath, updated, 0o644)
}

// SetTUITheme surgically sets "theme" in tui.json, creating the file if missing,
// backing it up, and revalidating (restoring on failure). A no-op when already set.
func SetTUITheme(tuiPath, theme string) error {
	var original []byte
	if b, err := os.ReadFile(tuiPath); err == nil {
		original = b
	} else if os.IsNotExist(err) {
		original = []byte(`{"$schema":"https://opencode.ai/tui.json"}`)
	} else {
		return fmt.Errorf("reading %q: %w", tuiPath, err)
	}
	if !gjson.ValidBytes(original) {
		return fmt.Errorf("%q is not valid JSON", tuiPath)
	}
	if gjson.GetBytes(original, "theme").String() == theme {
		return nil // already selected
	}
	backupPath := tuiPath + tuiBackupSuffix
	if err := os.WriteFile(backupPath, original, 0o644); err != nil {
		return fmt.Errorf("writing backup %q: %w", backupPath, err)
	}
	updated, err := sjson.SetBytes(original, "theme", theme)
	if err != nil {
		return fmt.Errorf("setting tui theme: %w", err)
	}
	if err := os.WriteFile(tuiPath, updated, 0o644); err != nil {
		return fmt.Errorf("writing %q: %w", tuiPath, err)
	}
	written, rerr := os.ReadFile(tuiPath)
	if rerr != nil || !gjson.ValidBytes(written) {
		if restoreErr := os.WriteFile(tuiPath, original, 0o644); restoreErr != nil {
			return fmt.Errorf("tui.json failed validation and restore failed: %w", restoreErr)
		}
		return fmt.Errorf("written %q failed JSON validation; restored original", tuiPath)
	}
	return nil
}

// RetoiaTUIPlugins are the OpenCode TUI plugins the suite installs for a richer
// sidebar: sub-agent activity and a task/plan panel. They are community plugins
// that OpenCode fetches (via Bun) from npm at start and caches under
// ~/.cache/opencode/node_modules (require OpenCode 1.18+). Versions are pinned
// for reproducibility and to skip the npm "latest" lookup on a cold cache. The
// features themselves (subagents, todos) are native — these only add the panels.
var RetoiaTUIPlugins = []string{
	"opencode-agents-monitor@0.1.2", // sub-agents in the sidebar
	"opencode-todo@0.0.3",           // task / plan backlog panel
}

// pluginBase strips a trailing "@version" so two entries for the same package
// (at different versions) are treated as one. A leading '@' (npm scope) is kept.
func pluginBase(s string) string {
	if i := strings.LastIndexByte(s, '@'); i > 0 {
		return s[:i]
	}
	return s
}

// RemoveTUIPlugins removes the given plugins (matched by base name, ignoring
// version) from the "plugin" array of the tui.json at tuiPath, preserving every
// other entry. A missing file or empty array is a no-op. It returns how many
// entries were removed. Backup + revalidate, restoring on failure.
func RemoveTUIPlugins(tuiPath string, plugins []string) (int, error) {
	original, err := os.ReadFile(tuiPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("reading %q: %w", tuiPath, err)
	}
	if !gjson.ValidBytes(original) {
		return 0, fmt.Errorf("%q is not valid JSON", tuiPath)
	}
	drop := map[string]bool{}
	for _, p := range plugins {
		drop[pluginBase(p)] = true
	}
	var kept []string
	removed := 0
	gjson.GetBytes(original, "plugin").ForEach(func(_, v gjson.Result) bool {
		s := v.String()
		if drop[pluginBase(s)] {
			removed++
		} else {
			kept = append(kept, s)
		}
		return true
	})
	if removed == 0 {
		return 0, nil
	}

	backupPath := tuiPath + tuiBackupSuffix
	if err := os.WriteFile(backupPath, original, 0o644); err != nil {
		return 0, fmt.Errorf("writing backup %q: %w", backupPath, err)
	}
	updated, err := sjson.SetBytes(original, "plugin", kept)
	if err != nil {
		return 0, fmt.Errorf("setting tui plugins: %w", err)
	}
	if err := os.WriteFile(tuiPath, updated, 0o644); err != nil {
		return 0, fmt.Errorf("writing %q: %w", tuiPath, err)
	}
	written, err := os.ReadFile(tuiPath)
	if err != nil || !gjson.ValidBytes(written) {
		if restoreErr := os.WriteFile(tuiPath, original, 0o644); restoreErr != nil {
			return 0, fmt.Errorf("tui.json failed validation and restore failed: %w", restoreErr)
		}
		return 0, fmt.Errorf("written %q failed JSON validation; restored original", tuiPath)
	}
	return removed, nil
}

// ConfigureTUIPlugins merges plugins into the "plugin" array of the tui.json at
// tuiPath, preserving every existing entry (never clobbering) and adding only
// the packages not already present (matched by base name, ignoring version). It
// creates the file if missing, backs it up, and revalidates the result,
// restoring on failure. It returns the entries actually added.
func ConfigureTUIPlugins(tuiPath string, plugins []string) ([]string, error) {
	var original []byte
	if b, err := os.ReadFile(tuiPath); err == nil {
		original = b
	} else if os.IsNotExist(err) {
		original = []byte(`{"$schema":"https://opencode.ai/tui.json"}`)
	} else {
		return nil, fmt.Errorf("reading %q: %w", tuiPath, err)
	}
	if !gjson.ValidBytes(original) {
		return nil, fmt.Errorf("%q is not valid JSON", tuiPath)
	}

	// Collect existing plugin entries and their base names.
	var merged []string
	haveBase := map[string]bool{}
	gjson.GetBytes(original, "plugin").ForEach(func(_, v gjson.Result) bool {
		s := v.String()
		merged = append(merged, s)
		haveBase[pluginBase(s)] = true
		return true
	})

	var added []string
	for _, p := range plugins {
		if haveBase[pluginBase(p)] {
			continue // same package already present (any version) — leave it
		}
		merged = append(merged, p)
		haveBase[pluginBase(p)] = true
		added = append(added, p)
	}
	if len(added) == 0 {
		return nil, nil // nothing to do; leave the file untouched
	}

	backupPath := tuiPath + tuiBackupSuffix
	if err := os.WriteFile(backupPath, original, 0o644); err != nil {
		return nil, fmt.Errorf("writing backup %q: %w", backupPath, err)
	}
	updated, err := sjson.SetBytes(original, "plugin", merged)
	if err != nil {
		return nil, fmt.Errorf("setting tui plugins: %w", err)
	}
	if err := os.WriteFile(tuiPath, updated, 0o644); err != nil {
		return nil, fmt.Errorf("writing %q: %w", tuiPath, err)
	}
	written, err := os.ReadFile(tuiPath)
	if err != nil || !gjson.ValidBytes(written) {
		if restoreErr := os.WriteFile(tuiPath, original, 0o644); restoreErr != nil {
			return nil, fmt.Errorf("tui.json failed validation and restore failed: %w", restoreErr)
		}
		return nil, fmt.Errorf("written %q failed JSON validation; restored original", tuiPath)
	}
	return added, nil
}
