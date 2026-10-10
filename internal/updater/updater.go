// Package updater implements `retoia update`: it fetches a signed manifest
// from a key-gated endpoint derived from the configured gateway, downloads the
// release archive for the running platform, verifies it (SHA-256 plus, when a
// public key is embedded, an Ed25519 signature), and atomically replaces the
// running binary. No GitHub access is required by the user — the gateway key
// that authorizes models also authorizes updates.
package updater

import (
	"archive/tar"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// signingPubKeyB64 is the base64-encoded Ed25519 public key releases are signed
// with. Empty by default: updates then verify the SHA-256 only and print an
// "unsigned" warning. Generate a key pair with `retoia update --keygen`, keep
// the private key as a CI secret that signs each archive, paste the public key
// here, and rebuild to enforce signatures.
const signingPubKeyB64 = ""

// Manifest is the JSON the update endpoint serves at /retoia/manifest.json.
type Manifest struct {
	Version   string              `json:"version"`
	Platforms map[string]Platform `json:"platforms"`
}

// Platform is one build in the manifest, keyed by "<os>_<arch>" (os uses the
// GoReleaser spelling, i.e. darwin is "mac").
type Platform struct {
	URL    string `json:"url"`    // absolute URL of the .tar.gz
	SHA256 string `json:"sha256"` // lowercase hex digest of the .tar.gz
	Sig    string `json:"sig"`    // base64 Ed25519 signature over the .tar.gz (optional)
}

// PlatformKey returns this binary's manifest key, e.g. "mac_arm64".
func PlatformKey() string {
	goos := runtime.GOOS
	if goos == "darwin" {
		goos = "mac"
	}
	return goos + "_" + runtime.GOARCH
}

// BaseFromProvider derives the update host from a provider baseURL such as
// "https://gw.example/v1" -> "https://gw.example". $RETOIA_UPDATE_BASE wins
// when set, so the endpoint can be hosted apart from the model gateway.
func BaseFromProvider(providerBaseURL string) (string, error) {
	if v := strings.TrimRight(os.Getenv("RETOIA_UPDATE_BASE"), "/"); v != "" {
		return v, nil
	}
	if providerBaseURL == "" {
		return "", fmt.Errorf("no gateway baseURL in opencode.json and $RETOIA_UPDATE_BASE unset; cannot locate the update endpoint")
	}
	u, err := url.Parse(providerBaseURL)
	if err != nil {
		return "", fmt.Errorf("parsing gateway baseURL %q: %w", providerBaseURL, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("gateway baseURL %q has no scheme/host", providerBaseURL)
	}
	return u.Scheme + "://" + u.Host, nil
}

// Options carries everything Run needs; the caller resolves the key and URL.
type Options struct {
	Base           string // update host, e.g. https://gw.example
	Key            string // gateway key, sent as Bearer
	CurrentVersion string // running binary version ("dev" for local builds)
	Client         *http.Client
}

// Run performs the update, writing progress to out. It returns nil both when it
// updated and when the binary was already current (it says which).
func Run(out io.Writer, opt Options) error {
	if opt.Key == "" {
		return fmt.Errorf("no gateway key found (keychain, opencode auth.json, or $RETOIA_API_KEY); run `retoia install` or set the key first")
	}
	client := opt.Client
	if client == nil {
		client = &http.Client{Timeout: 120 * time.Second}
	}

	m, err := fetchManifest(client, opt.Base, opt.Key)
	if err != nil {
		return err
	}
	plat, ok := m.Platforms[PlatformKey()]
	if !ok {
		return fmt.Errorf("the update manifest has no build for %s", PlatformKey())
	}
	if m.Version != "" && sameVersion(m.Version, opt.CurrentVersion) {
		fmt.Fprintf(out, "Already on the latest version (%s).\n", m.Version)
		return nil
	}

	fmt.Fprintf(out, "Updating %s -> %s ...\n", displayVersion(opt.CurrentVersion), displayVersion(m.Version))
	archive, err := download(client, plat.URL, opt.Key)
	if err != nil {
		return err
	}
	if err := verify(archive, plat); err != nil {
		return err
	}
	if signingPubKeyB64 == "" {
		fmt.Fprintln(out, "  warning: updates are UNSIGNED (no public key embedded); verified by SHA-256 only.")
	}

	bin, err := extractBinary(archive)
	if err != nil {
		return err
	}
	if err := replaceSelf(bin); err != nil {
		return err
	}
	fmt.Fprintf(out, "Done. retoia is now %s.\n", displayVersion(m.Version))
	return nil
}

// CheckLatest fetches the manifest and reports the latest version and whether it
// is newer than currentVersion. Best effort: any failure (no base/key, offline,
// bad response) returns ("", false), so callers can use it as a silent notice.
func CheckLatest(base, key, currentVersion string) (string, bool) {
	if base == "" || key == "" {
		return "", false
	}
	client := &http.Client{Timeout: 10 * time.Second}
	m, err := fetchManifest(client, base, key)
	if err != nil || m.Version == "" {
		return "", false
	}
	if sameVersion(m.Version, currentVersion) {
		return "", false
	}
	return m.Version, true
}

func fetchManifest(client *http.Client, base, key string) (*Manifest, error) {
	body, err := get(client, base+"/retoia/manifest.json", key)
	if err != nil {
		return nil, fmt.Errorf("fetching update manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("parsing update manifest: %w", err)
	}
	if len(m.Platforms) == 0 {
		return nil, fmt.Errorf("update manifest has no platforms")
	}
	return &m, nil
}

func download(client *http.Client, assetURL, key string) ([]byte, error) {
	b, err := get(client, assetURL, key)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", assetURL, err)
	}
	return b, nil
}

// get issues an authenticated GET and returns the body for a 2xx response.
func get(client *http.Client, u, key string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 128<<20)) // 128 MiB cap
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("the gateway rejected the key (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return body, nil
}

// verify checks the SHA-256 and, when a public key is embedded, the signature.
func verify(archive []byte, plat Platform) error {
	if plat.SHA256 == "" {
		return fmt.Errorf("manifest is missing the SHA-256 for this platform")
	}
	sum := sha256.Sum256(archive)
	if got := hex.EncodeToString(sum[:]); !strings.EqualFold(got, plat.SHA256) {
		return fmt.Errorf("SHA-256 mismatch: expected %s, got %s", plat.SHA256, got)
	}
	if signingPubKeyB64 == "" {
		return nil // unsigned mode: checksum only (caller warns)
	}
	pub, err := base64.StdEncoding.DecodeString(signingPubKeyB64)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("embedded signing public key is invalid")
	}
	if plat.Sig == "" {
		return fmt.Errorf("a signing key is embedded but the manifest has no signature for this platform")
	}
	sig, err := base64.StdEncoding.DecodeString(plat.Sig)
	if err != nil {
		return fmt.Errorf("decoding signature: %w", err)
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), archive, sig) {
		return fmt.Errorf("signature verification FAILED — refusing to install")
	}
	return nil
}

