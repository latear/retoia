package gateway

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ficopix/caldensec/internal/config"
)

// TestFetchModels_HappyPath verifies that a well-formed gateway response is
// parsed into a sorted, de-duplicated slice of config.Model, each with Name
// set to "live". Duplicate ids from the gateway collapse to one entry so the
// picker never lists the same model twice.
func TestFetchModels_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/model/info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Path != "/models" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"model-a"},{"id":"aegis"},{"id":"aegis"}]}`))
	}))
	defer server.Close()

	models, err := FetchModels(server.URL, "")
	if err != nil {
		t.Fatalf("FetchModels returned error: %v", err)
	}

	want := []config.Model{
		{ID: "aegis", Name: "aegis"},
		{ID: "model-a", Name: "model-a"},
	}
	if len(models) != len(want) {
		t.Fatalf("got %d models, want %d (duplicates should collapse): %+v", len(models), len(want), models)
	}
	for i := range want {
		if models[i] != want[i] {
			t.Errorf("model[%d] = %+v, want %+v", i, models[i], want[i])
		}
	}
}

// TestFetchModels_ErrorBodyBounded verifies that a large error-response body is
// truncated in the returned error, so a gateway/proxy error page cannot flood
// the caller's stderr warning line.
func TestFetchModels_ErrorBodyBounded(t *testing.T) {
	huge := make([]byte, 50_000)
	for i := range huge {
		huge[i] = 'x'
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/model/info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write(huge)
	}))
	defer server.Close()

	_, err := FetchModels(server.URL, "")
	if err == nil {
		t.Fatal("expected an error for a 502 response, got nil")
	}
	if len(err.Error()) > 1024 {
		t.Errorf("error message is %d bytes; want it bounded (<=1024) so a large body cannot flood stderr", len(err.Error()))
	}
}

// TestFetchModels_TrailingSlashBase verifies that a base URL with a trailing
// slash is joined correctly with "/models" (no double slash).
func TestFetchModels_TrailingSlashBase(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/model/info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Path != "/models" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"solo"}]}`))
	}))
	defer server.Close()

	models, err := FetchModels(server.URL+"/", "")
	if err != nil {
		t.Fatalf("FetchModels returned error: %v", err)
	}
	if len(models) != 1 || models[0].ID != "solo" {
		t.Fatalf("got %+v, want a single model %q", models, "solo")
	}
}

// TestFetchModels_AuthHeader verifies the Authorization header is sent when
// an API key is provided, and absent when it is empty.
func TestFetchModels_AuthHeader(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/model/info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
	}))
	defer server.Close()

	if _, err := FetchModels(server.URL, "secret-key"); err != nil {
		t.Fatalf("FetchModels returned error: %v", err)
	}
	if gotAuth != "Bearer secret-key" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer secret-key")
	}

	if _, err := FetchModels(server.URL, ""); err != nil {
		t.Fatalf("FetchModels returned error: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("Authorization header = %q, want empty when no key is set", gotAuth)
	}
}

// TestFetchModels_NonOKStatus verifies a non-2xx response is surfaced as an
// error mentioning the status code.
func TestFetchModels_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/model/info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer server.Close()

	_, err := FetchModels(server.URL, "")
	if err == nil {
		t.Fatal("expected an error for a 401 response, got nil")
	}
}

// TestFetchModels_EmptyData verifies an empty data array is treated as an
// error rather than an empty, silently-accepted model list.
func TestFetchModels_EmptyData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/model/info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()

	_, err := FetchModels(server.URL, "")
	if err == nil {
		t.Fatal("expected an error for an empty data array, got nil")
	}
}

// TestFetchModels_MalformedJSON verifies invalid JSON bodies are rejected.
func TestFetchModels_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/model/info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`not json`))
	}))
	defer server.Close()

	_, err := FetchModels(server.URL, "")
	if err == nil {
		t.Fatal("expected an error for a malformed JSON body, got nil")
	}
}

