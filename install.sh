#!/usr/bin/env bash
# Instalador de caldensec. Por defecto DESCARGA el binario prearmado del último
# release de GitHub (no requiere Go): el pack de skills viaja dentro del binario.
# Lo deja en ~/.local/bin y abre el instalador interactivo (caldensec install).
#
# Uso:
#   ./install.sh                 # baja el binario del release y abre el instalador
#   ./install.sh --version vX.Y.Z  # baja una versión concreta (default: la última)
#   ./install.sh --build         # compila desde fuente (para desarrollo; requiere el repo + Go)
#
# El repo es privado, así que la descarga usa `gh` (GitHub CLI) para autenticar.
#   - Instalá gh: https://cli.github.com  y logueate: gh auth login
#   - O exportá GH_TOKEN / GITHUB_TOKEN con un token que pueda leer el release.
#
# Una línea (con gh ya autenticado):
#   curl -fsSL https://raw.githubusercontent.com/ficopix/caldensec/main/install.sh | bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

REPO="ficopix/caldensec"
BIN="caldensec"
DEST_DIR="$HOME/.local/bin"
DEST="$DEST_DIR/$BIN"
STALE_BIN="$DEST_DIR/calden-models"

MODE="download"   # download | build
VERSION=""        # vacío = último release

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
# el nombre exacto del asset: caldensec_<os>_<arch>.tar.gz
detect_platform() {
  local os arch
  case "$(uname -s)" in
    Darwin) os="mac" ;;
    Linux)  os="linux" ;;
    *) echo "SO no soportado: $(uname -s). Usá --build si tenés Go." >&2; exit 1 ;;
  esac
  case "$(uname -m)" in
    x86_64|amd64) arch="amd64" ;;
    arm64|aarch64) arch="arm64" ;;
    *) echo "Arquitectura no soportada: $(uname -m). Usá --build si tenés Go." >&2; exit 1 ;;
  esac
  ASSET="${BIN}_${os}_${arch}.tar.gz"
}

# verify_checksum comprueba el asset contra checksums.txt cuando hay herramienta
# disponible; si no hay, avisa y sigue (no bloquea la instalación).
verify_checksum() {
  local dir="$1" asset="$2"
  [ -f "$dir/checksums.txt" ] || { echo "  (sin checksums.txt; salteo la verificación)"; return 0; }

  local tool
  if command -v sha256sum >/dev/null 2>&1; then tool="sha256sum"
  elif command -v shasum >/dev/null 2>&1; then tool="shasum -a 256"
  else echo "  (sin sha256sum/shasum; salteo la verificación)"; return 0; fi

  local want got
  want="$(grep " $asset\$" "$dir/checksums.txt" | awk '{print $1}')"
  [ -n "$want" ] || { echo "  (el asset no figura en checksums.txt; salteo)"; return 0; }
  got="$( $tool "$dir/$asset" | awk '{print $1}')"
  if [ "$want" != "$got" ]; then
    echo "ERROR: checksum no coincide para $asset" >&2
    exit 1
  fi
  echo "  checksum OK"
}

download_binary() {
  command -v gh >/dev/null 2>&1 || {
    echo "Necesito 'gh' (GitHub CLI) para bajar el release de un repo privado." >&2
    echo "  Instalá: https://cli.github.com  y logueate con: gh auth login" >&2
    echo "  O corré ./install.sh --build para compilar desde fuente (requiere Go)." >&2
    exit 1
  }

  detect_platform
  local tmp
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' RETURN

  local ref="${VERSION:-último release}"
  echo "Bajando $ASSET ($ref) de $REPO ..."

  local args=(release download --repo "$REPO" --pattern "$ASSET" --pattern "checksums.txt" --dir "$tmp" --clobber)
  [ -n "$VERSION" ] && args=(release download "$VERSION" --repo "$REPO" --pattern "$ASSET" --pattern "checksums.txt" --dir "$tmp" --clobber)
  gh "${args[@]}"

  [ -f "$tmp/$ASSET" ] || { echo "No se descargó $ASSET (¿existe ese asset en el release?)." >&2; exit 1; }
  verify_checksum "$tmp" "$ASSET"

  tar -xzf "$tmp/$ASSET" -C "$tmp"
  # The tarball wraps its contents in a caldensec/ folder.
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
    echo "Se eliminó el binario viejo calden-models: ahora es 'caldensec models'."
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
  # La passphrase de instalación se pide dentro de la TUI (paso 0). Si preferís
  # no interactivo, exportá CALDENSEC_UNLOCK antes de correr esto y la TUI se
  # saltea el paso 0.
  echo
  echo "Lanzando el instalador interactivo (caldensec install) ..."
  if command -v "$BIN" >/dev/null 2>&1; then "$BIN" install; else "$DEST" install; fi
}

case "$MODE" in
  build)    build_binary ;;
  download) download_binary ;;
esac
launch_tui
