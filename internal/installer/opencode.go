package installer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// baseURLBackupSuffix names the backup taken before setting the provider base URL.
const baseURLBackupSuffix = ".bak-retoia-baseurl"

// SetProviderBaseURL surgically sets provider.<providerID>.options.baseURL in the
// opencode.json at configPath, so OpenCode talks to the right gateway. It backs
// the file up, then re-reads and validates the result is still JSON, restoring
// the backup on failure.
func SetProviderBaseURL(configPath, providerID, baseURL string) error {
	original, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config %q: %w", configPath, err)
	}

	backupPath := configPath + baseURLBackupSuffix
	if err := os.WriteFile(backupPath, original, 0o644); err != nil {
		return fmt.Errorf("writing backup %q: %w", backupPath, err)
	}

	path := fmt.Sprintf("provider.%s.options.baseURL", providerID)
	updated, err := sjson.SetBytes(original, path, baseURL)
	if err != nil {
		return fmt.Errorf("setting %s: %w", path, err)
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

// SetOpenCodeCredential writes an API-key credential for provider into
// OpenCode's auth store (a JSON object of provider -> credential) at authPath,
// as {"type":"api","key":key}. Other providers in the file are preserved. The
// file is created (with a 0700 dir and 0600 file) if it does not exist. This is
// what `opencode auth login` writes, so OpenCode uses the key at inference time.
func SetOpenCodeCredential(authPath, provider, key string) error {
	creds := map[string]json.RawMessage{}

	data, err := os.ReadFile(authPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading auth store %q: %w", authPath, err)
	}
	if err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &creds); err != nil {
			return fmt.Errorf("parsing auth store %q: %w", authPath, err)
		}
	}

	entry, err := json.Marshal(map[string]string{"type": "api", "key": key})
	if err != nil {
		return fmt.Errorf("encoding credential: %w", err)
	}
	creds[provider] = entry

	out, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding auth store: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(authPath), 0o700); err != nil {
		return fmt.Errorf("creating auth store dir: %w", err)
	}
	if err := os.WriteFile(authPath, out, 0o600); err != nil {
		return fmt.Errorf("writing auth store %q: %w", authPath, err)
	}
	// Enforce 0600 even if the file pre-existed with looser perms.
	if err := os.Chmod(authPath, 0o600); err != nil {
		return fmt.Errorf("setting auth store permissions: %w", err)
	}
	return nil
}
