#!/usr/bin/env bash
# Instalador de retoia. Por defecto DESCARGA el binario prearmado (no requiere
# Go ni GitHub): lo baja de cli.retoia.app.
# Lo deja en ~/.local/bin y abre el instalador interactivo (retoia install).
#
# Una línea:
#   curl -fsSL https://cli.retoia.app/install | bash
#
# La clave de acceso sale de $RETOIA_API_KEY; si no está, te la pide por la terminal.
# No hace falta gh ni acceso al repo de GitHub.
#
# Uso:
#   ./install.sh                 # baja el binario y abre el instalador
#   ./install.sh --build         # compila desde fuente (desarrollo; requiere el repo + Go)
#   ./install.sh --version vX.Y.Z  # solo aplica a --build; la descarga siempre es la última
#
# Variables:
#   RETOIA_API_KEY      tu clave de acceso
#   RETOIA_UPDATE_BASE  base del servidor de updates (default https://cli.retoia.app)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

BIN="retoia"
DEST_DIR="$HOME/.local/bin"
DEST="$DEST_DIR/$BIN"
STALE_BIN="$DEST_DIR/retoia-models"

MODE="download"   # download | build
VERSION=""        # solo informativo; la descarga siempre es la última

while [ $# -gt 0 ]; do
  case "$1" in
    --build) MODE="build" ;;
    --version) shift; VERSION="${1:-}"; [ -n "$VERSION" ] || { echo "--version necesita un tag, p.ej. v0.1.0" >&2; exit 1; } ;;
    --version=*) VERSION="${1#*=}" ;;
    -h|--help)
      echo "Uso: ./install.sh [--build] [--version vX.Y.Z]"
      exit 0
      ;;
    *) echo "Opción desconocida: $1 (usá --build o --version vX.Y.Z)" >&2; exit 1 ;;
  esac
  shift
done

# detect_platform mapea uname a las combinaciones que publica GoReleaser y arma
# el nombre exacto del asset: retoia_<os>_<arch>.tar.gz
detect_platform() {
  local os arch
  case "$(uname -s)" in
    Darwin) os="mac" ;;
    Linux)  os="linux" ;;
    MINGW*|MSYS*|CYGWIN*) echo "Windows nativo no soportado: ejecutá este instalador dentro de WSL (WSL2 + una distro Linux)." >&2; exit 1 ;;
    *) echo "SO no soportado: $(uname -s). En Windows usá WSL; o --build si tenés Go." >&2; exit 1 ;;
  esac
  case "$(uname -m)" in
    x86_64|amd64) arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
    *) echo "Arquitectura no soportada: $(uname -m). Usá --build si tenés Go." >&2; exit 1 ;;
  esac
  PLAT="${os}_${arch}"
  ASSET="${BIN}_${os}_${arch}.tar.gz"
}

# verify_sha256 compara el sha256 de un archivo con el valor esperado; si no hay
# herramienta o valor, avisa y sigue (no bloquea la instalación).
verify_sha256() {
  local file="$1" want="$2"
  [ -n "$want" ] || { echo "  (el manifest no trae sha256; salteo la verificación)"; return 0; }

  local tool
  if command -v sha256sum >/dev/null 2>&1; then tool="sha256sum"
  elif command -v shasum >/dev/null 2>&1; then tool="shasum -a 256"
  else echo "  (sin sha256sum/shasum; salteo la verificación)"; return 0; fi

  local got
  got="$($tool "$file" | awk '{print $1}')"
  if [ "$want" != "$got" ]; then
    echo "ERROR: checksum no coincide para $(basename "$file")" >&2
    exit 1
  fi
  echo "  checksum OK"
}

# resolve_key deja la clave de acceso en KEY: primero $RETOIA_API_KEY, si no la pide
# por la terminal (funciona bajo `curl | bash` leyendo de /dev/tty).
resolve_key() {
  KEY="${RETOIA_API_KEY:-}"
  KEY="$(printf '%s' "$KEY" | tr -d '[:space:]')"
  if [ -z "$KEY" ]; then
    if [ -r /dev/tty ] && [ -w /dev/tty ]; then
      printf 'Pegá tu clave de acceso: ' >&2
      read -r KEY < /dev/tty || KEY=""
      KEY="$(printf '%s' "$KEY" | tr -d '[:space:]')"
    fi
  fi
  [ -n "$KEY" ] || {
    echo "Necesito tu clave de acceso: definí RETOIA_API_KEY o corré el instalador de forma interactiva." >&2
    exit 1
  }
}

# manifest_field imprime un campo ($2) de la plataforma $PLAT del manifest JSON ($1).
manifest_field() {
  local json="$1" field="$2"
  if command -v python3 >/dev/null 2>&1; then
    printf '%s' "$json" | python3 -c '
import json, sys
try:
    d = json.load(sys.stdin)
    print(d["platforms"][sys.argv[1]].get(sys.argv[2], ""))
except Exception:
    pass
' "$PLAT" "$field" 2>/dev/null || true
  else
    # Fallback sin python3: asume el JSON compacto/pretty de la plataforma.
    printf '%s' "$json" | tr -d '\n' \
      | sed -n "s/.*\"$PLAT\"[[:space:]]*:[[:space:]]*{\([^}]*\)}.*/\1/p" \
      | sed -n "s/.*\"$field\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p"
  fi
}

