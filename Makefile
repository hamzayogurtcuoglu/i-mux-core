# i-mux-core build tooling. See TASKS.md (C0.1).

BINARY    ?= imux-core
PKG       := github.com/hamzayogurtcuoglu/i-mux-core
VERSION   ?= 0.0.0-dev
LDFLAGS   := -s -w -X $(PKG)/internal/version.Version=$(VERSION)
FUZZTIME  ?= 10s
GO        ?= go
PKGS      ?= ./...

export CGO_ENABLED = 0

.PHONY: all build test lint fmt difftest fuzz bench ci clean

all: lint test build

build: ## Build bin/imux-core
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/imux-core

test: ## Unit tests with the race detector
	CGO_ENABLED=1 $(GO) test -race -count=1 $(PKGS)

lint: ## golangci-lint, and go vet for every system the engine runs on
	golangci-lint run ./...
	GOOS=linux $(GO) vet $(PKGS)
	GOOS=darwin $(GO) vet $(PKGS)
	GOOS=windows $(GO) vet $(PKGS)
	GOOS=windows golangci-lint run ./...

fmt: ## Format with gofumpt via golangci-lint
	golangci-lint fmt ./...

# The differential tests run scripts against real tmux and i-mux-core. They
# skip, and say so, when tmux is not installed; CI sets IMUX_DIFFTEST_REQUIRE=1
# so they never skip there.
difftest: build ## Differential tests against real tmux
	IMUX_CORE_BIN=$(CURDIR)/bin/$(BINARY) $(GO) test -count=1 -tags difftest ./internal/difftest/...

fuzz: ## Run every fuzz target for FUZZTIME (default 10s)
	FUZZTIME=$(FUZZTIME) ./scripts/fuzz-short.sh

bench: ## Benchmarks
	$(GO) test -run '^$$' -bench . -benchmem $(PKGS)

ci: lint test difftest ## What CI runs: lint, unit tests and the differential tests

clean:
	$(GO) clean
	rm -rf bin
