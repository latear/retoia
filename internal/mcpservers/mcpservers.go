// Package mcpservers holds a small curated catalog of MCP servers, so
// `retoia-cli mcp` can recommend and wire them the same way Engram is wired.
//
// Two kinds of entry exist:
//
//   - Wireable: carries a verified Command (or URL) that `retoia-cli mcp add`
//     writes straight into opencode.json. Some also carry Setup steps (a Docker
//     backend to build, say) that must run first; the install wizard runs those.
//   - Manual: no command can be baked because the real launch depends on a local
//     checkout path, a running GUI, or a paid product. These are listed with
//     their repo and a note, so the catalog still recommends them honestly
//     instead of writing a broken config.
//
// Every Command/Setup below was taken from the project's own documentation.
// Most entries need no credentials; a few (GitHub) need a token the user owns.
// Catalog policy: no key VALUE is ever baked into an entry or written to
// opencode.json -- Env lists only variable NAMES, which `mcp add` turns into
// {env:NAME} references resolved from the user's environment at launch.
//
// Engram and CodeGraph are intentionally not catalog entries here: Engram is
// wired separately via internal/installer/engram.go (it needs a release
// download step, not just a static command), and there is no CodeGraph MCP
// integration in this CLI today (see README's "What remains" section).
package mcpservers

import "sort"

// Server is one curated MCP-server recommendation.
type Server struct {
	ID          string // stable catalog id, also the default mcp.<name> key
	Name        string // human-facing name
	Description string // one line: what the server gives the agent
	Category    string // docs, etc.
	RepoURL     string // upstream project

	// Wiring into opencode.json. A local server carries Command; a remote one
	// carries URL. When both are empty the server is Manual (see the note).
	Command []string
	URL     string
	Env     []string // environment variables the server needs (names only, never values; written as {env:NAME})

	// Setup lists the verified shell steps the user must run before the wired
	// command works (e.g. building a Docker backend). The install wizard runs
	// these; empty means nothing to set up.
	Setup     []string
	SetupNote string   // one-line human summary of the setup / prerequisites
	Prereqs   []string // binaries the setup steps need on PATH (e.g. git, docker)
}

// Wireable reports whether `retoia-cli mcp add` can write this server straight
// into opencode.json (it has a command or URL to bake).
func (s Server) Wireable() bool { return len(s.Command) > 0 || s.URL != "" }

// Manual reports the opposite: the server needs hands-on setup and cannot be
// auto-wired (its launch depends on a local path, a GUI, or a paid product).
func (s Server) Manual() bool { return !s.Wireable() }

// NeedsSetup reports whether the server has setup steps to run before its wired
// command works.
func (s Server) NeedsSetup() bool { return len(s.Setup) > 0 }

// catalog is intentionally conservative: every Command and Setup below was
// verified from the project's own documentation.
var catalog = []Server{
	{
		ID:          "context7",
		Name:        "Context7 (library docs)",
		Description: "Up-to-date library/framework/API docs so the agent writes current, non-hallucinated code",
		Category:    "docs",
		RepoURL:     "https://github.com/upstash/context7",
		URL:         "https://mcp.context7.com/mcp",
		SetupNote:   "Hosted remote MCP; queries leave your environment. A CONTEXT7_API_KEY raises rate limits (optional).",
	},
	{
		ID:          "cloudflare",
		Name:        "Cloudflare (docs)",
		Description: "Up-to-date Cloudflare product docs so the agent builds correct Workers/Pages/DNS config",
		Category:    "docs",
		RepoURL:     "https://github.com/cloudflare/mcp-server-cloudflare",
		URL:         "https://docs.mcp.cloudflare.com/mcp",
		SetupNote:   "Hosted remote MCP (Cloudflare Docs); no key required.",
	},
	{
		ID:          "github",
		Name:        "GitHub",
		Description: "Repos, issues, PRs, code search and Actions for the agent",
		Category:    "dev",
		RepoURL:     "https://github.com/github/github-mcp-server",
		Command:     []string{"docker", "run", "-i", "--rm", "-e", "GITHUB_PERSONAL_ACCESS_TOKEN", "ghcr.io/github/github-mcp-server"},
		Env:         []string{"GITHUB_PERSONAL_ACCESS_TOKEN"},
		SetupNote:   "Needs Docker and a GitHub Personal Access Token exported as $GITHUB_PERSONAL_ACCESS_TOKEN.",
		Prereqs:     []string{"docker"},
	},
}

// Catalog returns the curated servers, sorted by id.
func Catalog() []Server {
	out := make([]Server, len(catalog))
	copy(out, catalog)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Lookup returns the catalog server with the given id, or ok=false if none.
func Lookup(id string) (Server, bool) {
	for _, s := range Catalog() {
		if s.ID == id {
			return s, true
		}
	}
	return Server{}, false
}

// Find returns the catalog server with the given id.
func Find(id string) (Server, bool) {
	for _, s := range catalog {
		if s.ID == id {
			return s, true
		}
	}
	return Server{}, false
}
