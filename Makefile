# The tag, not a placeholder. `make build` used to stamp "dev" whatever the tree was,
# so a stale binary and a fresh one printed the same thing and only a timestamp told
# them apart. --dirty marks a build with uncommitted changes, which is exactly when
# "is this the code I just wrote" is being asked.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
PKG     := github.com/sayaya1090/magi/internal/version
LDFLAGS := -s -w -X $(PKG).Version=$(VERSION) -X $(PKG).Commit=$(COMMIT) -X $(PKG).Date=$(DATE)

.PHONY: build test test-race cover vet fmt e2e snapshot licenses clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o magi ./cmd/magi

test:
	go test ./... -skip E2E

test-race:
	go test ./... -skip E2E -race

# Test coverage: writes coverage.out, prints the total, and points at the HTML view.
# internal/eval is a manual env-gated benchmark harness (not unit-tested production
# code), so it is excluded from the printed total to match CI. covignore then drops
# the functions marked //coverage:ignore — the process entry point and the one-line
# adapters no test can say anything about — and fails if a marker has gone stale.
cover:
	go test ./... -skip E2E -covermode=atomic -coverprofile=coverage.out
	@grep -v '/internal/eval/' coverage.out > coverage.prod.out
	@go run ./tools/covignore -i coverage.prod.out -o coverage.filtered.out -bypkg coverage.bypkg.tsv && mv coverage.filtered.out coverage.prod.out
	@go tool cover -func=coverage.prod.out | tail -1
	@echo "Per-package (same filtered profile, lowest first):"
	@sort -t'	' -k4 -g coverage.bypkg.tsv | sed -E 's#github.com/sayaya1090/magi/##' | awk -F'\t' '{printf "  %6.1f%%  %s\n", $$4, $$1}'
	@echo "HTML report: go tool cover -html=coverage.out"

vet:
	go vet ./...

fmt:
	gofmt -w .

# Real-model E2E against local Ollama (set MAGI_E2E_OLLAMA_MODEL to override).
e2e:
	go test -run E2E ./... -v

# Regenerate THIRD_PARTY_LICENSES from the modules in the binary.
licenses:
	./scripts/gen_licenses.sh

# Local multi-platform build via goreleaser (requires goreleaser installed).
snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -f magi
	rm -rf dist
