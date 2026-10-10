// Package providers is a small registry of well-known, OpenAI-compatible model
// providers that `caldensec models` can add as custom OpenCode providers. Each
// exposes an OpenAI-style `GET {BaseURL}/models` endpoint behind a Bearer key,
// so the same fetch (internal/gateway.FetchModels) works for all of them.
//
// Base URLs are sensible defaults; the install/add flow lets the user edit the
// base before fetching, so an out-of-date default is recoverable.
package providers

// Provider is one known model provider.
type Provider struct {
	ID      string // OpenCode provider id written to opencode.json (safe single token)
	Name    string // display name shown in the picker and as the category label
	BaseURL string // OpenAI-compatible base; models come from BaseURL + "/models"
}

// known lists the built-in OpenAI-compatible providers, in display order. The
// first four use endpoints confirmed by the maintainer; the rest are common
// defaults (all editable in the add-custom flow).
var known = []Provider{
	{ID: "zai", Name: "z.ai / GLM", BaseURL: "https://api.z.ai/api/coding/paas/v4"},
	{ID: "moonshot", Name: "Kimi (Moonshot)", BaseURL: "https://api.kimi.com/coding/v1"},
	{ID: "orcarouter", Name: "OrcaRouter", BaseURL: "https://api.orcarouter.ai/v1"},
	{ID: "minimax", Name: "MiniMax", BaseURL: "https://api.minimax.io/v1"},
	{ID: "openai", Name: "OpenAI", BaseURL: "https://api.openai.com/v1"},
	{ID: "openrouter", Name: "OpenRouter", BaseURL: "https://openrouter.ai/api/v1"},
	{ID: "deepseek", Name: "DeepSeek", BaseURL: "https://api.deepseek.com/v1"},
	{ID: "xai", Name: "xAI (Grok)", BaseURL: "https://api.x.ai/v1"},
}

// Known returns the built-in providers in display order.
func Known() []Provider {
	out := make([]Provider, len(known))
	copy(out, known)
	return out
}

// Lookup returns the provider with the given id.
func Lookup(id string) (Provider, bool) {
	for _, p := range known {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}
