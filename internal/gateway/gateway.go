// Package gateway discovers available models from the CaldenSec gateway
// (a gateway exposing an OpenAI-compatible `GET /models` endpoint)
// at runtime. This mirrors the `calden-agents.sh` script's
// `curl "$BASE/models" | jq -r '.data[].id' | sort` behavior.
package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ficopix/caldensec/internal/config"
	"github.com/tidwall/gjson"
)

// requestTimeout bounds how long a single gateway discovery call may take.
const requestTimeout = 10 * time.Second

// maxResponseBytes caps how much of the gateway response body is read, so a
// hostile or misbehaving endpoint cannot exhaust memory. A models list this
// large is not plausible for this use case.
const maxResponseBytes = 1 << 20 // 1 MiB

// ErrInvalidKey indicates the gateway explicitly rejected the server key
// (HTTP 401 or 403). Callers can distinguish this from connectivity issues
// with errors.Is(err, ErrInvalidKey).
var ErrInvalidKey = errors.New("gateway rejected the server key")

// ErrGatewayUnreachable indicates the gateway could not be reached or its
// response could not be parsed as a valid outcome: a network error, a
// timeout, or any non-2xx, non-auth response. Callers can distinguish this
// from an invalid key with errors.Is(err, ErrGatewayUnreachable).
var ErrGatewayUnreachable = errors.New("gateway unreachable")

// FetchModels queries "<base>/models" on the CaldenSec gateway and returns
// every model id found under the OpenAI-compatible `{"data":[{"id":...}]}`
// response shape, sorted ascending by id. Each returned config.Model has its
// Name set to its id (the endpoint carries no separate display name).
//
// If apiKey is non-empty, it is sent as a Bearer token in the Authorization
// header. A trailing slash on base is tolerated. FetchModels returns an
// error on a non-2xx response, a malformed response body, or a response that
// yields zero model ids.
func FetchModels(base, apiKey string) ([]config.Model, error) {
	url := strings.TrimSuffix(base, "/") + "/models"

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %q: %w", url, err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting %q: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("reading response body from %q: %w", url, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gateway %q returned status %d", url, resp.StatusCode)
	}

	if !gjson.ValidBytes(body) {
		return nil, fmt.Errorf("gateway %q returned a malformed JSON response", url)
	}

	ids := parseDataIDs(body)
	if len(ids) == 0 {
		return nil, fmt.Errorf("gateway %q returned no models", url)
	}

	return applyModelLimits(idsToModels(ids), fetchModelLimits(base, apiKey)), nil
}

// ValidateKey checks a server key against the gateway and returns the set of
// models it is authorized to use. It first tries the OpenAI-compatible
// "<base>/models" endpoint with the key as a Bearer token:
//
//   - HTTP 200: the response body is parsed the same way as FetchModels
//     (data[].id, deduped and sorted). An empty list is a valid outcome — a
//     key may legitimately be authorized for zero models — and is returned
//     as an empty, non-error slice.
//   - HTTP 401 or 403: the key is rejected; returns ErrInvalidKey.
//   - HTTP 404 or 405: the endpoint is not present on this gateway; falls
//     back to "<base>/key/info" with the same Bearer token, deriving the
//     authorized model ids from the gateway `key.models` shape (or a
//     top-level `models` array).
//   - Any other non-2xx response, a network error, or a malformed body:
//     returns ErrGatewayUnreachable.
//
// A trailing slash on base is tolerated. The response body is bounded to
// protect against a hostile or misbehaving endpoint.
func ValidateKey(base, serverKey string) ([]config.Model, error) {
	trimmedBase := strings.TrimSuffix(base, "/")
	url := trimmedBase + "/models"

	resp, body, err := doGatewayRequest(url, serverKey)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrGatewayUnreachable, err)
	}

	switch {
	case resp.StatusCode == http.StatusOK:
		if !gjson.ValidBytes(body) {
			return nil, fmt.Errorf("%w: gateway %q returned a malformed JSON response", ErrGatewayUnreachable, url)
		}
		return applyModelLimits(idsToModels(parseDataIDs(body)), fetchModelLimits(trimmedBase, serverKey)), nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("%w: gateway %q returned status %d", ErrInvalidKey, url, resp.StatusCode)
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed:
		return validateKeyViaKeyInfo(trimmedBase, serverKey)
	default:
		return nil, fmt.Errorf("%w: gateway %q returned status %d", ErrGatewayUnreachable, url, resp.StatusCode)
	}
}

