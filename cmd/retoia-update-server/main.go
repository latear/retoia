// Command retoia-update-server is the key-gated middleware that backs
// `retoia update`. It never hands out GitHub access: clients authenticate
// with their gateway key, and the server (holding a GitHub token) builds a
// manifest from the newest release and streams the private assets.
//
// It serves two Bearer-authenticated endpoints:
//
//	GET /retoia/manifest.json  -> {version, platforms{<os>_<arch>:{url,sha256,sig}}}
//	GET /retoia/dl/<asset>     -> the release asset bytes
//
// Auth is validated against LiteLLM (LITELLM_URL) and/or a static allowlist
// (UPDATE_KEYS); the server refuses to start with neither. It holds no signing
// key — signatures, when present, are read from sibling "<asset>.sig" release
// assets produced at release time, so a compromised server cannot forge one.
//
// Dependency-free (standard library only). Configure via environment:
//
//	ADDR         listen address (default ":8099")
//	GITHUB_REPO  owner/repo of the releases (default "latear/retoia")
//	GITHUB_TOKEN token with read access to the private repo (required)
//	PUBLIC_BASE  external base URL for asset links (default "https://"+Host)
//	LITELLM_URL  validate keys via GET {LITELLM_URL}/v1/models (optional)
//	UPDATE_KEYS  comma-separated allowlist of accepted keys (optional)
package main

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

func main() {
	srv, err := newServer()
	if err != nil {
		log.Fatalf("retoia-update-server: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/retoia/manifest.json", srv.handleManifest)
	mux.HandleFunc("/retoia/dl/", srv.handleDownload)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "ok") })

	log.Printf("retoia-update-server listening on %s (repo %s)", srv.addr, srv.repo)
	log.Fatal(http.ListenAndServe(srv.addr, mux))
}

type server struct {
	addr       string
	repo       string
	token      string
	publicBase string
	litellmURL string
	keys       []string
	client     *http.Client

	mu     sync.Mutex
	cached *release
	cachAt time.Time
}

