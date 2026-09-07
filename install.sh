#!/usr/bin/env sh
# Simple installer for gpac release binaries.
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/jlcoulter/gpac/main/install.sh | sh
# Or download and run: curl -fsSL -o install.sh https://... && sh install.sh
# Optional environment variables:
#   TAG       - specific release tag to install (default: latest)
#   BIN_DIR   - directory to install into (default: $HOME/bin)

set -eu

REPO="jlcoulter/gpac"
TAG="${TAG:-}"
BIN_DIR="${BIN_DIR:-$HOME/bin}"

die() { printf "%s\n" "$1" >&2; exit 1; }

os() {
  case "$(uname -s)" in
    Linux) echo linux ;;
    Darwin) echo darwin ;;
    MINGW*|MSYS*|CYGWIN*) echo windows ;;
    *) die "unsupported OS: $(uname -s)" ;;
  esac
}

arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo amd64 ;;
    aarch64|arm64) echo arm64 ;;
    *) die "unsupported arch: $(uname -m)" ;;
  esac
}

GOOS=$(os)
GOARCH=$(arch)

if [ -z "$TAG" ]; then
  TAG=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | awk -F\" '/tag_name/{print $4; exit}') || die "failed to get latest tag"
fi

EXT="tar.gz"
if [ "$GOOS" = "windows" ]; then
  EXT="zip"
fi
ASSET="gpac_${TAG}_${GOOS}_${GOARCH}.${EXT}"
URL="https://github.com/${REPO}/releases/download/${TAG}/${ASSET}"

printf "Installing gpac %s (%s/%s) to %s\n" "$TAG" "$GOOS" "$GOARCH" "$BIN_DIR"

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT INT TERM

printf "Downloading %s...\n" "$URL"
if ! curl -fsSL "$URL" -o "$TMPDIR/$ASSET"; then
  die "download failed: $URL"
fi

mkdir -p "$BIN_DIR"
BIN_NAME="gpac"
if [ "$GOOS" = "windows" ]; then BIN_NAME="gpac.exe"; fi

if [ "$EXT" = "zip" ]; then
  if command -v unzip >/dev/null 2>&1; then
    unzip -q -o "$TMPDIR/$ASSET" -d "$TMPDIR"
  else
    die "unzip is required on windows for this installer"
  fi
else
  tar -xzf "$TMPDIR/$ASSET" -C "$TMPDIR"
fi

# After extraction, the binary may be at top-level or inside a single directory.
if [ -f "$TMPDIR/$BIN_NAME" ]; then
  mv -f "$TMPDIR/$BIN_NAME" "$BIN_DIR/$BIN_NAME"
elif [ -f "$TMPDIR"/*/"$BIN_NAME" ]; then
  mv -f "$TMPDIR"/*/"$BIN_NAME" "$BIN_DIR/$BIN_NAME"
else
  die "could not locate $BIN_NAME inside archive"
fi

chmod +x "$BIN_DIR/$BIN_NAME" || true
printf "Installed gpac %s to %s/%s\n" "$TAG" "$BIN_DIR" "$BIN_NAME"

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) printf "Warning: %s is not on your PATH\n" "$BIN_DIR" ;;
esac
