package mcpservers

import (
	"strings"
	"testing"
)

func TestCatalogSortedAndValid(t *testing.T) {
	cat := Catalog()
	if len(cat) == 0 {
		t.Fatal("catalog is empty")
	}
	for i, s := range cat {
		if s.ID == "" || s.Name == "" || s.Description == "" || s.RepoURL == "" {
			t.Errorf("catalog[%d] is missing required fields: %+v", i, s)
		}
		// A wireable entry carries a command (local) OR a url (remote); a manual
		// one carries neither.
		if s.Manual() && (len(s.Command) != 0 || s.URL != "") {
			t.Errorf("%s: Manual() true but has a command/url", s.ID)
		}
		if !s.Manual() && len(s.Command) == 0 && s.URL == "" {
			t.Errorf("%s: not manual but has neither command nor url", s.ID)
		}
		if i > 0 && cat[i-1].ID >= s.ID {
			t.Errorf("catalog not sorted at %d: %q >= %q", i, cat[i-1].ID, s.ID)
		}
	}
}

func TestFind(t *testing.T) {
	// context7 is wireable via a remote URL (no local command to bake, and no
	// setup steps to run).
	s, ok := Find("context7")
	if !ok {
		t.Fatal("context7 not found in catalog")
	}
	if !s.Wireable() || s.URL == "" {
		t.Errorf("context7 should be wireable with a URL, got %q", s.URL)
	}
	if s.NeedsSetup() {
		t.Error("context7 should not report NeedsSetup (no setup steps)")
	}

	if _, ok := Find("does-not-exist"); ok {
		t.Error("Find returned ok for a missing id")
	}
}

// TestEnvNamesOnly enforces the catalog rule: Env lists variable NAMES only
// (the add path writes them as {env:NAME}); no key value is ever baked in.
func TestEnvNamesOnly(t *testing.T) {
	for _, s := range Catalog() {
		for _, k := range s.Env {
			if k == "" || strings.ToUpper(k) != k || strings.ContainsAny(k, "=:{} ") {
				t.Errorf("%s: env %q must be a bare UPPER_CASE variable name", s.ID, k)
			}
		}
	}
}

func TestCloudflareAndGitHubEntries(t *testing.T) {
	cf, ok := Lookup("cloudflare")
	if !ok {
		t.Fatal("cloudflare not in catalog")
	}
	if !cf.Wireable() || cf.URL != "https://docs.mcp.cloudflare.com/mcp" || len(cf.Command) != 0 || len(cf.Env) != 0 {
		t.Errorf("cloudflare should be a keyless remote at /mcp, got %+v", cf)
	}

	gh, ok := Lookup("github")
	if !ok {
		t.Fatal("github not in catalog")
	}
	if !gh.Wireable() || gh.URL != "" || len(gh.Command) == 0 || gh.Command[0] != "docker" {
		t.Errorf("github should be a local docker command, got %+v", gh)
	}
	if len(gh.Env) != 1 || gh.Env[0] != "GITHUB_PERSONAL_ACCESS_TOKEN" {
		t.Errorf("github env = %v, want [GITHUB_PERSONAL_ACCESS_TOKEN]", gh.Env)
	}
	// The container must receive the token by name (-e NAME, no baked value).
	joined := strings.Join(gh.Command, " ")
	if !strings.Contains(joined, "-e GITHUB_PERSONAL_ACCESS_TOKEN ") || strings.Contains(joined, "GITHUB_PERSONAL_ACCESS_TOKEN=") {
		t.Errorf("github command must pass the token by name: %q", joined)
	}
	if len(gh.Prereqs) != 1 || gh.Prereqs[0] != "docker" {
		t.Errorf("github prereqs = %v, want [docker]", gh.Prereqs)
	}
	for _, s := range []Server{cf, gh} {
		if s.Name == "" || s.Description == "" || !strings.HasPrefix(s.RepoURL, "https://") {
			t.Errorf("%s: malformed fields %+v", s.ID, s)
		}
		if s.URL != "" && !strings.HasPrefix(s.URL, "https://") {
			t.Errorf("%s: remote URL must be https, got %q", s.ID, s.URL)
		}
	}
}

// TestLookup covers the id lookup used by the install TUI to wire context7.
func TestLookup(t *testing.T) {
	s, ok := Lookup("context7")
	if !ok {
		t.Fatal("context7 not found in the catalog")
	}
	if s.URL == "" {
		t.Errorf("context7 should be wireable (have a URL); got none")
	}
	if _, ok := Lookup("does-not-exist"); ok {
		t.Error("Lookup of an unknown id should return ok=false")
	}
}
