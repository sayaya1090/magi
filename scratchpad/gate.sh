#!/bin/sh
# The gate, in one place, because typing it out is how the -skip gets dropped.
#
# TestE2E and TestEvalSuite talk to a live Ollama. A bench run is usually holding that GPU, and a
# stray E2E steals it for minutes — twice in one session before this file existed. The skip is not
# optional and it is not a flag to remember: it is here.
set -e
cd "$(dirname "$0")/.."
gofmt -l ./internal ./cmd
go build ./...
GOOS=windows go build ./cmd/magi ./cmd/magi-web
GOOS=linux GOARCH=amd64 go vet ./internal/... ./cmd/...
go test ./internal/... ./cmd/... -skip 'TestE2E|TestEvalSuite' -count=1 "$@"
