#!/bin/sh
# Run Terminal-Bench against magi driven by a local MLX model.
#
# What this sets up, and why each piece is here rather than in your head:
#
#   the endpoint     mlx_lm.server on 0.0.0.0 (see serve-mlx.sh) so a task container can reach it
#                    at host.docker.internal — the container's localhost is the container.
#   the binary       magi cross-compiled for both arches and served over HTTP, so each task
#                    downloads it in seconds instead of installing Go and building from source.
#   the model id     passed verbatim to `magi --model`, which passes it verbatim to the endpoint.
#
# One-off setup, if the venv is not there:
#   brew install python@3.12                       # mlx-lm >= 0.31.3 needs 3.10+; macOS ships 3.9
#   /opt/homebrew/opt/python@3.12/bin/python3.12 -m venv /tmp/magi-live/mlx
#   /tmp/magi-live/mlx/bin/pip install -U mlx-lm
#
# Usage:
#   scratchpad/bench-mlx.sh                        # one task, the smoke test
#   scratchpad/bench-mlx.sh --dataset terminal-bench-core==0.1.1   # a full run
#   MODEL=mlx-community/... scratchpad/bench-mlx.sh
set -eu

MODEL="${MODEL:-mlx-community/NVIDIA-Nemotron-3.5-Lightning-30B-A3B-4bit}"
PORT="${PORT:-8000}"
BIN_PORT="${BIN_PORT:-8077}"
REPO="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO"

command -v tb >/dev/null 2>&1 || { echo "no tb — uv tool install terminal-bench"; exit 1; }
docker info >/dev/null 2>&1 || { echo "docker is not running"; exit 1; }

# The endpoint. Started here only if nothing is answering yet, so a server you already have
# running (with its tool-call check already done) is left alone.
if ! curl -sf "http://127.0.0.1:$PORT/v1/models" >/dev/null 2>&1; then
  echo "starting the model server…"
  "$REPO/scratchpad/serve-mlx.sh" "$MODEL" "$PORT" &
  SERVER=$!
  trap 'kill $SERVER 2>/dev/null || true; kill ${BINSRV:-0} 2>/dev/null || true' EXIT INT TERM
  i=0
  until curl -sf "http://127.0.0.1:$PORT/v1/models" >/dev/null 2>&1; do
    i=$((i + 1)); [ "$i" -gt 600 ] && { echo "the model server never came up"; exit 1; }
    sleep 1
  done
fi

# The binary, for both arches a task container might be. Built from THIS working tree, so what is
# being benchmarked is what is in front of you — not whatever main happens to be.
mkdir -p /tmp/magi-serve
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o /tmp/magi-serve/magi-arm64 ./cmd/magi
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o /tmp/magi-serve/magi-amd64 ./cmd/magi
if ! curl -sf "http://127.0.0.1:$BIN_PORT/magi-arm64" -o /dev/null 2>&1; then
  (cd /tmp/magi-serve && python3 -m http.server "$BIN_PORT" >/dev/null 2>&1) &
  BINSRV=$!
fi

# Default to one task. A full dataset against a local model is hours, and the first thing worth
# knowing is whether a single task completes at all.
if [ "$#" -eq 0 ]; then
  set -- --task-id hello-world --dataset terminal-bench-core==0.1.1
fi

echo "model:    $MODEL"
echo "endpoint: http://host.docker.internal:$PORT/v1 (from the container)"
echo

MAGI_BASE_URL="http://host.docker.internal:$PORT/v1" \
MAGI_API_KEY="" \
tb run \
  --agent-import-path bench.terminalbench.magi_agent:MagiAgent \
  -m "$MODEL" \
  -k "binary_url=http://host.docker.internal:$BIN_PORT" \
  "$@"
