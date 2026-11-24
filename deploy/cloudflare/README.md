# cli.retoia.app — Cloudflare Pages Function for `retoia update`

A serverless, key-gated update endpoint — no server to run. It holds the GitHub
token as a secret and is the only thing that talks to GitHub; clients fetch
updates with their **gateway key** and never touch GitHub. Same contract as
[`../../cmd/retoia-update-server`](../../cmd/retoia-update-server/), so the
existing `retoia update` client works against it unchanged.

Shipped as a single `_worker.js` (Pages "advanced mode"), so a **Pages-scoped**
Cloudflare token deploys it. (It is also a valid standalone Worker — see the end.)

## Deploy (Pages)

```bash
cd deploy/cloudflare
# a Pages-scoped API token works here (export CLOUDFLARE_API_TOKEN, or `wrangler login`)

wrangler pages project create retoia-cli          # first time only
wrangler pages secret put GITHUB_TOKEN --project-name retoia-cli   # read access to the private repo
# auth: at least one of —
wrangler pages secret put LITELLM_URL  --project-name retoia-cli   # validate keys vs the gateway, OR
wrangler pages secret put UPDATE_KEYS  --project-name retoia-cli   # comma-separated allowlist

wrangler pages deploy . --project-name retoia-cli
```

Then map the domain: Cloudflare dashboard → the `retoia-cli` Pages project →
Custom domains → add **cli.retoia.app**.

## Auth

`Authorization: Bearer <key>` is accepted when it is in `UPDATE_KEYS` (allowlist)
or — if `LITELLM_URL` is set — when LiteLLM accepts it (`GET {LITELLM_URL}/v1/models`),
so the same gateway key authorizes updates. No/wrong key on the API → `401`; the
root and other paths → `302` to `HOME_URL`.

## Endpoints

- `GET /retoia/manifest.json` — `{version, platforms{<os>_<arch>:{url,sha256,sig}}}`
- `GET /retoia/dl/<asset>` — the release asset bytes
- `GET /install` (also `/install.sh`) — the repo's `install.sh`, unauthenticated (the binary download it performs is what is key-gated): `curl -fsSL https://cli.retoia.app/install | bash`
- `GET /healthz` — unauthenticated liveness

## Token scopes

- **Pages token** (what you likely have): works with `wrangler pages deploy`.
  Needs *Cloudflare Pages: Edit*. Set secrets with `wrangler pages secret put`.
- A **Workers-only token does NOT** deploy this via `wrangler pages deploy`.

## Also deployable as a plain Worker

The same `_worker.js` runs as a standalone Worker if you prefer (needs a
*Workers Scripts: Edit* token): `wrangler deploy _worker.js --name retoia-cli`,
secrets via `wrangler secret put`.

## Signatures

The endpoint serves the `*.sig` assets from the release but never holds the
signing key, so a compromised endpoint cannot forge an update — the client
verifies the Ed25519 signature against its embedded public key (see
`../../docs/UPDATE.md`).
