package installtui

import (
	"fmt"
	"io"
	"os"

	"github.com/ficopix/caldensec/internal/config"
	"github.com/ficopix/caldensec/internal/gateway"
	"github.com/ficopix/caldensec/internal/keystore"
	"github.com/ficopix/caldensec/internal/onboard"
)

// HeadlessOptions configures a non-interactive install.
type HeadlessOptions struct {
	Engram     bool // install + configure the Engram MCP
	Context7   bool // wire the context7 docs MCP
	TUIPlugins bool // install the OpenCode sidebar plugins (sub-agent monitor + todo)
}

// RunHeadless performs a non-interactive install, reusing the same apply() the
// interactive TUI uses so the two paths never drift. It resolves the server key
// from the environment / key store and validates it against the gateway (best
// effort) to get the authorized models, then applies the provider, credential,
// PATH and (optionally) Engram/Context7/TUI plugins, writing a short summary to
// out. It is the entry point used by `caldensec install --yes` and by the
// sandbox image, where no TTY is available.
func RunHeadless(out io.Writer, configPath, providerID, base, passphrase string, opts HeadlessOptions) error {
	serverKey := onboard.ResolveServerKey(os.Getenv("CALDENSEC_API_KEY"), keystore.Get)
	var models []config.Model
	if serverKey != "" && base != "" {
		if m, err := gateway.ValidateKey(base, serverKey); err == nil {
			models = m
		}
	}
	setup := setupResult{
		installEngram:     opts.Engram,
		installContext7:   opts.Context7,
		installTUIPlugins: opts.TUIPlugins,
		openCodePresent:   openCodeInstalled(),
	}
	key := keyResult{
		models:      models,
		keepCurrent: serverKey != "",
		offline:     serverKey == "" || len(models) == 0,
	}
	res, err := apply(configPath, providerID, base, passphrase, setup, key)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "caldensec: headless install done → %s\n", res.configPath)
	if res.baseURLSet && res.baseURL != "" {
		fmt.Fprintf(out, "  gateway: %s\n", res.baseURL)
	}
	if res.credentialSet {
		fmt.Fprintf(out, "  credential: stored for %s\n", res.credentialProvider)
	}
	if len(res.tuiPluginsAdded) > 0 {
		fmt.Fprintf(out, "  sidebar plugins: added %d → %s\n", len(res.tuiPluginsAdded), res.tuiPluginsPath)
	}
	if res.engramConfigured {
		fmt.Fprintln(out, "  engram: configured")
	}
	if res.context7Configured {
		fmt.Fprintln(out, "  context7: configured")
	}
	if res.noGateway {
		fmt.Fprintln(out, "  gateway: none — extras installed; set the gateway and assign models later")
	}
	return nil
}
