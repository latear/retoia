# `caldensec update` — key-gated, signed self-update

`caldensec update` upgrades the binary **without giving anyone GitHub access**.
The gateway key that authorizes models also authorizes updates, so distribution
stays behind the same secret you already hand out.

## What the client does

1. Resolves the gateway **key** (no prompt): `$CALDENSEC_API_KEY` → the caldensec
   keystore (OS keychain / fallback file) → OpenCode's `auth.json` (`<provider>.key`).
2. Resolves the update **host**: `$CALDENSEC_UPDATE_BASE`, else the scheme+host of
   `provider.<id>.options.baseURL` from `opencode.json` (e.g. `https://gw/v1` → `https://gw`).
3. `GET {host}/caldensec/manifest.json` with `Authorization: Bearer <key>`.
4. Picks the entry for its platform (`PlatformKey()` → `mac_arm64`, `linux_amd64`, …;
   `darwin` is spelled `mac`). If the manifest version equals the running version,
   it stops ("already on the latest").
5. Downloads the archive (Bearer), verifies the **SHA-256**, and — when a public
   key is embedded — the **Ed25519 signature** over the archive bytes.
6. Extracts `caldensec/caldensec` and atomically replaces the running binary.

If no public key is embedded, updates verify the SHA-256 only and print an
`UNSIGNED` warning. The transport is HTTPS and the endpoint is key-gated, but a
compromised endpoint could still serve a matching checksum — **enable signatures
for real tamper protection** (below).

## Server contract (what the middleware must serve)

All requests carry `Authorization: Bearer <gateway key>`; reject anything else.

`GET /caldensec/manifest.json`:

```json
{
  "version": "v0.2.6",
  "platforms": {
    "mac_arm64":   { "url": "https://gw/caldensec/dl/caldensec_mac_arm64.tar.gz",   "sha256": "<hex>", "sig": "<base64 ed25519, optional>" },
    "mac_amd64":   { "url": "https://gw/caldensec/dl/caldensec_mac_amd64.tar.gz",   "sha256": "<hex>", "sig": "<base64>" },
    "linux_arm64": { "url": "https://gw/caldensec/dl/caldensec_linux_arm64.tar.gz", "sha256": "<hex>", "sig": "<base64>" },
    "linux_amd64": { "url": "https://gw/caldensec/dl/caldensec_linux_amd64.tar.gz", "sha256": "<hex>", "sig": "<base64>" }
  }
}
```

The `url`s are fetched by the client with the same Bearer. The `sha256` values are
exactly the ones in the release's `checksums.txt`. Build the manifest from the
newest GitHub release (names + digests from `checksums.txt`) using a server-side
GitHub token; the user never sees GitHub.

## Reference middleware (ready to deploy)

A complete, dependency-free implementation ships in this repo:
[`cmd/caldensec-update-server`](../cmd/caldensec-update-server/). It validates the
Bearer key against LiteLLM and/or a static allowlist, builds the manifest from the
newest release (reading `checksums.txt` and sibling `*.sig` assets), and streams
the private assets — holding a GitHub token but **no signing key**.

```bash
go build -o caldensec-update-server ./cmd/caldensec-update-server
GITHUB_TOKEN=ghp_xxx LITELLM_URL=http://127.0.0.1:4000 PUBLIC_BASE=https://gw.example \
  ./caldensec-update-server     # serves /caldensec/ on :8099
```

Put it behind the gateway's TLS front door (reverse-proxy `location /caldensec/`)
so the client reaches it at the gateway host. Build/run/systemd/nginx details are
in its [README](../cmd/caldensec-update-server/README.md).

**Serverless option (no server to run):** a ready Cloudflare **Pages Function**
(`_worker.js`) ships in [`deploy/cloudflare/`](../deploy/cloudflare/) — same
contract, holds the GitHub token as a secret, validates keys against an allowlist
and/or LiteLLM, and deploys with a Pages-scoped token (`wrangler pages deploy`)
to e.g. `cs-updates.fico.ar`. The only requirements for any implementation are:
check the Bearer, never expose the GitHub token, and serve the JSON + assets over
HTTPS.

## Enabling signatures (recommended)

1. Generate a key pair: `caldensec update --keygen`. Keep the **private** key as a
   CI secret; it never leaves the signing step.
2. Paste the **public** key into `signingPubKeyB64` in
   `internal/updater/updater.go`, then rebuild and release. From then on the
   client refuses any update whose signature does not verify.
3. In the release pipeline, sign each `*.tar.gz` with the private key (Ed25519 over
   the raw archive bytes) and put the base64 signature in the manifest's `sig`.

With a key embedded, a compromised endpoint cannot push a tampered binary: the
signature check fails closed before anything is written.
