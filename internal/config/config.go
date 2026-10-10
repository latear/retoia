// Package config loads and surgically edits the OpenCode configuration file
// (opencode.json) used by the Retoia agent suite. It never re-marshals the
// whole document: reads use gjson, writes use sjson to patch a single value.
package config

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Agent describes one Retoia-managed agent as found under `agent.<name>`.
type Agent struct {
	Name         string // agent key, e.g. "cs-recon"
	Mode         string // "primary" or "subagent"
	Description  string
	CurrentModel string // model id with the provider prefix stripped
}

// Model describes one entry under `provider.<id>.models`.
type Model struct {
	ID   string // model key, e.g. "model-a"
	Name string // display name, e.g. "Model A"

	// Provider is the OpenCode provider this model belongs to (e.g. "retoia"
	// or a custom "zai"); assignment writes "<Provider>/<ID>". CategoryLabel, when
	// set, overrides the id-prefix category so custom providers group under their
	// own name. Both are empty for a plain gateway model (Provider defaults to the
	// managed provider at the call site, category comes from the id prefix).
	Provider      string
	CategoryLabel string

	// ContextLimit and OutputLimit are the model's token limits written as
	// `limit.context` / `limit.output`. OpenCode needs them for a custom provider
	// to count context and run compaction. Zero means "unknown"; the writer fills
	// in a default so OpenCode still works.
	ContextLimit int
	OutputLimit  int
}

// Config holds the raw JSON bytes of a loaded opencode.json plus the
// resolved path and provider id it was loaded with.
type Config struct {
	Path       string
	ProviderID string
	raw        []byte
}

// Load reads the config file at path and validates it is well-formed JSON.
// It does not mutate anything; callers use ManagedAgents/AvailableModels to
// inspect it and SetAgentModel (a free function) to write changes.
func Load(path, providerID string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %q: %w", path, err)
	}
	if !gjson.ValidBytes(data) {
		return nil, fmt.Errorf("config %q does not contain valid JSON", path)
	}
	if providerID == "" {
		return nil, fmt.Errorf("provider id must not be empty")
	}
	return &Config{Path: path, ProviderID: providerID, raw: data}, nil
}

// isRetoiaAgentName reports whether an agent name belongs to the Retoia suite by
// its name prefix — "Retoia" for the primaries (and legacy installs) and "cs-"
// for the subagents — so those agents stay managed even when pointed at a
// non-primary provider.
func isRetoiaAgentName(name string) bool {
	return strings.HasPrefix(name, "Retoia") || strings.HasPrefix(name, "retoia") || strings.HasPrefix(name, "cs-")
}

// ManagedAgents returns every agent under `.agent` whose `.model` string
// starts with "<providerID>/", sorted by name. Agents with a missing, null,
// or differently-prefixed model are excluded.
func (c *Config) ManagedAgents() []Agent {
	prefix := c.ProviderID + "/"
	var agents []Agent

	gjson.GetBytes(c.raw, "agent").ForEach(func(key, value gjson.Result) bool {
		modelResult := value.Get("model")
		if modelResult.Type != gjson.String {
			return true // missing/null model: not managed here, keep iterating
		}
		model := modelResult.String()
		name := key.String()
		// Managed = bound to the primary provider, OR a Retoia-suite agent
		// (name prefix) regardless of provider, so an agent stays managed after
		// being pointed at a custom provider like "zai/...".
		if !strings.HasPrefix(model, prefix) && !isRetoiaAgentName(name) {
			return true
		}
		// CurrentModel drops the provider prefix (everything up to the first "/").
		current := model
		if i := strings.IndexByte(model, '/'); i >= 0 {
			current = model[i+1:]
		}
		agents = append(agents, Agent{
			Name:         name,
			Mode:         value.Get("mode").String(),
			Description:  value.Get("description").String(),
			CurrentModel: current,
		})
		return true
	})

	sort.Slice(agents, func(i, j int) bool { return agents[i].Name < agents[j].Name })
	return agents
}

// AvailableModels returns every model key under `.provider.<providerID>.models`
// with its display name, sorted by id.
func (c *Config) AvailableModels() []Model {
	var models []Model

	path := fmt.Sprintf("provider.%s.models", c.ProviderID)
	gjson.GetBytes(c.raw, path).ForEach(func(key, value gjson.Result) bool {
		models = append(models, Model{
			ID:   key.String(),
			Name: value.Get("name").String(),
		})
		return true
	})

	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models
}

// ProviderBaseURL returns provider.<providerID>.options.baseURL, or "" when it
// is not set. This is the gateway URL OpenCode itself uses to route, written at
// install time; the models command reads it back when no flag or env override
// is given.
func (c *Config) ProviderBaseURL() string {
	path := fmt.Sprintf("provider.%s.options.baseURL", c.ProviderID)
	return gjson.GetBytes(c.raw, path).String()
}

