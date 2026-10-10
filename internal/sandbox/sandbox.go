// Package sandbox generates the artifacts for the Reto IA "sandbox edition":
// a single Docker container (Debian base; the agent can install whatever tools
// it needs) with OpenCode + the Retoia suite installed INSIDE it, so the
// host's OpenCode is never touched. retoia is built from the repo source in a
// Go stage; the config is baked at container start by `retoia install --yes`,
// which reads the gateway URL / key from the environment.
//
// The artifacts are a starting point meant to be reviewed and built on the
// user's machine (this tool cannot run Docker); they are correct by construction
// but unverified here.
package sandbox

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Files returns the always-written sandbox artifacts (filename → content). When
// persist is true the compose file also bind-mounts Engram's and OpenCode's
// state dirs to ./sandbox-data so memory and sessions survive the container.
func Files(persist bool) map[string]string {
	return map[string]string{
		"Dockerfile.sandbox":   dockerfile,
		"compose.sandbox.yaml": composeFor(persist),
		"retoia-entrypoint.sh": entrypoint,
		"SANDBOX.md":           readme,
	}
}

// Write writes the sandbox artifacts into dir (created if needed), returning the
// filenames written. `.dockerignore` is written only when absent, so an existing
// one is never clobbered; when it is skipped, its name is returned with a
// " (kept existing)" suffix.
func Write(dir string, persist bool) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating %q: %w", dir, err)
	}

	files := Files(persist)
	names := make([]string, 0, len(files)+1)
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)

	var written []string
	for _, n := range names {
		mode := os.FileMode(0o644)
		if strings.HasSuffix(n, ".sh") {
			mode = 0o755
		}
		if err := os.WriteFile(filepath.Join(dir, n), []byte(files[n]), mode); err != nil {
			return written, fmt.Errorf("writing %s: %w", n, err)
		}
		written = append(written, n)
	}

	// .dockerignore: never clobber an existing one.
	di := filepath.Join(dir, ".dockerignore")
	if _, err := os.Stat(di); os.IsNotExist(err) {
		if err := os.WriteFile(di, []byte(dockerignore), 0o644); err != nil {
			return written, fmt.Errorf("writing .dockerignore: %w", err)
		}
		written = append(written, ".dockerignore")
	} else {
		written = append(written, ".dockerignore (kept existing)")
	}
	return written, nil
}

const dockerfile = `# Reto IA sandbox edition — a single isolated container, built from PUBLIC
# pieces only (no retoia repo needed). The Retoia config (opencode.json +
# skills + tui.json) is generated on the host by your retoia binary and baked
# in from ./opencode-config; the gateway key is written at start from the env,
# never baked into an image layer.
#
#   docker build -f Dockerfile.sandbox -t retoia-sandbox .
FROM debian:stable-slim
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates curl git nodejs npm python3 python3-pip \
    && rm -rf /var/lib/apt/lists/*

# uv, for uvx-based MCP servers.
RUN curl -LsSf https://astral.sh/uv/install.sh | sh
ENV PATH="/root/.local/bin:${PATH}"

# OpenCode itself, inside the container (the host's OpenCode is untouched).
# Pinned: the V2 HTTP API drifts between releases and Retoia's orchestration
# targets this exact shape (/api/session, /global/health, /doc). Bump deliberately.
RUN curl -fsSL https://opencode.ai/install | bash -s -- --version 1.18.34
ENV PATH="/root/.opencode/bin:${PATH}"

# Engram (persistent-memory MCP), inside the container. Its SQLite lives at
# ~/.engram in the container filesystem; it dies with the container unless
# persist mode bind-mounts it to ./sandbox-data/engram. Best effort — a failed download must not fail the whole image build.
RUN bash -c 'set -u; \
    case "$(uname -m)" in x86_64) a=amd64;; aarch64|arm64) a=arm64;; *) a=amd64;; esac; \
    tag="$(curl -fsSL --connect-timeout 15 --max-time 60 https://api.github.com/repos/Gentleman-Programming/engram/releases/latest | grep -o "\"tag_name\": *\"[^\"]*\"" | head -1 | sed -E "s/.*\"([^\"]*)\"$/\1/")"; \
    curl -fsSL --connect-timeout 15 --max-time 180 "https://github.com/Gentleman-Programming/engram/releases/download/${tag}/engram_${tag#v}_linux_${a}.tar.gz" | tar xz -C /usr/local/bin engram; \
    engram --version' || echo "engram: optional install skipped"

# Baked Retoia config, generated on the host (no retoia binary needed here).
COPY opencode-config/ /root/.config/opencode/

COPY retoia-entrypoint.sh /usr/local/bin/retoia-entrypoint
RUN chmod +x /usr/local/bin/retoia-entrypoint

# OpenCode is a TUI: a usable terminal type so it renders (a bare docker exec
# otherwise leaves TERM=dumb and the UI flashes and exits). Placed late to keep
# the expensive base/OpenCode layers cached across rebuilds.
ENV TERM=xterm-256color

# Stamp the retoia version that built this image (declared late so it only
# rebusts this final layer). The installer reads it back to tell whether a
# running sandbox is up to date.
ARG RETOIA_VERSION=dev
LABEL retoia.version="${RETOIA_VERSION}"

WORKDIR /work
ENTRYPOINT ["/usr/local/bin/retoia-entrypoint"]
CMD []
`

