package installer

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// engramRepo is the GitHub repo engram release binaries are pulled from.
const engramRepo = "Gentleman-Programming/engram"

// InstallEngramRelease downloads the latest engram release binary for this
// OS/arch from GitHub and installs it to ~/.local/bin/engram, returning its
// path. It needs no Homebrew and works on Linux and macOS — the right fallback
// when ResolveEngramPath finds nothing and brew is absent.
func InstallEngramRelease() (string, error) {
	goos, arch := runtime.GOOS, runtime.GOARCH
	if goos != "linux" && goos != "darwin" {
		return "", fmt.Errorf("unsupported OS %q for automatic engram install", goos)
	}
	if arch != "amd64" && arch != "arm64" {
		return "", fmt.Errorf("unsupported arch %q for automatic engram install", arch)
	}
	client := &http.Client{Timeout: 60 * time.Second}

	tag, err := latestReleaseTag(client, engramRepo)
	if err != nil {
		return "", err
	}
	asset := fmt.Sprintf("engram_%s_%s_%s.tar.gz", strings.TrimPrefix(tag, "v"), goos, arch)
	url := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", engramRepo, tag, asset)

	bin, err := downloadTarMember(client, url, "engram")
	if err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, "engram")
	if err := os.WriteFile(dest, bin, 0o755); err != nil {
		return "", err
	}
	return dest, nil
}

// latestReleaseTag returns the tag_name of a repo's latest GitHub release.
func latestReleaseTag(client *http.Client, repo string) (string, error) {
	resp, err := client.Get("https://api.github.com/repos/" + repo + "/releases/latest")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github releases/latest for %s: HTTP %d", repo, resp.StatusCode)
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return "", err
	}
	if rel.TagName == "" {
		return "", fmt.Errorf("no tag_name in latest release of %s", repo)
	}
	return rel.TagName, nil
}

// downloadTarMember fetches a .tar.gz from url and returns the bytes of the
// named top-level member (matched by base name).
func downloadTarMember(client *http.Client, url, member string) ([]byte, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: HTTP %d", url, resp.StatusCode)
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("member %q not found in %s", member, url)
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && filepath.Base(h.Name) == member {
			return io.ReadAll(io.LimitReader(tr, 256<<20))
		}
	}
}

// ResolveEngramPath locates the engram binary: first on PATH, then in the usual
// install locations (Homebrew, /usr/local, ~/.local/bin). It returns the path
// and true on the first hit, or "", false when engram is not installed.
func ResolveEngramPath() (string, bool) {
	if p, err := exec.LookPath("engram"); err == nil {
		return p, true
	}
	candidates := []string{
		"/opt/homebrew/bin/engram",
		"/usr/local/bin/engram",
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".local", "bin", "engram"))
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c, true
		}
	}
	return "", false
}

// ConfigureEngramMCP surgically registers Engram as a local MCP server in the
// opencode.json at configPath, writing:
//
//	"mcp": { "engram": { "command": [engramPath, "mcp", "--tools=agent"], "type": "local" } }
//
// Any other mcp entries (and the rest of the file) are preserved: it delegates to
// ConfigureMCP, which patches the single path, backs the file up and validates.
func ConfigureEngramMCP(configPath, engramPath string) error {
	return ConfigureMCP(configPath, "engram", MCPSpec{
		Type:    "local",
		Command: []string{engramPath, "mcp", "--tools=agent"},
	})
}
