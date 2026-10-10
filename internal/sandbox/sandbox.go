// Package sandbox generates the artifacts for the Reto IA "sandbox edition":
// a single Docker container (Kali base, so pentest tools are available and
// installable) with OpenCode + the Retoia suite installed INSIDE it, so the
// host's OpenCode is never touched. retoia is built from the repo source in a
// Go stage; the config is baked at container start by `retoia install --yes`,
// which reads the gateway passphrase / key from the environment.
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

// Files returns the always-written sandbox artifacts (filename → content). The
// build context is the repo root, so run docker from a retoia checkout.
func Files() map[string]string {
	return map[string]string{
		"Dockerfile.sandbox":   dockerfile,
		"compose.sandbox.yaml": compose,
		"retoia-entrypoint.sh": entrypoint,
		"SANDBOX.md":           readme,
	}
}

// Write writes the sandbox artifacts into dir (created if needed), returning the
// filenames written. `.dockerignore` is written only when absent, so an existing
// one is never clobbered; when it is skipped, its name is returned with a
// " (kept existing)" suffix.
func Write(dir string) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating %q: %w", dir, err)
	}

	files := Files()
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
FROM kalilinux/kali-rolling
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates curl git nodejs npm python3 python3-pip nmap \
    && rm -rf /var/lib/apt/lists/*

# uv, for uvx-based MCP servers (e.g. the kali MCP client).
RUN curl -LsSf https://astral.sh/uv/install.sh | sh
ENV PATH="/root/.local/bin:${PATH}"

# OpenCode itself, inside the container (the host's OpenCode is untouched).
# Pinned: the V2 HTTP API drifts between releases and Retoia's orchestration
# targets this exact shape (/api/session, /global/health, /doc). Bump deliberately.
RUN curl -fsSL https://opencode.ai/install | bash -s -- --version 1.18.34
ENV PATH="/root/.opencode/bin:${PATH}"

# Engram (persistent-memory MCP), inside the container. Its SQLite lives at
# ~/.engram in the container filesystem, so the memory is per-container and dies
# with it. Best effort — a failed download must not fail the whole image build.
RUN bash -c 'set -u; \
    case "$(uname -m)" in x86_64) a=amd64;; aarch64|arm64) a=arm64;; *) a=amd64;; esac; \
    tag="$(curl -fsSL --connect-timeout 15 --max-time 60 https://api.github.com/repos/Gentleman-Programming/engram/releases/latest | grep -o "\"tag_name\": *\"[^\"]*\"" | head -1 | sed -E "s/.*\"([^\"]*)\"$/\1/")"; \
    curl -fsSL --connect-timeout 15 --max-time 180 "https://github.com/Gentleman-Programming/engram/releases/download/${tag}/engram_${tag#v}_linux_${a}.tar.gz" | tar xz -C /usr/local/bin engram; \
    engram --version' || echo "engram: optional install skipped"

# Tor (torsocks / proxychains4 / iptables) — optional, chosen in the installer.
# Placed late (and behind an ARG) so toggling it does not rebust the expensive
# base/OpenCode/Engram layers. When off, the entrypoint skips the Tor daemon too.
ARG RETOIA_WITH_TOR=1
RUN if [ "$RETOIA_WITH_TOR" = "1" ]; then \
        apt-get update && apt-get install -y --no-install-recommends \
            tor torsocks proxychains4 iptables \
        && rm -rf /var/lib/apt/lists/*; \
    else echo "retoia: Tor not requested — skipping tor/torsocks/proxychains4"; fi

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

const compose = `# Reto IA sandbox edition.
#   RETOIA_API_KEY=... docker compose -f compose.sandbox.yaml up -d --build
services:
  retoia:
    build:
      context: .
      dockerfile: Dockerfile.sandbox
      args:
        RETOIA_VERSION: "${RETOIA_VERSION:-dev}"
        RETOIA_WITH_TOR: "${RETOIA_WITH_TOR:-1}"
    image: retoia-sandbox
    stdin_open: true
    tty: true
    # The entrypoint runs "sleep infinity", which ignores SIGTERM, so a recreate
    # otherwise waits the full 10s default before SIGKILL — looking frozen. Cap it.
    stop_grace_period: 3s
    environment:
      RETOIA_API_KEY: "${RETOIA_API_KEY:-}"
      # Set to 1 to force ALL public egress + DNS through Tor (see SANDBOX.md).
      RETOIA_TOR: "${RETOIA_TOR:-}"
    cap_add:
      - NET_RAW
      - NET_ADMIN
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

# Tor. The daemon always runs so 'torsocks <tool>' / 'proxychains4 <tool>' work
# per command (SocksPort 9050). With RETOIA_TOR=1 it also installs a
# transparent proxy: every public TCP connection and all DNS egress go through
# Tor, while loopback and private/LAN ranges stay direct so in-scope internal
# targets remain reachable. Tor is TCP-only, so SYN/UDP/raw scans (nmap -sS/-sU)
# will not work while it is enforced — use 'nmap -sT -Pn'. All best effort: a
# failure here must never kill the container, so it runs under 'set +e'. Skipped
# entirely when Tor was not installed in the image (the installer's Tor toggle).
if command -v tor >/dev/null 2>&1; then
(
  set +e
  install -d -o debian-tor -g debian-tor /var/lib/tor 2>/dev/null
  cat > /etc/tor/torrc.retoia <<'TORRC'
User debian-tor
DataDirectory /var/lib/tor
SocksPort 9050
TransPort 9040
DNSPort 5353
TORRC
  tor -f /etc/tor/torrc.retoia >/var/log/tor-retoia.log 2>&1 &
  tor_up=0
  for _ in $(seq 1 30); do
    (exec 3<>/dev/tcp/127.0.0.1/9050) 2>/dev/null && { exec 3>&-; tor_up=1; break; }
    sleep 1
  done

  if [ "$tor_up" != "1" ]; then
    echo "[retoia] Tor failed to start (see /var/log/tor-retoia.log); egress stays DIRECT (not anonymized)."
  elif [ "${RETOIA_TOR:-}" = "1" ] || [ "${RETOIA_TOR:-}" = "true" ]; then
    TOR_UID="$(id -u debian-tor)"
    iptables -t nat -A OUTPUT -m owner --uid-owner "$TOR_UID" -j RETURN
    iptables -t nat -A OUTPUT -o lo -j RETURN
    iptables -t nat -A OUTPUT -p udp --dport 53 -j REDIRECT --to-ports 5353
    iptables -t nat -A OUTPUT -p tcp --dport 53 -j REDIRECT --to-ports 5353
    for net in 127.0.0.0/8 10.0.0.0/8 172.16.0.0/12 192.168.0.0/16 169.254.0.0/16; do
      iptables -t nat -A OUTPUT -d "$net" -j RETURN
    done
    iptables -t nat -A OUTPUT -p tcp --syn -j REDIRECT --to-ports 9040
    echo "[retoia] Tor transparent proxy ON: public TCP + DNS via Tor, private/LAN direct (nmap: use -sT -Pn)."
  else
    echo "[retoia] Tor daemon up (SocksPort 9050). Per command: torsocks <tool> / proxychains4 <tool>. Set RETOIA_TOR=1 to force ALL public egress via Tor."
  fi
) || echo "[retoia] Tor setup failed; continuing without it."
else
  echo "[retoia] Tor not installed in this image (disabled at build); egress is direct."
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
`

const readme = `# Reto IA — sandbox edition

Everything runs inside one container (Kali base), so your host OpenCode and its
config are never touched. Pentest tools are available via apt and the agent can
install more, just like a Kali box.

This runs the SAME Retoia suite as the Local edition - same agents, same
cybersecurity skills pack, same sealed per-agent model templates - just
isolated and ephemeral: OpenCode is pinned to 1.18.34 (Local tracks latest)
and everything dies with the container except the /work bind mount. Tor is
optional (see below).

Built from PUBLIC pieces only — you do NOT need the retoia repo. Your retoia
binary generates the Retoia config on the host; the image bakes it in on top of
the official OpenCode installer and the kalilinux/kali-rolling base.

## Onboarding (process 0)

Opening the sandbox lands on Retoia-Orchestrator. When there is no engagement
file yet, it runs the intake itself, in the same session - no agent switch:
attack type, then the concrete goal, then scope. Once that is set, it
orchestrates bounded by it and stops when the goal is met. Engagement file:
/work/engagement.yaml. Full guide: docs/onboarding.md (in the retoia repo).

## Build & run

"retoia install" → Sandbox does this for you (detecting Docker, or setting up
Colima on macOS). To do it by hand from this directory:

    RETOIA_API_KEY='<key>' docker compose -f compose.sandbox.yaml up -d --build
    docker compose -f compose.sandbox.yaml exec retoia opencode   # or: … exec retoia bash

The gateway key is written into the container at start from RETOIA_API_KEY, so
it is never stored in an image layer. Your work dir is mounted at ./sandbox-work.

## Tor (OPSEC for authorized recon)

The container ships tor, torsocks and proxychains4. The Tor daemon runs on start
(SocksPort 9050), so you can route any single command:

    torsocks nmap -sT -Pn <target>       # Tor is TCP-only: connect scans only
    proxychains4 curl https://ifconfig.me

To force ALL public egress (and DNS) through Tor, start the container with
RETOIA_TOR=1:

    RETOIA_API_KEY='<key>' RETOIA_TOR=1 docker compose -f compose.sandbox.yaml up -d --build

Loopback and private/LAN ranges (10/8, 172.16/12, 192.168/16, 169.254/16) stay
DIRECT, so in-scope internal targets remain reachable and Tor is not asked to
route addresses it cannot reach. Caveats: Tor is TCP-only (no -sS/-sU/raw scans;
use "nmap -sT -Pn"), exit nodes are often slow or blocked, and .onion needs
torsocks per command. Needs the NET_ADMIN capability (already in the compose).

## Teardown

    retoia sandbox uninstall            # docker compose down (stop + remove container)
    retoia sandbox uninstall --rmi      # also remove the retoia-sandbox image
    retoia sandbox uninstall --purge    # also delete this scaffold (keeps sandbox-work/)

sandbox-work/ is always kept — it is your mounted work dir.

## Notes

- These files are a reviewed starting point; validate the build on your machine.
- Requires Docker (or Colima on macOS) and internet for the base image + OpenCode.
- Memory: give the Docker VM at least ~4 GB to BUILD the image (the 2 GB Colima
  default OOM-kills it, exit 137); running the built container needs ~2 GB. On
  Colima: colima start --cpus 4 --memory 6
- Console tools run inside the container; nothing installs on your host.
`