download_binary() {
  detect_platform
  resolve_key

  local base="${RETOIA_UPDATE_BASE:-https://cli.retoia.app}"
  base="${base%/}"

  if [ -n "$VERSION" ]; then
    echo "Nota: la descarga por el gateway solo ofrece la última versión; ignoro --version $VERSION (usá --build para una versión concreta)." >&2
  fi

  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN

  echo "Consultando $base ..."
  local code manifest
  code="$(curl -sS -o "$tmp/manifest.json" -w '%{http_code}' -H "Authorization: Bearer $KEY" "$base/retoia/manifest.json" 2>/dev/null || true)"
  case "$code" in
    200) manifest="$(cat "$tmp/manifest.json")" ;;
    401) echo "ERROR: tu clave de acceso fue rechazada (401) — revisá que sea la correcta." >&2; exit 1 ;;
    000|"") echo "ERROR: no pude contactar $base" >&2; exit 1 ;;
    *) echo "ERROR: $base respondió HTTP $code al pedir el manifest." >&2; exit 1 ;;
  esac

  local want
  want="$(manifest_field "$manifest" sha256)"

  echo "Bajando $ASSET ..."
  curl -fsSL -H "Authorization: Bearer $KEY" "$base/retoia/dl/$ASSET" -o "$tmp/$ASSET" || {
    echo "ERROR: no pude bajar $ASSET desde $base (¿existe para $PLAT?)." >&2
    exit 1
  }

  [ -f "$tmp/$ASSET" ] || { echo "No se descargó $ASSET." >&2; exit 1; }
  verify_sha256 "$tmp/$ASSET" "$want"

  tar -xzf "$tmp/$ASSET" -C "$tmp"
  # The tarball wraps its contents in a retoia/ folder.
  local binsrc="$tmp/$BIN/$BIN"
  [ -f "$binsrc" ] || binsrc="$tmp/$BIN"
  [ -f "$binsrc" ] || { echo "El archivo no contiene el binario $BIN." >&2; exit 1; }

  mkdir -p "$DEST_DIR"
  install -m 0755 "$binsrc" "$DEST"
  echo "Instalado: $DEST"
  clear_quarantine
  post_install
}

# clear_quarantine removes macOS's Gatekeeper quarantine flag from the freshly
# installed binary, so it is not blocked as "from an unidentified developer".
# It is a no-op off macOS and when xattr is unavailable.
clear_quarantine() {
  [ "$(uname -s)" = "Darwin" ] || return 0
  command -v xattr >/dev/null 2>&1 || return 0
  xattr -d com.apple.quarantine "$DEST" 2>/dev/null || true
}

# ensure_go instala Go automáticamente si falta (solo para --build).
ensure_go() {
  command -v go >/dev/null 2>&1 && return 0
  echo "Go no está instalado; lo instalo automáticamente ..."
  if command -v brew >/dev/null 2>&1; then brew install go
  elif command -v apt-get >/dev/null 2>&1; then sudo apt-get update && sudo apt-get install -y golang-go
  elif command -v dnf >/dev/null 2>&1; then sudo dnf install -y golang
  elif command -v pacman >/dev/null 2>&1; then sudo pacman -S --noconfirm go
  else
    echo "No pude instalar Go (no encontré brew/apt/dnf/pacman). Instalalo desde https://go.dev/dl" >&2
    exit 1
  fi
  command -v go >/dev/null 2>&1 || { echo "Go no quedó en el PATH; abrí una shell nueva y reintentá." >&2; exit 1; }
}

build_binary() {
  [ -f "$SCRIPT_DIR/go.mod" ] || { echo "--build necesita el repo (no encuentro go.mod junto al script)." >&2; exit 1; }
  ensure_go
  echo "Compilando $BIN desde fuente ..."
  mkdir -p "$DEST_DIR"
  ( cd "$SCRIPT_DIR" && go build -o "$DEST" . )
  echo "Instalado: $DEST"
  post_install
}

# post_install limpia el binario viejo y avisa por el PATH.
post_install() {
  if [ -e "$STALE_BIN" ]; then
    rm -f "$STALE_BIN"
    echo "Se eliminó el binario viejo retoia-models: ahora es 'retoia models'."
  fi
  case ":$PATH:" in
    *":$DEST_DIR:"*) : ;;
    *)
      echo
      echo "OJO: $DEST_DIR no está en tu PATH."
      echo "Agregá esta línea a tu ~/.zshrc (o ~/.bashrc) y reabrí la terminal:"
      echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
      ;;
  esac
}

launch_tui() {
  echo
  echo "Lanzando el instalador interactivo (retoia install) ..."
  local exe="$DEST"
  command -v "$BIN" >/dev/null 2>&1 && exe="$BIN"
  # Pasamos la key por env para no pedirla dos veces; /dev/tty hace que el TUI
  # funcione aunque el script venga por `curl | bash`.
  if [ -r /dev/tty ] && [ -w /dev/tty ]; then
    RETOIA_API_KEY="${KEY:-${RETOIA_API_KEY:-}}" "$exe" install < /dev/tty
  else
    RETOIA_API_KEY="${KEY:-${RETOIA_API_KEY:-}}" "$exe" install
  fi
}

case "$MODE" in
  build)    build_binary ;;
  download) download_binary ;;
esac
launch_tui
