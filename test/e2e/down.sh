#!/usr/bin/env bash
# Tear down the E2E backend stack.
# Usage: ./test/e2e/down.sh
set -euo pipefail

SOCK="$(podman machine inspect --format '{{.ConnectionInfo.PodmanSocket.Path}}')"
export DOCKER_HOST="unix://${SOCK}"

cd "$(dirname "$0")"
docker compose -f compose.e2e.yml --profile gpu down
