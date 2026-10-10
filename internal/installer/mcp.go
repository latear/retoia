package installer

import (
	"fmt"
	"os"
	"sort"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// mcpBackupSuffix names the backup taken before writing an MCP server block.
const mcpBackupSuffix = ".bak-retoia-mcp"

// MCPSpec describes one MCP server as OpenCode expects it under `.mcp.<name>`.
// A local server runs a stdio Command; a remote server is reached at URL. Only
// the fields relevant to Type are written, so a local spec never emits a url and
// a remote spec never emits a command.
type MCPSpec struct {
	Type        string            // "local" or "remote"
	Command     []string          // local: the stdio launch command, e.g. ["npx","-y","pkg"]
	URL         string            // remote: the server URL
	Environment map[string]string // local: environment variables (values may use {env:VAR})
	Headers     map[string]string // remote: request headers (e.g. Authorization)
}

// entry builds the opencode.json object for this spec, including only the fields
// that apply to its type.
func (s MCPSpec) entry() (map[string]any, error) {
	e := map[string]any{"type": s.Type}
	switch s.Type {
	case "local":
		if len(s.Command) == 0 {
			return nil, fmt.Errorf("local MCP server needs a command")
		}
		e["command"] = s.Command
		if len(s.Environment) > 0 {
			e["environment"] = s.Environment
		}
	case "remote":
		if s.URL == "" {
			return nil, fmt.Errorf("remote MCP server needs a url")
		}
		e["url"] = s.URL
		if len(s.Headers) > 0 {
			e["headers"] = s.Headers
		}
	default:
		return nil, fmt.Errorf("unknown MCP server type %q (want local or remote)", s.Type)
	}
	return e, nil
}

// ConfigureMCP surgically registers an MCP server under `.mcp.<name>` in the
// opencode.json at configPath. Any other mcp entries (and the rest of the file)
// are preserved: the write is a single sjson patch, never a full re-marshal. It
// backs the file up first, then re-reads and validates the result is still JSON,
// restoring the backup on failure.
func ConfigureMCP(configPath, name string, spec MCPSpec) error {
	entry, err := spec.entry()
	if err != nil {
		return fmt.Errorf("mcp %q: %w", name, err)
	}

	original, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config %q: %w", configPath, err)
	}

	backupPath := configPath + mcpBackupSuffix
	if err := os.WriteFile(backupPath, original, 0o644); err != nil {
		return fmt.Errorf("writing backup %q: %w", backupPath, err)
	}

	// gjson/sjson treat "." in a key as a path separator, so an MCP name that
	// contains one must be escaped; names are otherwise written literally.
	updated, err := sjson.SetBytes(original, "mcp."+sjsonKey(name), entry)
	if err != nil {
		return fmt.Errorf("setting mcp.%s: %w", name, err)
	}

	if err := os.WriteFile(configPath, updated, 0o644); err != nil {
		return fmt.Errorf("writing config %q: %w", configPath, err)
	}

	written, err := os.ReadFile(configPath)
	if err != nil || !gjson.ValidBytes(written) {
		if restoreErr := os.WriteFile(configPath, original, 0o644); restoreErr != nil {
			return fmt.Errorf("written config failed validation and restore failed: %w", restoreErr)
		}
		return fmt.Errorf("written config %q failed JSON validation; restored original", configPath)
	}
	return nil
}

// ConfiguredMCP returns the names of every MCP server currently registered under
// `.mcp` in the opencode.json at configPath, sorted. A missing file or missing
// mcp block yields an empty slice, not an error.
func ConfiguredMCP(configPath string) ([]string, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading config %q: %w", configPath, err)
	}
	var names []string
	gjson.GetBytes(data, "mcp").ForEach(func(k, _ gjson.Result) bool {
		names = append(names, k.String())
		return true
	})
	sort.Strings(names)
	return names, nil
}

// sjsonKey escapes the characters gjson/sjson treat specially in a path segment
// so a name is used as a literal key.
func sjsonKey(name string) string {
	var b []byte
	for i := 0; i < len(name); i++ {
		if c := name[i]; c == '.' || c == '*' || c == '?' || c == '\\' {
			b = append(b, '\\')
		}
		b = append(b, name[i])
	}
	return string(b)
}
