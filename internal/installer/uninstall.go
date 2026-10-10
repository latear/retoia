package installer

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// uninstallBackupSuffix names the backup taken before removing config keys.
const uninstallBackupSuffix = ".bak-retoia-uninstall"

// DeleteConfigPaths removes each gjson/sjson path from the opencode.json at
// configPath (e.g. "agent.Calden-Orchestrator", "provider.caldensec",
// "mcp.engram"), backing the file up first and restoring it if the result is
// not valid JSON. Missing paths are skipped. Returns how many paths existed and
// were removed.
func DeleteConfigPaths(configPath string, paths []string) (int, error) {
	original, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil // nothing to clean
		}
		return 0, fmt.Errorf("reading config %q: %w", configPath, err)
	}

	updated := original
	removed := 0
	for _, p := range paths {
		if !gjson.GetBytes(updated, p).Exists() {
			continue
		}
		next, derr := sjson.DeleteBytes(updated, p)
		if derr != nil {
			return removed, fmt.Errorf("removing %s: %w", p, derr)
		}
		updated = next
		removed++
	}
	if removed == 0 {
		return 0, nil
	}

	backupPath := configPath + uninstallBackupSuffix
	if err := os.WriteFile(backupPath, original, 0o644); err != nil {
		return 0, fmt.Errorf("writing backup %q: %w", backupPath, err)
	}
	if err := os.WriteFile(configPath, updated, 0o644); err != nil {
		return 0, fmt.Errorf("writing config %q: %w", configPath, err)
	}
	if written, err := os.ReadFile(configPath); err != nil || !gjson.ValidBytes(written) {
		if restoreErr := os.WriteFile(configPath, original, 0o644); restoreErr != nil {
			return 0, fmt.Errorf("cleaned config failed validation and restore failed: %w", restoreErr)
		}
		return 0, fmt.Errorf("cleaned config %q failed JSON validation; restored original", configPath)
	}
	return removed, nil
}

// RemoveOpenCodeCredential deletes one provider entry from OpenCode's auth
// store, preserving the others. It is a no-op when the file or entry is absent.
func RemoveOpenCodeCredential(authPath, provider string) error {
	data, err := os.ReadFile(authPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading auth store %q: %w", authPath, err)
	}
	creds := map[string]json.RawMessage{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &creds); err != nil {
			return fmt.Errorf("parsing auth store %q: %w", authPath, err)
		}
	}
	if _, ok := creds[provider]; !ok {
		return nil
	}
	delete(creds, provider)

	out, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding auth store: %w", err)
	}
	if err := os.WriteFile(authPath, out, 0o600); err != nil {
		return fmt.Errorf("writing auth store %q: %w", authPath, err)
	}
	return os.Chmod(authPath, 0o600)
}
