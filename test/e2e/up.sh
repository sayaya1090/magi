#!/usr/bin/env bash
# Bring up the E2E backend stack using podman's docker-compatible socket.
# Usage: ./test/e2e/up.sh
set -euo pipefail

SOCK="$(podman machine inspect --format '{{.ConnectionInfo.PodmanSocket.Path}}')"
export DOCKER_HOST="unix://${SOCK}"

cd "$(dirname "$0")"

echo "Starting LiteLLM (proxying to host Ollama)..."
docker compose -f compose.e2e.yml up -d litellm

echo "Waiting for LiteLLM to become healthy on :4000 ..."
for i in $(seq 1 30); do
  if curl -sf http://localhost:4000/health/liveliness >/dev/null 2>&1 \
     || curl -sf http://localhost:4000/v1/models >/dev/null 2>&1; then
    echo "LiteLLM is up: http://localhost:4000/v1"
    exit 0
  fi
  sleep 1
done

echo "LiteLLM did not become ready in time; check: docker compose -f test/e2e/compose.e2e.yml logs litellm" >&2
exit 1