// extractBinary pulls the retoia executable out of the GoReleaser archive,
// which wraps it as "retoia/retoia".
func extractBinary(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(strings.NewReader(string(archive)))
	if err != nil {
		return nil, fmt.Errorf("opening archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("no retoia binary found in the archive")
		}
		if err != nil {
			return nil, fmt.Errorf("reading archive: %w", err)
		}
		if h.Typeflag == tar.TypeReg && filepath.Base(h.Name) == "retoia" {
			return io.ReadAll(tr)
		}
	}
}

// replaceSelf atomically replaces the running executable with newBinary. It
// writes a temp file in the same directory (so the rename stays on one
// filesystem) and renames it over the current path — which succeeds on Unix even
// while the old binary is executing.
func replaceSelf(newBinary []byte) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating the running binary: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	return writeAndRename(self, newBinary)
}

// writeAndRename writes data to a temp file beside target, makes it executable,
// and renames it over target — atomic on a single filesystem, and valid on Unix
// even while target is the running binary.
func writeAndRename(target string, data []byte) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".retoia-update-*")
	if err != nil {
		return fmt.Errorf("the install dir %s is not writable (%w); re-run the install, or use sudo", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeds
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("writing the new binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return fmt.Errorf("replacing %s (%w); re-run the install, or use sudo", target, err)
	}
	return nil
}

func displayVersion(v string) string {
	if v == "" {
		return "unknown"
	}
	return v
}

// sameVersion compares versions ignoring a leading "v", so a "v0.2.6" manifest
// matches a binary built with version "0.2.6" (or vice versa).
func sameVersion(a, b string) bool {
	return strings.TrimPrefix(a, "v") == strings.TrimPrefix(b, "v")
}