// PersistEnabled reports whether the compose file in dir already mounts the
// persisted state dirs, so a refresh can keep the user's earlier choice.
func PersistEnabled(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "compose.sandbox.yaml"))
	return err == nil && strings.Contains(string(data), persistMounts[0])
}

// persistMounts are the extra compose volumes added in persist mode.
var persistMounts = []string{
	"./sandbox-data/engram:/root/.engram",
	"./sandbox-data/opencode:/root/.local/share/opencode",
}

func composeFor(persist bool) string {
	if !persist {
		return compose
	}
	var b strings.Builder
	b.WriteString(compose)
	for _, m := range persistMounts {
		b.WriteString("      - " + m + "\n")
	}
	return b.String()
}

const compose = `# Reto IA sandbox edition.
#   RETOIA_API_KEY=... docker compose -f compose.sandbox.yaml up -d --build
services:
  retoia:
    build:
      context: .
      dockerfile: Dockerfile.sandbox
      args:
        RETOIA_VERSION: "${RETOIA_VERSION:-dev}"
    image: retoia-sandbox
    stdin_open: true
    tty: true
    # The entrypoint runs "sleep infinity", which ignores SIGTERM, so a recreate
    # otherwise waits the full 10s default before SIGKILL — looking frozen. Cap it.
    stop_grace_period: 3s
    environment:
      RETOIA_API_KEY: "${RETOIA_API_KEY:-}"
    volumes:
      - ./sandbox-work:/work
`

const entrypoint = `#!/usr/bin/env bash
# Write the gateway credential from the environment (so the key is never baked
# into an image layer), then keep the container alive. OpenCode is a TUI, so it
# is NOT the main process (it would exit without a TTY and kill the container):
# attach interactively with
#   docker compose -f compose.sandbox.yaml exec retoia opencode
set -euo pipefail

if [ -n "${RETOIA_API_KEY:-}" ]; then
  mkdir -p "$HOME/.local/share/opencode"
  printf '{ "retoia": { "type": "api", "key": "%s" } }\n' "$RETOIA_API_KEY" \
    > "$HOME/.local/share/opencode/auth.json"
  chmod 600 "$HOME/.local/share/opencode/auth.json"
fi

exec sleep infinity
`

const dockerignore = `.git
.codegraph
dist
retoia
retoia-models
*.bak-retoia*
sandbox-work
sandbox-data
`

const readme = `# Reto IA — sandbox edition

Everything runs inside one container (Debian base), so your host OpenCode and its
config are never touched. The agent can install whatever tools it needs inside
the container with apt.

This runs the SAME Retoia suite as the Local edition - same agents, same
skills, same per-agent model templates - just isolated: OpenCode is pinned to
1.18.34 (Local tracks latest) and everything dies with the container except the
bind mounts (/work, plus the persisted state below).

Built from PUBLIC pieces only — you do NOT need the retoia repo. Your retoia
binary generates the Retoia config on the host; the image bakes it in on top of
the official OpenCode installer and the debian:stable-slim base.

## Onboarding (process 0)

Opening the sandbox lands on Retoia-Orchestrator. When there is no task
file yet, it runs the intake itself, in the same session - no agent switch:
the concrete goal, then scope. Once that is set, it orchestrates bounded by it
and stops when the goal is met. Task file: /work/engagement.yaml. Full guide: docs/onboarding.md (in the retoia repo).

## Build & run

"retoia install" → Sandbox does this for you (detecting Docker, or setting up
Colima on macOS). To do it by hand from this directory:

    RETOIA_API_KEY='<key>' docker compose -f compose.sandbox.yaml up -d --build
    docker compose -f compose.sandbox.yaml exec retoia opencode   # or: … exec retoia bash

The gateway key is written into the container at start from RETOIA_API_KEY, so
it is never stored in an image layer. Your work dir is mounted at ./sandbox-work.

## Persist data (on by default)

So memory and sessions survive the container being closed or crashing, persist
mode also bind-mounts, next to sandbox-work/:

    ./sandbox-data/engram   → /root/.engram                      (Engram memory)
    ./sandbox-data/opencode → /root/.local/share/opencode        (sessions, db, credentials)

Turn it off in the install wizard (or ` + "`retoia sandbox init --no-persist`" + `) for a
fully ephemeral sandbox. Treat sandbox-data/ as sensitive: it holds credentials.

## Teardown

    retoia sandbox uninstall            # docker compose down (stop + remove container)
    retoia sandbox uninstall --rmi      # also remove the retoia-sandbox image
    retoia sandbox uninstall --purge    # also delete this scaffold (keeps sandbox-work/ and sandbox-data/)
    retoia sandbox uninstall --all      # remove EVERYTHING incl. your work + memory (asks to confirm; --yes skips)

By default sandbox-work/ and sandbox-data/ are kept — they hold your work and
your persisted memory/state. --all is the only command that deletes them, and
it asks for confirmation first.

## Notes

- These files are a reviewed starting point; validate the build on your machine.
- Requires Docker (or Colima on macOS) and internet for the base image + OpenCode.
- Memory: give the Docker VM at least ~4 GB to BUILD the image (the 2 GB Colima
  default OOM-kills it, exit 137); running the built container needs ~2 GB. On
  Colima: colima start --cpus 4 --memory 6
- Console tools run inside the container; nothing installs on your host.
`
