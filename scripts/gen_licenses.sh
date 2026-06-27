#!/usr/bin/env bash
# Regenerate THIRD_PARTY_LICENSES from the modules compiled into the binary.
# Run from the repo root (or via `make licenses`).
set -euo pipefail
cd "$(dirname "$0")/.."
out=THIRD_PARTY_LICENSES

cat > "$out" <<'HDR'
magi — Third-Party Licenses

magi itself is licensed under Apache-2.0 (see LICENSE). Binary releases (e.g. via
goreleaser) statically embed the third-party Go modules listed below, each under
its own license, reproduced here to satisfy their attribution requirements.
Source distributions resolve these modules via go.mod and do not vendor them.

Regenerate with: make licenses   (from: go list -deps ./cmd/magi)

HDR

go list -deps -f '{{with .Module}}{{.Path}}|{{.Version}}|{{.Dir}}{{end}}' ./cmd/magi 2>/dev/null \
  | grep -v '^github.com/sayaya1090/magi' \
  | sed '/^[[:space:]]*$/d' \
  | sort -u \
  | while IFS='|' read -r path ver dir; do
      [ -z "$path" ] && continue
      lic=$(find "$dir" -maxdepth 1 -type f \
              \( -iname 'license*' -o -iname 'copying*' -o -iname 'notice*' \) 2>/dev/null \
            | sort | head -n1)
      {
        echo "================================================================================"
        echo "$path  ($ver)"
        echo "================================================================================"
        if [ -n "$lic" ]; then
          cat "$lic"
        else
          echo "[License file not bundled in the module; see https://$path]"
        fi
        echo
        echo
      } >> "$out"
    done

echo "wrote $out ($(grep -c '^====' "$out" | awk '{print $1/2}') modules)"
