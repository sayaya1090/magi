VERSION ?= dev
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
PKG     := github.com/sayaya1090/magi/internal/version
LDFLAGS := -s -w -X $(PKG).Version=$(VERSION) -X $(PKG).Commit=$(COMMIT) -X $(PKG).Date=$(DATE)

.PHONY: build test test-race vet fmt e2e snapshot clean

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o magi ./cmd/magi

test:
	go test ./... -skip E2E

test-race:
	go test ./... -skip E2E -race

vet:
	go vet ./...

fmt:
	gofmt -w .

# Real-model E2E against local Ollama (set MAGI_E2E_OLLAMA_MODEL to override).
e2e:
	go test -run E2E ./... -v

# Local multi-platform build via goreleaser (requires goreleaser installed).
snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -f magi
	rm -rf dist
