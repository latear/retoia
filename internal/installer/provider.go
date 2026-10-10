package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/ficopix/caldensec/internal/config"
)

// providerBackupSuffix names the backup taken before writing a provider block.
const providerBackupSuffix = ".bak-calden-provider"

// Default per-model token limits written when a model carries none. The gateway
// does not report context windows, so these are conservative starting values the
// user can tune (globally via env, or per model by editing opencode.json).
const (
	defaultContextLimit = 32768
	defaultOutputLimit  = 8192
)

// defaultModelLimits returns the context/output limits to use for a model that
// carries none, honoring the CALDENSEC_CONTEXT_LIMIT / CALDENSEC_OUTPUT_LIMIT
// environment overrides when they are positive integers.
func defaultModelLimits() (context, output int) {
	context, output = defaultContextLimit, defaultOutputLimit
	if v, err := strconv.Atoi(os.Getenv("CALDENSEC_CONTEXT_LIMIT")); err == nil && v > 0 {
		context = v
	}
	if v, err := strconv.Atoi(os.Getenv("CALDENSEC_OUTPUT_LIMIT")); err == nil && v > 0 {
		output = v
	}
	return context, output
}

// EnsureConfig makes sure an opencode.json exists at path: on a fresh machine
// (no file yet) it creates the parent directory and writes a minimal valid
// config, so the surgical sjson writers have a file to edit. It is a no-op when
// the file already exists.
func EnsureConfig(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking config %q: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	if err := os.WriteFile(path, []byte("{\n  \"$schema\": \"https://opencode.ai/config.json\"\n}\n"), 0o644); err != nil {
		return fmt.Errorf("creating config %q: %w", path, err)
	}
	return nil
}

// AddProvider registers a custom model provider in the opencode.json at
// configPath: it sets provider.<providerID>.options.baseURL and replaces
// provider.<providerID>.models with the given models (each as {"name": ...}).
// Model ids are written as literal map keys, so ids containing "." or "/"
// (e.g. "glm-4.6", "anthropic/claude-x") are handled correctly. Other providers
// (and the rest of the file) are preserved. It backs the file up first, then
// re-reads and validates the result is still JSON, restoring on failure.
//
// The provider's API key is NOT written here — it belongs in OpenCode's auth
// store (see SetOpenCodeCredential), never in opencode.json.
func AddProvider(configPath, providerID, baseURL string, models []config.Model) error {
	original, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config %q: %w", configPath, err)
	}

	backupPath := configPath + providerBackupSuffix
	if err := os.WriteFile(backupPath, original, 0o644); err != nil {
		return fmt.Errorf("writing backup %q: %w", backupPath, err)
	}

	// Build the models map with literal ids as keys (no path parsing). Each
	// entry carries a name plus a token `limit`, which OpenCode needs for a
	// custom provider to count context and run compaction. Per-model limits come
	// from the gateway's /model/info (max_input_tokens / max_output_tokens) when
	// it publishes them; a conservative default is written for any model that
	// carries none (overridable via CALDENSEC_CONTEXT_LIMIT / CALDENSEC_OUTPUT_LIMIT,
	// and freely editable per model in opencode.json).
	ctxLimit, outLimit := defaultModelLimits()
	modelsMap := make(map[string]any, len(models))
	for _, m := range models {
		name := m.Name
		if name == "" {
			name = m.ID
		}
		c, o := m.ContextLimit, m.OutputLimit
		if c <= 0 {
			c = ctxLimit
		}
		if o <= 0 {
			o = outLimit
		}
		modelsMap[m.ID] = map[string]any{
			"name":  name,
			"limit": map[string]int{"context": c, "output": o},
		}
	}

	updated := original
	if updated, err = sjson.SetBytes(updated, "provider."+providerID+".options.baseURL", baseURL); err != nil {
		return fmt.Errorf("setting provider base URL: %w", err)
	}
	if updated, err = sjson.SetBytes(updated, "provider."+providerID+".models", modelsMap); err != nil {
		return fmt.Errorf("setting provider models: %w", err)
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
