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

# Which version, from a fixed address rather than from /releases/latest.
#
# "Latest" here means newest BY DATE across every train this repository runs — v* for the core,
# web-v* for the console, jetbrains-v* for the plugin, office-v* for the Office client. So the
# moment any of the others releases, this URL stops having the core's assets on it. Measured
# 2026-09-08, minutes after an office release: both the tarball and checksums.txt answered 404, and
# what a person running this script saw was `curl: (56) The requested URL returned error: 404` with
# nothing about which version or which train.
#
# The core release writes its own tag to badges/core-latest.txt for exactly this. One line, one
# fixed address, no API call and no JSON.
# `|| true` because of `set -e` at the top: without it a failed curl ends the script AT THE
# ASSIGNMENT, before the check below can say anything, and what the person sees is curl's exit 56
# and no sentence. Measured while writing this — the message underneath had never once printed.
ver=$(curl -fsSL --max-time 20 "https://raw.githubusercontent.com/${OWNER}/${REPO}/badges/core-latest.txt" 2>/dev/null | tr -d '[:space:]' || true)
if [ -z "$ver" ]; then
  echo "Could not read which magi version is current" >&2
  echo "  (https://raw.githubusercontent.com/${OWNER}/${REPO}/badges/core-latest.txt)" >&2
  echo "  Check your connection, or pick a version at https://github.com/${OWNER}/${REPO}/releases" >&2
  exit 1
fi
url="https://github.com/${OWNER}/${REPO}/releases/download/${ver}/${asset}"

echo "Downloading ${asset} (${ver})…"
tmp=$(mktemp -d)
if ! curl -fsSL "$url" -o "$tmp/$asset"; then
  echo "Could not download ${asset} for ${ver}" >&2
  echo "  $url" >&2
  echo "  That version may not ship this platform; the releases page lists what it has." >&2
  exit 1
fi
tar -xzf "$tmp/$asset" -C "$tmp"

echo "Installing to ${BINDIR} (may require sudo)…"
if [ -w "$BINDIR" ]; then
  mv "$tmp/magi" "$BINDIR/magi"
else
  sudo mv "$tmp/magi" "$BINDIR/magi"
fi
chmod +x "$BINDIR/magi"
rm -rf "$tmp"
echo "Installed: $("$BINDIR/magi" --version)"