// validateKeyViaKeyInfo is the fallback path for gateways that do not expose
// a per-key-filtered "/models" endpoint. It queries "<base>/key/info" and
// derives the authorized model ids from the gateway `{"key":{"models":[...]}}`
// shape, also accepting a top-level `{"models":[...]}` shape.
func validateKeyViaKeyInfo(base, serverKey string) ([]config.Model, error) {
	url := base + "/key/info"

	resp, body, err := doGatewayRequest(url, serverKey)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrGatewayUnreachable, err)
	}

	switch {
	case resp.StatusCode == http.StatusOK:
		if !gjson.ValidBytes(body) {
			return nil, fmt.Errorf("%w: gateway %q returned a malformed JSON response", ErrGatewayUnreachable, url)
		}
		ids := parseModelIDList(body, "key.models")
		if len(ids) == 0 {
			ids = parseModelIDList(body, "models")
		}
		return applyModelLimits(idsToModels(ids), fetchModelLimits(base, serverKey)), nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("%w: gateway %q returned status %d", ErrInvalidKey, url, resp.StatusCode)
	default:
		return nil, fmt.Errorf("%w: gateway %q returned status %d", ErrGatewayUnreachable, url, resp.StatusCode)
	}
}

// doGatewayRequest issues a bounded GET request against url, with serverKey
// (if non-empty) sent as a Bearer token, and returns the response along with
// its size-limited body. The caller owns interpreting the status code.
func doGatewayRequest(url, serverKey string) (*http.Response, []byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("building request for %q: %w", url, err)
	}
	if serverKey != "" {
		req.Header.Set("Authorization", "Bearer "+serverKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("requesting %q: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, nil, fmt.Errorf("reading response body from %q: %w", url, err)
	}

	return resp, body, nil
}

// parseDataIDs extracts deduped, sorted model ids from the OpenAI-compatible
// `{"data":[{"id":...}]}` response shape.
func parseDataIDs(body []byte) []string {
	var ids []string
	seen := make(map[string]struct{})
	gjson.GetBytes(body, "data").ForEach(func(_, value gjson.Result) bool {
		id := value.Get("id").String()
		if id == "" {
			return true
		}
		if _, dup := seen[id]; dup {
			return true // collapse duplicate ids so the picker lists each once
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
		return true
	})
	sort.Strings(ids)
	return ids
}

// parseModelIDList extracts deduped, sorted model ids from a plain JSON
// array of strings at the given gjson path (e.g. "key.models" or "models").
func parseModelIDList(body []byte, path string) []string {
	var ids []string
	seen := make(map[string]struct{})
	gjson.GetBytes(body, path).ForEach(func(_, value gjson.Result) bool {
		id := value.String()
		if id == "" {
			return true
		}
		if _, dup := seen[id]; dup {
			return true
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
		return true
	})
	sort.Strings(ids)
	return ids
}

// idsToModels converts a slice of model ids into []config.Model. Each model's
// Name is set to its id, which is what gets written as the OpenCode display name
// (the gateway's /models endpoint carries no separate display name). A nil or
// empty ids slice yields a non-nil empty slice.
func idsToModels(ids []string) []config.Model {
	models := make([]config.Model, 0, len(ids))
	for _, id := range ids {
		models = append(models, config.Model{ID: id, Name: id})
	}
	return models
}

// modelLimit is a model's token windows as reported by the gateway.
type modelLimit struct{ context, output int }

// fetchModelLimits queries "<base>/model/info" (so on an OpenAI-compatible base
// ending in /v1 it hits /v1/model/info) and returns, per model name, the
// context (max_input_tokens) and output (max_output_tokens) windows the gateway
// reports. It is best effort: any failure, non-2xx, or unpopulated field yields
// no entry, so the caller keeps the conservative default for that model.
func fetchModelLimits(base, apiKey string) map[string]modelLimit {
	url := strings.TrimSuffix(base, "/") + "/model/info"
	resp, body, err := doGatewayRequest(url, apiKey)
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 || !gjson.ValidBytes(body) {
		return nil
	}
	lims := make(map[string]modelLimit)
	gjson.GetBytes(body, "data").ForEach(func(_, v gjson.Result) bool {
		name := v.Get("model_name").String()
		if name == "" {
			return true
		}
		mi := v.Get("model_info")
		l := modelLimit{
			context: int(mi.Get("max_input_tokens").Int()),
			output:  int(mi.Get("max_output_tokens").Int()),
		}
		if l.context > 0 || l.output > 0 {
			lims[name] = l
		}
		return true
	})
	return lims
}

// applyModelLimits fills ContextLimit/OutputLimit on each model whose id has an
// entry in lims (keyed by the gateway's model name). Models without an entry are
// left untouched, so AddProvider writes the default for them.
func applyModelLimits(models []config.Model, lims map[string]modelLimit) []config.Model {
	for i := range models {
		if l, ok := lims[models[i].ID]; ok {
			models[i].ContextLimit = l.context
			models[i].OutputLimit = l.output
		}
	}
	return models
}