func newServer() (*server, error) {
	s := &server{
		addr:       envOr("ADDR", ":8099"),
		repo:       envOr("GITHUB_REPO", "latear/retoia"),
		token:      os.Getenv("GITHUB_TOKEN"),
		publicBase: strings.TrimRight(os.Getenv("PUBLIC_BASE"), "/"),
		litellmURL: strings.TrimRight(os.Getenv("LITELLM_URL"), "/"),
		client:     &http.Client{Timeout: 60 * time.Second},
	}
	if s.token == "" {
		return nil, fmt.Errorf("GITHUB_TOKEN is required")
	}
	for _, k := range strings.Split(os.Getenv("UPDATE_KEYS"), ",") {
		if k = strings.TrimSpace(k); k != "" {
			s.keys = append(s.keys, k)
		}
	}
	if s.litellmURL == "" && len(s.keys) == 0 {
		return nil, fmt.Errorf("configure auth: set LITELLM_URL and/or UPDATE_KEYS (refusing to run open)")
	}
	return s, nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// --- auth ---

// authorize extracts the Bearer key and validates it against the allowlist
// (constant-time) and/or LiteLLM. Returns false and writes 401 when invalid.
func (s *server) authorize(w http.ResponseWriter, r *http.Request) bool {
	key := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	if key == "" {
		http.Error(w, "missing bearer key", http.StatusUnauthorized)
		return false
	}
	if s.keyAllowed(key) || s.litellmValid(key) {
		return true
	}
	http.Error(w, "unauthorized", http.StatusUnauthorized)
	return false
}

func (s *server) keyAllowed(key string) bool {
	for _, k := range s.keys {
		if subtle.ConstantTimeCompare([]byte(k), []byte(key)) == 1 {
			return true
		}
	}
	return false
}

// litellmValid reports whether key can list models on the gateway (any valid
// virtual key can), i.e. it is a live gateway key.
func (s *server) litellmValid(key string) bool {
	if s.litellmURL == "" {
		return false
	}
	req, err := http.NewRequest(http.MethodGet, s.litellmURL+"/v1/models", nil)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := s.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// --- GitHub release (cached) ---

type release struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"url"` // API asset URL (used with Accept: octet-stream)
	} `json:"assets"`
}

func (s *server) latestRelease() (*release, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached != nil && time.Since(s.cachAt) < 60*time.Second {
		return s.cached, nil
	}
	req, _ := http.NewRequest(http.MethodGet, "https://api.github.com/repos/"+s.repo+"/releases/latest", nil)
	req.Header.Set("Authorization", "token "+s.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return nil, fmt.Errorf("github releases/latest: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	s.cached, s.cachAt = &rel, time.Now()
	return &rel, nil
}

func (rel *release) asset(name string) (string, bool) {
	for _, a := range rel.Assets {
		if a.Name == name {
			return a.URL, true
		}
	}
	return "", false
}

// fetchAsset downloads a release asset's bytes by name via the GitHub API. The
// API redirects to a signed storage URL on a different host; net/http drops the
// Authorization header on that cross-host redirect, so the token never leaks.
func (s *server) fetchAsset(rel *release, name string) ([]byte, error) {
	u, ok := rel.asset(name)
	if !ok {
		return nil, fmt.Errorf("asset %q not found in release %s", name, rel.TagName)
	}
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	req.Header.Set("Authorization", "token "+s.token)
	req.Header.Set("Accept", "application/octet-stream")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: HTTP %d", name, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 256<<20))
}

// --- handlers ---

type manifest struct {
	Version   string              `json:"version"`
	Platforms map[string]platform `json:"platforms"`
}

type platform struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Sig    string `json:"sig,omitempty"`
}

func (s *server) handleManifest(w http.ResponseWriter, r *http.Request) {
	if !s.authorize(w, r) {
		return
	}
	rel, err := s.latestRelease()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	sums, err := s.fetchAsset(rel, "checksums.txt")
	if err != nil {
		http.Error(w, "reading checksums: "+err.Error(), http.StatusBadGateway)
		return
	}
	digests := parseChecksums(string(sums))

	base := s.publicBase
	if base == "" {
		base = "https://" + r.Host
	}
	m := manifest{Version: rel.TagName, Platforms: map[string]platform{}}
	for _, a := range rel.Assets {
		pk, ok := platformKey(a.Name)
		if !ok {
			continue
		}
		p := platform{
			URL:    base + "/retoia/dl/" + a.Name,
			SHA256: digests[a.Name],
		}
		if sig, ok := rel.asset(a.Name + ".sig"); ok && sig != "" {
			if b, err := s.fetchAsset(rel, a.Name+".sig"); err == nil {
				p.Sig = strings.TrimSpace(string(b))
			}
		}
		m.Platforms[pk] = p
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(m)
}

func (s *server) handleDownload(w http.ResponseWriter, r *http.Request) {
	if !s.authorize(w, r) {
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/retoia/dl/")
	if name == "" || strings.ContainsAny(name, "/\\") {
		http.Error(w, "bad asset name", http.StatusBadRequest)
		return
	}
	rel, err := s.latestRelease()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	data, err := s.fetchAsset(rel, name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(data)
}

// platformKey maps a release asset filename such as
// "retoia_mac_arm64.tar.gz" to the manifest key "mac_arm64", or false when
// the name is not a per-platform archive.
func platformKey(assetName string) (string, bool) {
	s := strings.TrimPrefix(assetName, "retoia_")
	if s == assetName {
		return "", false
	}
	s, ok := strings.CutSuffix(s, ".tar.gz")
	if !ok || s == "" || !strings.Contains(s, "_") {
		return "", false
	}
	return s, true
}

// parseChecksums parses GoReleaser's "<hex>  <filename>" lines into a map.
func parseChecksums(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 {
			out[f[1]] = f[0]
		}
	}
	return out
}