// TestValidateKey_HappyPath verifies a 200 response from /models is parsed
// into a sorted, de-duplicated slice of authorized config.Model.
func TestValidateKey_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/model/info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Path != "/models" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"model-a"},{"id":"auto"}]}`))
	}))
	defer server.Close()

	models, err := ValidateKey(server.URL, "server-key")
	if err != nil {
		t.Fatalf("ValidateKey returned error: %v", err)
	}

	want := []config.Model{
		{ID: "auto", Name: "auto"},
		{ID: "model-a", Name: "model-a"},
	}
	if len(models) != len(want) {
		t.Fatalf("got %d models, want %d: %+v", len(models), len(want), models)
	}
	for i := range want {
		if models[i] != want[i] {
			t.Errorf("model[%d] = %+v, want %+v", i, models[i], want[i])
		}
	}
}

// TestValidateKey_EmptyAuthorizedList verifies an empty data array from
// /models is treated as a valid key with no authorized models, not an error
// — unlike FetchModels, an empty list here is a legitimate outcome.
func TestValidateKey_EmptyAuthorizedList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/model/info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()

	models, err := ValidateKey(server.URL, "server-key")
	if err != nil {
		t.Fatalf("ValidateKey returned error: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("got %+v, want an empty (non-nil-checked) slice for a key with no authorized models", models)
	}
}

// TestValidateKey_Unauthorized401 verifies a 401 from /models is reported as
// ErrInvalidKey.
func TestValidateKey_Unauthorized401(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/model/info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer server.Close()

	models, err := ValidateKey(server.URL, "bad-key")
	if !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("ValidateKey error = %v, want errors.Is(err, ErrInvalidKey)", err)
	}
	if models != nil {
		t.Errorf("got models %+v, want nil on an invalid key", models)
	}
}

// TestValidateKey_Forbidden403 verifies a 403 from /models is reported as
// ErrInvalidKey.
func TestValidateKey_Forbidden403(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/model/info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"forbidden"}`))
	}))
	defer server.Close()

	_, err := ValidateKey(server.URL, "bad-key")
	if !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("ValidateKey error = %v, want errors.Is(err, ErrInvalidKey)", err)
	}
}

// TestValidateKey_AuthHeader verifies the request to /models carries the
// server key as a Bearer token.
func TestValidateKey_AuthHeader(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/model/info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":[{"id":"m1"}]}`))
	}))
	defer server.Close()

	if _, err := ValidateKey(server.URL, "secret-key"); err != nil {
		t.Fatalf("ValidateKey returned error: %v", err)
	}
	if gotAuth != "Bearer secret-key" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer secret-key")
	}
}

// TestValidateKey_Unreachable verifies that a closed (unreachable) server
// yields ErrGatewayUnreachable, distinct from ErrInvalidKey.
func TestValidateKey_Unreachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/model/info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
	}))
	server.Close() // close before use: connections to server.URL now fail

	models, err := ValidateKey(server.URL, "any-key")
	if !errors.Is(err, ErrGatewayUnreachable) {
		t.Fatalf("ValidateKey error = %v, want errors.Is(err, ErrGatewayUnreachable)", err)
	}
	if errors.Is(err, ErrInvalidKey) {
		t.Errorf("ValidateKey error = %v, must not also be ErrInvalidKey", err)
	}
	if models != nil {
		t.Errorf("got models %+v, want nil when the gateway is unreachable", models)
	}
}

// TestValidateKey_ServerError500 verifies a 500 response (not a clean models
// response, not an auth failure) is reported as ErrGatewayUnreachable.
func TestValidateKey_ServerError500(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/model/info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`internal error`))
	}))
	defer server.Close()

	_, err := ValidateKey(server.URL, "any-key")
	if !errors.Is(err, ErrGatewayUnreachable) {
		t.Fatalf("ValidateKey error = %v, want errors.Is(err, ErrGatewayUnreachable)", err)
	}
}

