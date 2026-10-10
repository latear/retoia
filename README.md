# retoia-cli

A generic CLI base for the **retoia** project, forked from `caldensec` (a
security/pentest agent CLI for [OpenCode](https://opencode.ai)).

This fork keeps the reusable **install / provider / gateway framework** —
pointing an OpenCode provider at a gateway, managing MCP servers, keeping
`~/.local/bin` on `PATH`, a signed self-update mechanism, and a Bubble Tea TUI
for all of it — and removes everything specific to the original security
agent suite:

- The ~150-skill "cybersecurity skills" pack (`extras/cybersec-skills/`) and
  its `go:embed` wiring.
- The static Calden agent roster (`Calden-Orchestrator`, `Calden-alone`,
  `cs-coder`, `cs-recon`, `cs-pentester`, `cs-reviewer`, `cs-reporter`), their
  system prompts, and the install-time "write this agent suite" step.
- The `caldensec-config` MCP server (let an agent self-configure that fixed
  agent suite) and its `agent-mcp` subcommand.
- The security-specific MCP catalog entries (Kali full/curated, Nmap,
  Metasploit, Burp).

What's left is a CLI that installs a provider pointed at your own gateway,
wires a couple of generic MCP servers, and otherwise does nothing
security-specific. You bring your own agents and skills.

## MCP servers wired by this CLI

- **Engram** — persistent memory, wired via `internal/installer/engram.go`
  (it downloads/resolves the `engram` binary and registers it, rather than
  being a static catalog entry).
- **Context7** — up-to-date library/framework docs, a remote MCP in
  `internal/mcpservers/mcpservers.go`'s catalog (`caldensec mcp add context7`).
- **CodeGraph** — not wired. There was no CodeGraph MCP integration in the
  source this was forked from; see "What remains" below.

## Build / run

```bash
go build -o caldensec .      # requires Go 1.26+
go test ./...
```

The binary keeps its original name (`caldensec`) and Go module path
(`github.com/ficopix/caldensec`) for now — see "What remains" below.

### Install flow

```bash
./caldensec install           # interactive TUI: provider/gateway + Engram + Context7
./caldensec install --yes     # non-interactive, used by automation/CI
./caldensec uninstall         # reverses it: provider, MCP entries, credential
./caldensec mcp list|add|setup|remove   # manage MCP servers directly
./caldensec models            # assign an OpenCode model to whatever agents you add later
./caldensec sandbox ...        # scaffold the (currently still Kali-flavored) Docker sandbox
```

`caldensec models` and the sandbox's model picker are generic: they read
whatever `.agent.*` entries exist in `opencode.json` rather than a hardcoded
roster, so they keep working once you define your own agents.

Tests run against `testdata/opencode.sample.json` and temp-dir copies; they
never touch your real `~/.config/opencode/opencode.json`.

## What remains / TODO for the owner

- **Module/identity rename.** The Go module is still
  `github.com/ficopix/caldensec`, and the binary/command name, backup-file
  suffixes, env var prefixes (`CALDENSEC_*`), and various in-code comments
  still say "caldensec"/"CaldenSec". None of that was renamed — only the
  security/agent payload was stripped. Renaming the module path touches every
  import in the tree; do it deliberately in its own change.
- **The sandbox decision.** `internal/sandbox/` (the Kali Docker sandbox
  generator) still compiles and still generates a Dockerfile/README/compose
  file that talks about "the Calden suite" and "the cybersecurity skills
  pack" in their templated text — it has no Go-level dependency on the
  removed agent/skills code, so it was left untouched rather than redesigned.
  Decide whether the sandbox edition still makes sense for retoia (as a
  plain Docker-isolated OpenCode, dropping the Kali base image and its
  security tooling), or whether to drop it.
- **`internal/installtui`'s install flow was slimmed down**: the per-agent
  model-assignment step during install, the skills-pack toggle, the
  "CaldenSec config MCP" toggle, and the "kali-full MCP" toggle are gone from
  page 1 (now just Engram / Context7 / sidebar-plugins toggles + the server
  key page). If anything there still prints stale wording, it's cosmetic.
- **`caldensec seal-template`** (dev/distribution tooling for sealing a
  recommended agent→model template into `internal/onboard/sealed.go`) still
  exists and still compiles — it no longer special-cases "Calden"/"cs-"
  prefixed names (that filter was removed since there's no fixed roster
  anymore), but it's distribution tooling for the original project and may
  not be meaningful for retoia; left in place rather than guessed at.
- **`internal/config.go`'s "managed agent" convention** still special-cases
  names prefixed `Calden`/`cs-` (`isCaldenAgentName`) when deciding which
  `.agent.*` entries in `opencode.json` count as "managed" by this tool, in
  addition to the normal provider-prefix match. It's dead weight for agents
  you name anything else, but harmless (it only ever widens the match) and
  was left as-is rather than guessing at a replacement convention.
- **Trivial stale strings**: `install.sh`'s comment about "the skills pack
  ships inside the binary", and other scattered mentions of "Calden"/
  "CaldenSec" in flavor text, help strings, and shell scripts were left as-is
  per the fork's own scope — functional code was the priority, not a full
  rebrand.

## License

[MIT](LICENSE)
