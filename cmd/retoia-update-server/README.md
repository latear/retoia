# retoia-update-server

The key-gated middleware behind `retoia update`. Clients authenticate with
their **gateway key**; this server (holding a GitHub token) builds the manifest
from the newest release and streams the private assets. Nobody needs GitHub
access, and the server holds **no signing key** — see [`../../docs/UPDATE.md`](../../docs/UPDATE.md)
for the contract and the signing model.

Dependency-free (standard library only).

## Build

```bash
go build -o retoia-update-server ./cmd/retoia-update-server
```

## Run

Configure via environment. You must set `GITHUB_TOKEN` and at least one auth
source (`LITELLM_URL` and/or `UPDATE_KEYS`); it refuses to run open.

| Env | Default | Purpose |
|-----|---------|---------|
| `ADDR` | `:8099` | listen address |
| `GITHUB_REPO` | `latear/retoia` | owner/repo of the releases |
| `GITHUB_TOKEN` | — (required) | token with read access to the private repo |
| `PUBLIC_BASE` | `https://`+Host | external base URL used in asset links |
| `LITELLM_URL` | — | validate keys via `GET {LITELLM_URL}/v1/models` |
| `UPDATE_KEYS` | — | comma-separated static allowlist of accepted keys |

```bash
GITHUB_TOKEN=ghp_xxx \
LITELLM_URL=http://127.0.0.1:4000 \
PUBLIC_BASE=https://gw.example \
./retoia-update-server
```

It serves:

- `GET /retoia/manifest.json` — the signed manifest (Bearer-authed).
- `GET /retoia/dl/<asset>` — a release asset (Bearer-authed).
- `GET /healthz` — unauthenticated liveness.

Put it behind the same TLS front door as the gateway (so the client reaches it
at the gateway host), e.g. a reverse-proxy location for `/retoia/`.

## systemd

```ini
[Unit]
Description=retoia update server
After=network-online.target

[Service]
Environment=GITHUB_TOKEN=ghp_xxx
Environment=LITELLM_URL=http://127.0.0.1:4000
Environment=PUBLIC_BASE=https://gw.example
ExecStart=/usr/local/bin/retoia-update-server
Restart=on-failure
DynamicUser=yes

[Install]
WantedBy=multi-user.target
```

## nginx (reverse proxy on the gateway host)

```nginx
location /retoia/ {
    proxy_pass http://127.0.0.1:8099;
    proxy_set_header Authorization $http_authorization;
}
```

Then `retoia update` — pointing at the gateway's scheme+host — reaches
`/retoia/manifest.json` with the gateway key and self-updates.
