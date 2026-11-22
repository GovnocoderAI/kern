#!/usr/bin/env sh
# Kern installer for Linux. Downloads a release, verifies its checksum and
# installs the binary to ~/.local/bin (override with KERN_INSTALL_DIR).
set -eu

REPO="GovnocoderAI/kern"
BIN="kern"
INSTALL_DIR="${KERN_INSTALL_DIR:-$HOME/.local/bin}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
  linux) os=linux ;;
  *) echo "kern: unsupported OS '$os' (use WSL on Windows)" >&2; exit 1 ;;
esac

arch="$(uname -m)"
case "$arch" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) echo "kern: unsupported architecture '$arch'" >&2; exit 1 ;;
esac

version="${KERN_VERSION:-}"
if [ -z "$version" ]; then
  version="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" |
    sed -n 's/.*"tag_name": *"v\{0,1\}\([^"]*\)".*/\1/p' | head -n1)"
fi
if [ -z "$version" ]; then
  echo "kern: could not determine the latest version" >&2
  exit 1
fi

archive="${BIN}_${version}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/v${version}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "kern: downloading $base/$archive"
curl -fsSL "$base/$archive" -o "$tmp/$archive"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"

if command -v sha256sum >/dev/null 2>&1; then
  (cd "$tmp" && grep " $archive\$" checksums.txt | sha256sum -c -)
elif command -v shasum >/dev/null 2>&1; then
  (cd "$tmp" && grep " $archive\$" checksums.txt | shasum -a 256 -c -)
else
  echo "kern: warning: no sha256 tool found; skipping checksum verification" >&2
fi

tar -xzf "$tmp/$archive" -C "$tmp"
mkdir -p "$INSTALL_DIR"
install -m 0755 "$tmp/$BIN" "$INSTALL_DIR/$BIN"
echo "kern: installed $BIN $version to $INSTALL_DIR/$BIN"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "kern: add $INSTALL_DIR to PATH, then run 'kern doctor'." ;;
esac
