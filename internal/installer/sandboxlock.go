package installer

import (
	"fmt"
	"os"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// SetAutoupdate surgically sets the top-level "autoupdate" key in an
// opencode.json. The sandbox bakes a pinned OpenCode version, but OpenCode
// self-updates to the latest release on startup by default, drifting off the
// pin; setting "autoupdate": false makes the baked version hold (paired with the
// OPENCODE_DISABLE_AUTOUPDATE env var in the compose file). Operates on the
// freshly generated staging config, so it reads, sets, revalidates and writes
// without a backup.
func SetAutoupdate(configPath string, enabled bool) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("reading %q: %w", configPath, err)
	}
	if !gjson.ValidBytes(data) {
		return fmt.Errorf("%q is not valid JSON", configPath)
	}
	updated, err := sjson.SetBytes(data, "autoupdate", enabled)
	if err != nil {
		return fmt.Errorf("setting autoupdate: %w", err)
	}
	if !gjson.ValidBytes(updated) {
		return fmt.Errorf("writing autoupdate would invalidate %q", configPath)
	}
	if err := os.WriteFile(configPath, updated, 0o644); err != nil {
		return fmt.Errorf("writing %q: %w", configPath, err)
	}
	return nil
}