// TestValidateKey_FallbackToKeyInfo verifies that when /models is not
// present (404), ValidateKey falls back to /key/info and derives the
// authorized models from the gateway key.models shape.
func TestValidateKey_FallbackToKeyInfo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/model/info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch r.URL.Path {
		case "/models":
			w.WriteHeader(http.StatusNotFound)
		case "/key/info":
			if got := r.Header.Get("Authorization"); got != "Bearer server-key" {
				t.Errorf("Authorization header on /key/info = %q, want %q", got, "Bearer server-key")
			}
			_, _ = w.Write([]byte(`{"key":{"models":["m2","m1"]}}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	models, err := ValidateKey(server.URL, "server-key")
	if err != nil {
		t.Fatalf("ValidateKey returned error: %v", err)
	}
	want := []config.Model{
		{ID: "m1", Name: "m1"},
		{ID: "m2", Name: "m2"},
	}
	if len(models) != len(want) {
		t.Fatalf("got %d models, want %d: %+v", len(models), len(want), models)
	}
	for i := range want {
		if models[i] != want[i] {
			t.Errorf("model[%d] = %+v, want %+v", i, models[i], want[i])
		}
	}
}

// TestValidateKey_FallbackToKeyInfoInvalidKey verifies that a 401 from the
// /key/info fallback is reported as ErrInvalidKey.
func TestValidateKey_FallbackToKeyInfoInvalidKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/model/info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch r.URL.Path {
		case "/models":
			w.WriteHeader(http.StatusNotFound)
		case "/key/info":
			w.WriteHeader(http.StatusUnauthorized)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	_, err := ValidateKey(server.URL, "bad-key")
	if !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("ValidateKey error = %v, want errors.Is(err, ErrInvalidKey)", err)
	}
}

// TestValidateKey_FallbackOn405 verifies a 405 from /models also triggers
// the /key/info fallback (some gateways signal "endpoint not present" this
// way instead of 404).
func TestValidateKey_FallbackOn405(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/model/info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch r.URL.Path {
		case "/models":
			w.WriteHeader(http.StatusMethodNotAllowed)
		case "/key/info":
			_, _ = w.Write([]byte(`{"models":["solo"]}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	models, err := ValidateKey(server.URL, "server-key")
	if err != nil {
		t.Fatalf("ValidateKey returned error: %v", err)
	}
	if len(models) != 1 || models[0].ID != "solo" {
		t.Fatalf("got %+v, want a single model %q", models, "solo")
	}
}

// TestValidateKey_TrailingSlashBase verifies a trailing slash on base is
// tolerated for both /models and the /key/info fallback.
func TestValidateKey_TrailingSlashBase(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/model/info" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Path != "/models" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"solo"}]}`))
	}))
	defer server.Close()

	models, err := ValidateKey(server.URL+"/", "server-key")
	if err != nil {
		t.Fatalf("ValidateKey returned error: %v", err)
	}
	if len(models) != 1 || models[0].ID != "solo" {
		t.Fatalf("got %+v, want a single model %q", models, "solo")
	}
}

// TestFetchModels_PopulatesLimitsFromModelInfo verifies that per-model context
// windows from /model/info flow onto the returned models, while a model with
// null windows is left at zero (AddProvider then writes the default for it).
func TestFetchModels_PopulatesLimitsFromModelInfo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			w.Write([]byte(`{"data":[{"id":"calden-sk-orca"},{"id":"nolimit"}]}`))
		case "/model/info":
			w.Write([]byte(`{"data":[` +
				`{"model_name":"calden-sk-orca","model_info":{"max_input_tokens":262144,"max_output_tokens":8192}},` +
				`{"model_name":"nolimit","model_info":{"max_input_tokens":null,"max_output_tokens":null}}` +
				`]}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	models, err := FetchModels(server.URL, "k")
	if err != nil {
		t.Fatalf("FetchModels: %v", err)
	}
	by := map[string]config.Model{}
	for _, m := range models {
		by[m.ID] = m
	}
	if m := by["calden-sk-orca"]; m.ContextLimit != 262144 || m.OutputLimit != 8192 {
		t.Errorf("orca limits = %d/%d, want 262144/8192", m.ContextLimit, m.OutputLimit)
	}
	if m := by["nolimit"]; m.ContextLimit != 0 || m.OutputLimit != 0 {
		t.Errorf("nolimit limits = %d/%d, want 0/0 (default applied later)", m.ContextLimit, m.OutputLimit)
	}
}
