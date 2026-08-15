#!/bin/sh
# The gate, in one place, because typing it out is how the -skip gets dropped.
#
# TestE2E and TestEvalSuite talk to a live Ollama. A bench run is usually holding that GPU, and a
# stray E2E steals it for minutes — twice in one session before this file existed. The skip is not
# optional and it is not a flag to remember: it is here.
set -e
cd "$(dirname "$0")/.."
# gofmt -l PRINTS the unformatted files and exits 0, so with `set -e` this line was a report
# nobody read: three pushes reached CI with a file gofmt did not like, each time after a gate that
# had just said nothing was wrong. The exit code is the whole judgement here, so it has to be made.
unformatted=$(gofmt -l ./internal ./cmd)
if [ -n "$unformatted" ]; then
  echo "not gofmt-clean:"; echo "$unformatted"; exit 1
fi
go build ./...
GOOS=windows go build ./cmd/magi ./cmd/magi-web
GOOS=linux GOARCH=amd64 go vet ./internal/... ./cmd/...
# -race matches CI's race job, so a data race or a race-only flake is caught here and not two
# pushes later. It needs cgo (default on) and roughly doubles the test wall time — worth it.
go test ./internal/... ./cmd/... -skip 'TestE2E|TestEvalSuite' -race -count=1 "$@"
