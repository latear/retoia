#!/usr/bin/env bash
# Verify the sealed agent→model templates in internal/onboard/sealed.go unseal
# correctly, with the SAME passphrase as the sealed gateway URL. The passphrase
# is read interactively (hidden) and passed only to the test subprocess — never
# on a command line, never stored.
#
# Uso:
#   ./verify-template.sh
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")"

PASS="${CALDENSEC_UNLOCK:-}"
if [ -z "$PASS" ]; then
  [ -r /dev/tty ] || { echo "No hay TTY para pedir la clave. Corré el script en una terminal interactiva." >&2; exit 1; }
  printf "Passphrase del instalador (se escribe oculta): " >&2
  read -rs PASS < /dev/tty; echo >&2
fi
[ -n "$PASS" ] || { echo "Passphrase vacía; aborto." >&2; exit 1; }

echo "Verificando que los 3 templates + la URL desellen con esta clave ..." >&2
echo >&2
CALDENSEC_UNLOCK="$PASS" go test -run TestRealTemplates_Unseal -v ./internal/onboard