// AgentNames returns every agent key under `.agent`, in file order. Used by
// uninstall to find all Retoia-suite agents (including legacy names) to remove.
func (c *Config) AgentNames() []string {
	var names []string
	gjson.GetBytes(c.raw, "agent").ForEach(func(key, _ gjson.Result) bool {
		names = append(names, key.String())
		return true
	})
	return names
}

// ProviderModels returns the models under `provider.<providerID>.models`, each
// tagged with that provider id (so assignment can write "<provider>/<id>").
// Order matches the file; the caller sorts/groups.
func (c *Config) ProviderModels(providerID string) []Model {
	var models []Model
	path := fmt.Sprintf("provider.%s.models", providerID)
	gjson.GetBytes(c.raw, path).ForEach(func(key, value gjson.Result) bool {
		models = append(models, Model{
			ID:       key.String(),
			Name:     value.Get("name").String(),
			Provider: providerID,
		})
		return true
	})
	return models
}

// ProviderIDs returns the ids of every `provider.<id>` that has a models block,
// sorted. These are the providers whose models the picker can offer.
func (c *Config) ProviderIDs() []string {
	var ids []string
	gjson.GetBytes(c.raw, "provider").ForEach(func(key, value gjson.Result) bool {
		if value.Get("models").Exists() {
			ids = append(ids, key.String())
		}
		return true
	})
	sort.Strings(ids)
	return ids
}

// BackupSuffix is appended to the config path to name the pre-write backup.
const BackupSuffix = ".bak-retoia-models"

// SetAgentModel backs up the config file at path, then surgically sets
// `agent.<agentName>.model` to "<providerID>/<modelID>" using sjson, and
// writes the result back to path. After writing it re-reads the file and
// validates the bytes still parse as JSON; if they do not, it restores the
// original content from the backup and returns an error.
func SetAgentModel(path, providerID, agentName, modelID string) error {
	original, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading config %q: %w", path, err)
	}

	backupPath := path + BackupSuffix
	if err := os.WriteFile(backupPath, original, 0o644); err != nil {
		return fmt.Errorf("writing backup %q: %w", backupPath, err)
	}

	newModel := providerID + "/" + modelID
	sjsonPath := fmt.Sprintf("agent.%s.model", agentName)
	updated, err := sjson.SetBytes(original, sjsonPath, newModel)
	if err != nil {
		return fmt.Errorf("setting model for agent %q: %w", agentName, err)
	}

	if err := os.WriteFile(path, updated, 0o644); err != nil {
		return fmt.Errorf("writing config %q: %w", path, err)
	}

	// Verify the bytes actually landed on disk still parse as JSON. If not,
	// restore the pre-write content from the backup we just created.
	written, err := os.ReadFile(path)
	if err != nil || !gjson.ValidBytes(written) {
		if restoreErr := os.WriteFile(path, original, 0o644); restoreErr != nil {
			return fmt.Errorf("written config failed validation and restore failed: %w", restoreErr)
		}
		return fmt.Errorf("written config %q failed JSON validation; restored original from %q", path, backupPath)
	}

	return nil
}

// SetAgent backs up the config file at path, then surgically writes
// `agent.<agentName>.mode`, `.description` and `.model` (= "<providerID>/<modelID>")
// in one transaction, preserving the rest of the file. It re-reads the result and
// validates it still parses as JSON; on failure it restores the original content
// and returns an error.
func SetAgent(path, agentName, mode, description, providerID, modelID string) error {
	original, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading config %q: %w", path, err)
	}

	if !gjson.ValidBytes(original) {
		return fmt.Errorf("config %q does not contain valid JSON; leaving it untouched", path)
	}

	backupPath := path + BackupSuffix
	if err := os.WriteFile(backupPath, original, 0o644); err != nil {
		return fmt.Errorf("writing backup %q: %w", backupPath, err)
	}

	updated := original
	fields := []struct{ key, value string }{
		{"mode", mode},
		{"description", description},
		{"model", providerID + "/" + modelID},
	}
	for _, f := range fields {
		updated, err = sjson.SetBytes(updated, fmt.Sprintf("agent.%s.%s", agentName, f.key), f.value)
		if err != nil {
			return fmt.Errorf("setting %s for agent %q: %w", f.key, agentName, err)
		}
	}

	if err := os.WriteFile(path, updated, 0o644); err != nil {
		return fmt.Errorf("writing config %q: %w", path, err)
	}

	written, err := os.ReadFile(path)
	if err != nil || !gjson.ValidBytes(written) {
		if restoreErr := os.WriteFile(path, original, 0o644); restoreErr != nil {
			return fmt.Errorf("written config failed validation and restore failed: %w", restoreErr)
		}
		return fmt.Errorf("written config %q failed JSON validation; restored original from %q", path, backupPath)
	}
	return nil
}
