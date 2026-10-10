// Package providers is the registry of the model provider that `retoia models`
// can add as a custom OpenCode provider. It exposes an OpenAI-style
// `GET {BaseURL}/models` endpoint behind a Bearer key, so the shared fetch
// (internal/gateway.FetchModels) works against it.
//
// Reto IA ships a single provider: the Reto IA gateway. The base URL is a
// sensible default; the install/add flow lets the user edit the base before
// fetching, so an out-of-date default is recoverable.
package providers

// DefaultGatewayBase is the Reto IA gateway, the OpenAI-compatible endpoint the
// server key validates against and that serves the authorized models.
const DefaultGatewayBase = "https://llm.retoia.app"

// Provider is one known model provider.
type Provider struct {
	ID      string // OpenCode provider id written to opencode.json (safe single token)
	Name    string // display name shown in the picker and as the category label
	BaseURL string // OpenAI-compatible base; models come from BaseURL + "/models"
}

// known lists the built-in provider: the Reto IA gateway. The base URL is
// editable in the add-custom flow, so an out-of-date default is recoverable.
var known = []Provider{
	{ID: "retoia", Name: "Reto IA", BaseURL: DefaultGatewayBase},
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
