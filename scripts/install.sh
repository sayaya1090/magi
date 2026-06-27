#!/usr/bin/env bash
# Install the latest magi release for your platform.
# Usage: curl -fsSL https://raw.githubusercontent.com/sayaya1090/magi/main/scripts/install.sh | bash
set -euo pipefail

OWNER=sayaya1090
REPO=magi
BINDIR="${MAGI_BINDIR:-/usr/local/bin}"

os=$(uname -s)        # Darwin | Linux
arch=$(uname -m)      # arm64 | x86_64
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "unsupported arch: $arch" >&2; exit 1 ;;
esac

asset="magi_${os}_${arch}.tar.gz"
url="https://github.com/${OWNER}/${REPO}/releases/latest/download/${asset}"

echo "Downloading ${asset}…"
tmp=$(mktemp -d)
curl -fsSL "$url" -o "$tmp/$asset"
tar -xzf "$tmp/$asset" -C "$tmp"

echo "Installing to ${BINDIR} (may require sudo)…"
if [ -w "$BINDIR" ]; then
  mv "$tmp/magi" "$BINDIR/magi"
else
  sudo mv "$tmp/magi" "$BINDIR/magi"
fi
chmod +x "$BINDIR/magi"
rm -rf "$tmp"
echo "Installed: $($BINDIR/magi --version)"
