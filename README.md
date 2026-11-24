# retoia-cli

The **Reto IA** command-line installer. It wires [OpenCode](https://opencode.ai)
to the Reto IA gateway: you paste a server key, the CLI validates it against the
gateway, pulls your authorized models, and seeds a single principal agent ready
to use. It also keeps `~/.local/bin` on `PATH`, wires a couple of MCP servers,
and self-updates from a signed release feed.

Vendored from an earlier installer framework, with the gateway kept **in the
clear** (no sealing/passphrase), reduced to a **single provider**, and a
**single principal agent** created at install.

## What it does

- **Gateway provider** — registers an OpenCode provider (`retoia`) pointed at
  the Reto IA gateway `https://llm.retoia.app` (override with `--gateway` or
  `$RETOIA_BASE`).
- **Server key → models** — asks for your `sk-...` server key, validates it
  against the gateway, and writes the authorized models into `opencode.json`.
  The raw key goes only into OpenCode's `auth.json` (0600), never into
  `opencode.json`.
- **Principal agent** — seeds one `primary` agent named `retoia` in
  `opencode.json`, pointed at the gateway provider and a default model.
- **MCP servers** — a curated catalog wired into `opencode.json` via
  `retoia mcp add <id>`: Engram (persistent memory), Context7 (library docs),
  **GitHub** (`ghcr.io/github/github-mcp-server` via Docker; needs
  `$GITHUB_PERSONAL_ACCESS_TOKEN`), and **Cloudflare** (keyless remote docs MCP
  at `https://docs.mcp.cloudflare.com/mcp`).
- **Impeccable design skill** — optional install step (on by default) that runs
  `npx skills add pbakaus/impeccable` so the agent gets the Impeccable frontend
  design skill. Requires Node/`npx`; skipped with a note if it is missing.
- **Self-update** — `retoia update` pulls signed releases from
  `https://cli.retoia.app` (override with `$RETOIA_UPDATE_BASE`).

## Sandbox

`retoia sandbox` scaffolds a plain **Debian**-based, Docker-isolated OpenCode
container (no Kali, no pentest tooling), so an agent can run with your host
config untouched.

## Platforms

- **macOS** (Intel + Apple Silicon)
- **Linux** (amd64 + arm64)
- **Windows — via WSL.** Run the Linux install inside a WSL2 distro; it is
  detected as Linux and uses the Linux binary. There is no native Windows build.

## Install

```bash
# macOS / Linux / WSL
curl -fsSL https://cli.retoia.app/install | bash
```

The script downloads the release binary for your OS/arch from `cli.retoia.app`
(gated by your gateway server key; no GitHub access or `gh` needed), puts
`retoia` on your `PATH`, and launches `retoia install`. It reads the key from
`$RETOIA_API_KEY` or prompts for it.

## Commands

```bash
retoia install            # interactive TUI: gateway provider + server key + agent + MCP
retoia install --yes      # non-interactive (reads the key from $RETOIA_API_KEY)
retoia uninstall          # reverses it: provider, principal agent, MCP entries, credential
retoia models             # (re)assign an OpenCode model to your agents
retoia mcp list|add|setup|remove   # manage MCP servers
retoia update             # self-update to the latest signed release
retoia key                # show / reset the stored server key
retoia version
```

## Build / test

```bash
go build -o retoia .      # requires Go 1.26+
go test ./...
```

Tests run against `testdata/opencode.sample.json` and temp-dir copies; they
never touch your real `~/.config/opencode/opencode.json`.

## What remains / TODO for the owner

- **Create the release repo.** The Go module is `github.com/latear/retoia` and
  the release/download references point at `latear/retoia` (`install.sh`,
  `.goreleaser.yaml`, the Cloudflare worker, the update server). Create that
  GitHub repo and publish releases so `install.sh` and `retoia update` resolve.
- **Deploy the update host.** `deploy/cloudflare/` serves `cli.retoia.app`
  (`/retoia/manifest.json` + `/retoia/dl/<asset>`), key-gated against the
  gateway. Set `GITHUB_TOKEN` and `LITELLM_URL` (= the gateway) as secrets.

## License

[MIT](LICENSE)
