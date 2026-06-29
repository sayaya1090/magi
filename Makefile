VERSION ?= dev
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
# code), so it is excluded from the printed total to match CI.
cover:
	go test ./... -skip E2E -covermode=atomic -coverprofile=coverage.out
	@grep -v '/internal/eval/' coverage.out > coverage.prod.out
	@go tool cover -func=coverage.prod.out | tail -1
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
