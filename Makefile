# i-mux-core build tooling. See TASKS.md (C0.1).

BINARY    ?= imux-core
PKG       := github.com/hamzayogurtcuoglu/i-mux-core
VERSION   ?= 0.0.0-dev
LDFLAGS   := -s -w -X $(PKG)/internal/version.Version=$(VERSION)
FUZZTIME  ?= 10s
GO        ?= go
PKGS      ?= ./...

export CGO_ENABLED = 0

.PHONY: all build test lint fmt tmux difftest fuzz bench ci clean

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

# The differential tests compare i-mux-core with the pinned tmux (3.6) that
# `make tmux` builds. They skip, and say so, when it has not been built; CI
# sets IMUX_DIFFTEST_REQUIRE=1 so they never skip there.
TMUX_PINNED ?= $(CURDIR)/.cache/tmux/3.6/bin/tmux

tmux: ## Build the pinned tmux 3.6 into .cache/tmux
	./scripts/build-tmux.sh

difftest: build ## Differential tests against the pinned tmux
	IMUX_CORE_BIN=$(CURDIR)/bin/$(BINARY) IMUX_DIFFTEST_TMUX=$(TMUX_PINNED) \
		$(GO) test -count=1 -tags difftest ./internal/difftest/...

fuzz: ## Run every fuzz target for FUZZTIME (default 10s)
	FUZZTIME=$(FUZZTIME) ./scripts/fuzz-short.sh

bench: ## Benchmarks
	$(GO) test -run '^$$' -bench . -benchmem $(PKGS)

ci: lint test difftest ## What CI runs: lint, unit tests and the differential tests

clean:
	$(GO) clean
	rm -rf bin
