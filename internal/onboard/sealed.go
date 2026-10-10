package onboard

// sealedBase is the gateway URL sealed with internal/urlseal, opened at runtime
// with the passphrase in $CALDENSEC_UNLOCK. It is EMPTY by default so the repo
// and the binary carry no gateway URL.
//
// To bake one in, the distributor runs:
//
//	CALDENSEC_UNLOCK='the-shared-passphrase' caldensec seal https://your-gateway/v1
//
// and pastes the printed constant here, then rebuilds and releases. Installers
// are given the passphrase out-of-band and set $CALDENSEC_UNLOCK.
//
// While this is empty, the gateway must be provided explicitly via the
// --gateway flag or $CALDENSEC_BASE.
var sealedBase = "AQ0hd0X09UdGmBKemaqlUGBx3HQncXbMbeouvYBQdwK1BUFjvQ9Ey8v6hXI8XWASfOCM5xwvqgdBYcflj7RrZWiHRA=="

// sealedUpdateBase is the `caldensec update` endpoint URL, sealed with the SAME
// install passphrase as sealedBase, so the distributed binary carries no update
// infra in the clear. EMPTY by default. Bake it with:
//
//	CALDENSEC_UNLOCK='the-shared-passphrase' caldensec seal-update https://cs-updates.fico.ar
//
// and paste the printed constant here, then rebuild. When empty, `caldensec
// update` falls back to $CALDENSEC_UPDATE_BASE, then the gateway host.
var sealedUpdateBase = "ATCnYXLLWIzD4ntGZoX+7EfCsAzFY6Xez3nJA9bOBXHwa7Z+9uvhFF7nSQWn85wVI7fEZBIgkWvzeF9hTW7F"

// sealedAgentsCalden, sealedAgentsCloud and sealedAgentsBoth are the suggested
// agent→model assignments (JSON objects of {"<agent>":"<model id>"}), sealed
// with the SAME install passphrase as sealedBase, so the real model names never
// appear in the repo or the binary in the clear.
//
// At install, once unlocked, the profile is chosen by what the key authorizes:
// a key with both Calden and cloud models gets the "both" template, a cloud-only
// key gets "cloud", a Calden-only key gets "calden"; each pick still falls back
// to the first authorized model when that exact model is not authorized. No key
// leaves agents unassigned (they can be set manually with `caldensec models`).
//
// The distributor bakes them with:
//
//	CALDENSEC_UNLOCK='pass' caldensec seal-template --slot calden --from-json '{...}'
//	CALDENSEC_UNLOCK='pass' caldensec seal-template --slot cloud  --from-json '{...}'
//	CALDENSEC_UNLOCK='pass' caldensec seal-template --slot both   --from-json '{...}'
//
// pasting each printed constant here and rebuilding. Empty means no template.
var (
	sealedAgentsCalden = "ARFTSo0c7aIg2ocv09Xdy2qyWbz706+rw+2un6c/eRuR/0HMQksvRQ6kxbT+geBiVSg2rykOPbeu0GAlYv9LIgBjHQ+Q9x8G4GHXvYSreXrI7uFbGk0nL+y122aORXxKlgc8m5u8yM9/lT5LPVNb0o/AoVIwZnYskZQwXdYbsPkvLm5n3W2Itarh+fdYxbq5S+9menBGU9rnB9OmRdbyeBN6xsxrhwOqJx7acegFRi2bFQ5nc0oRq13a3Tibc4seLPZkI6MoqABjYBKHD8TAOULoqDb6VGdgpauY1RktJYzrcwaXZnKCEwxho3YVwYDZC5+d9ETL72fd4C299AOkclX7EH/WTqwLnZUDteg="
	sealedAgentsCloud  = "AUd+IH3kJlT+7O4JfZ2JX8Ay9gtwNHq8mpMlDdhhNQPt+lSXsIcShwZ4MvTmRKwv/YtUtDYOY7wgTzy/4GVnyDkKT5qbvlxf4VCoNBmBX4nTSgV4OF0Tg+xaM7ntXC+XvR9Gr/lg7s7iLdDC4Mr+F3e+kRoTB4giYMzsb5MTFVxND+sCsZqKfud22w2d6e8BynhYcGHIxnwmyxWCiBhJ2aD9yni8ptF03/L75nGdNGVVUvhxSgiwzTTY2WTtX7G/5ooGlHB6VeykdlacJVazMqRRRy/vRpwv6HSctX5599piZujiPeuokWEZXYhQNildJ34k+c1/BdbH9+X1Vy2quAJ6812CZ7MEhkRoE5zeaTk4KmXgLc4I4vvaHWIdNaTSJLnS"
	sealedAgentsBoth   = "AVZJw/DJpIb3I2h5Jx3/N/9zyKIM4FizWQWGxnqsFL2jv7qQ3Pa8o2xuZEtBoFNgBdcupEOe/pRCOzY0zQq79npcBKJvGZ24px/fXEyrrt0tDRKFW0P2lohxHtCB+8ferg/LuZnVK2TyJAl3PinMnScTA9G9AwcxVCLmyjmTFdlfFI55CVB0B4aQYNtMcIKmkCR3Didn3pZ2zy4dIQeTLCN/ajYtCQqrRjZ9rsXgQPYPm/R93T4yoNKMJN+iONerdXzhDjAgz5NeBwBRSDKYaVM56YMP4+FKGOivEmd/joaQzob/Ffnhw4HPlJa+WFpKK4DUWDMQPty6k/qBNtCm4CUe7fAotpYyzK2bU8+P2WYdkpTigg=="
)
