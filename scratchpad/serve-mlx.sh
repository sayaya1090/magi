#!/bin/sh
# Serve a local MLX model as an OpenAI-compatible endpoint, for magi and for the bench.
#
# Why this exists rather than a line in a README: the two things that go wrong here are both
# invisible until something else fails much later.
#
#   1. Bind address. mlx_lm.server defaults to 127.0.0.1, which a Terminal-Bench task container
#      cannot reach — the container's localhost is its own. It binds 0.0.0.0 here so
#      host.docker.internal:PORT works from inside a task. On a shared network that is a listener
#      anybody can reach: run it behind whatever the machine already has, or drop --host when the
#      bench is not the point.
#   2. Tool calls. magi uses tools on every turn. mlx-lm does not apply a model's tool parser for
#      you, and NVIDIA's own quick start for Nemotron names one (qwen3_coder). So this script
#      CHECKS, once, that a tool call comes back as a tool call rather than as prose — because the
#      failure mode otherwise is an agent that looks like it is thinking and never does anything.
#
# Usage:  scratchpad/serve-mlx.sh [model-id] [port]
set -eu

MODEL="${1:-mlx-community/NVIDIA-Nemotron-3.5-Lightning-30B-A3B-4bit}"
PORT="${2:-8000}"
PY=/tmp/magi-live/mlx/bin/python

command -v "$PY" >/dev/null 2>&1 || { echo "no mlx venv at $PY — see the note at the top of bench-mlx.sh"; exit 1; }

echo "serving $MODEL on 0.0.0.0:$PORT"
"$PY" -m mlx_lm server --model "$MODEL" --host 0.0.0.0 --port "$PORT" &
SERVER=$!
trap 'kill $SERVER 2>/dev/null || true' EXIT INT TERM

# Up, before anything is asked of it. A model this size takes tens of seconds to load and the
# first request during that window looks like a broken endpoint.
i=0
until curl -sf "http://127.0.0.1:$PORT/v1/models" >/dev/null 2>&1; do
  i=$((i + 1))
  [ "$i" -gt 300 ] && { echo "the server never came up"; exit 1; }
  sleep 1
done
echo "up after ${i}s"

# The one check that matters for an agent: does a tool call come back as a tool call?
answer=$(curl -s -m 180 "http://127.0.0.1:$PORT/v1/chat/completions" \
  -H 'Content-Type: application/json' \
  -d '{"model":"'"$MODEL"'","messages":[{"role":"user","content":"Read the file /tmp/x.go and say what it does."}],"tools":[{"type":"function","function":{"name":"read","description":"read a file","parameters":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}}}]}')
if printf '%s' "$answer" | grep -q '"tool_calls"'; then
  echo "tool calls: OK"
else
  echo "⚠ tool calls: the model answered in prose. magi will not get past its first step."
  echo "  what came back: $(printf '%s' "$answer" | head -c 300)"
  echo "  mlx-lm applies no tool parser; this model's own quick start names qwen3_coder."
fi

echo
echo "point magi at it:  base_url = \"http://127.0.0.1:$PORT/v1\"   model = \"$MODEL\""
echo "from a container:  http://host.docker.internal:$PORT/v1"
wait $SERVER
