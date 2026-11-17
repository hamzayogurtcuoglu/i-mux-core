# i-mux-core build tooling. See TASKS.md (C0.1).

BINARY    ?= imux-core
PKG       := github.com/hamzayogurtcuoglu/i-mux-core
VERSION   ?= 0.0.0-dev
LDFLAGS   := -s -w -X $(PKG)/internal/version.Version=$(VERSION)
FUZZTIME  ?= 10s
GO        ?= go
PKGS      ?= ./...

export CGO_ENABLED = 0

.PHONY: all build test lint fmt tmux difftest e2e vt-fixtures vt-fixtures-check layout-fixtures layout-fixtures-check parse-fixtures parse-fixtures-check term-fixtures term-fixtures-check render-fixtures render-fixtures-check contract contract-record fuzz bench ci clean

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
	for os in linux darwin windows; do GOOS=$$os $(GO) vet ./testdata/faketui || exit 1; done
	GOOS=linux golangci-lint run ./...
	GOOS=windows golangci-lint run ./...

fmt: ## Format with gofumpt via golangci-lint
	golangci-lint fmt ./...

# The differential tests compare i-mux-core with the pinned tmux (3.8) that
# `make tmux` builds. They skip, and say so, when it has not been built; CI
# sets IMUX_DIFFTEST_REQUIRE=1 so they never skip there.
TMUX_PINNED ?= $(CURDIR)/.cache/tmux/3.8/bin/tmux

tmux: ## Build the pinned tmux 3.8 into .cache/tmux
	./scripts/build-tmux.sh

difftest: build vt-fixtures-check layout-fixtures-check parse-fixtures-check term-fixtures-check render-fixtures-check ## Differential tests against the pinned tmux
	IMUX_CORE_BIN=$(CURDIR)/bin/$(BINARY) IMUX_DIFFTEST_TMUX=$(TMUX_PINNED) \
		$(GO) test -count=1 -tags difftest ./internal/difftest/...

e2e: build ## The engine's end-to-end scenarios (e2e/), against imux-core alone
	IMUX_CORE_BIN=$(CURDIR)/bin/$(BINARY) $(GO) test -count=1 -tags e2e ./e2e/...

# The VT fixtures' expected screens come from the pinned tmux (cmd/vtfixture).
vt-fixtures: ## Regenerate testdata/vt/*.screen from the pinned tmux
	$(GO) build -o bin/vtfixture ./cmd/vtfixture
	./bin/vtfixture -tmux $(TMUX_PINNED)

vt-fixtures-check: ## Check that the pinned tmux still gives testdata/vt/*.screen
	@if [ -x "$(TMUX_PINNED)" ]; then \
		$(GO) build -o bin/vtfixture ./cmd/vtfixture && ./bin/vtfixture -check -tmux $(TMUX_PINNED); \
	elif [ "$$IMUX_DIFFTEST_REQUIRE" = 1 ]; then \
		echo "vt-fixtures-check: the pinned tmux is not built"; exit 1; \
	else \
		echo "vt-fixtures-check: the pinned tmux is not built (make tmux): fixtures NOT checked"; \
	fi

# The layout scenarios' expected steps come from the pinned tmux (cmd/layoutfixture).
layout-fixtures: ## Regenerate testdata/layout/*.golden from the pinned tmux
	$(GO) build -o bin/layoutfixture ./cmd/layoutfixture
	./bin/layoutfixture -tmux $(TMUX_PINNED)

layout-fixtures-check: ## Check that the pinned tmux still gives testdata/layout/*.golden
	@if [ -x "$(TMUX_PINNED)" ]; then \
		$(GO) build -o bin/layoutfixture ./cmd/layoutfixture && ./bin/layoutfixture -check -tmux $(TMUX_PINNED); \
	elif [ "$$IMUX_DIFFTEST_REQUIRE" = 1 ]; then \
		echo "layout-fixtures-check: the pinned tmux is not built"; exit 1; \
	else \
		echo "layout-fixtures-check: the pinned tmux is not built (make tmux): fixtures NOT checked"; \
	fi

# How the pinned tmux parses testdata/cmdlang/*.conf (cmd/parsefixture, source-file -nv).
parse-fixtures: ## Regenerate testdata/cmdlang/*.golden from the pinned tmux
	$(GO) build -o bin/parsefixture ./cmd/parsefixture
	./bin/parsefixture -tmux $(TMUX_PINNED)

parse-fixtures-check: ## Check that the pinned tmux still gives testdata/cmdlang/*.golden
	@if [ -x "$(TMUX_PINNED)" ]; then \
		$(GO) build -o bin/parsefixture ./cmd/parsefixture && ./bin/parsefixture -check -tmux $(TMUX_PINNED); \
	elif [ "$$IMUX_DIFFTEST_REQUIRE" = 1 ]; then \
		echo "parse-fixtures-check: the pinned tmux is not built"; exit 1; \
	else \
		echo "parse-fixtures-check: the pinned tmux is not built (make tmux): fixtures NOT checked"; \
	fi

# How the pinned tmux describes client terminals (cmd/termfixture, show-messages -T).
term-fixtures: ## Regenerate testdata/term/*.json from the pinned tmux
	$(GO) build -o bin/termfixture ./cmd/termfixture
	./bin/termfixture -tmux $(TMUX_PINNED)

term-fixtures-check: ## Check that the pinned tmux still gives testdata/term/*.json
	@if [ -x "$(TMUX_PINNED)" ]; then \
		$(GO) build -o bin/termfixture ./cmd/termfixture && ./bin/termfixture -check -tmux $(TMUX_PINNED); \
	elif [ "$$IMUX_DIFFTEST_REQUIRE" = 1 ]; then \
		echo "term-fixtures-check: the pinned tmux is not built"; exit 1; \
	else \
		echo "term-fixtures-check: the pinned tmux is not built (make tmux): fixtures NOT checked"; \
	fi

# What a client terminal shows on the pinned tmux (cmd/renderfixture).
render-fixtures: ## Regenerate testdata/render/*.screen from the pinned tmux
	$(GO) build -o bin/renderfixture ./cmd/renderfixture
	./bin/renderfixture -tmux $(TMUX_PINNED)

render-fixtures-check: ## Check that the pinned tmux still draws testdata/render/*.screen
	@if [ -x "$(TMUX_PINNED)" ]; then \
		$(GO) build -o bin/renderfixture ./cmd/renderfixture && ./bin/renderfixture -check -tmux $(TMUX_PINNED); \
	elif [ "$$IMUX_DIFFTEST_REQUIRE" = 1 ]; then \
		echo "render-fixtures-check: the pinned tmux is not built"; exit 1; \
	else \
		echo "render-fixtures-check: the pinned tmux is not built (make tmux): fixtures NOT checked"; \
	fi

# The contract (TASKS.md C0.3): contract-record runs i-mux's e2e suite with a
# recording tmux and writes contract/recorded.json (slow, needs ../i-mux and
# its e2e prerequisites); contract compares it with contract/expected.json.
contract: ## Report what i-mux uses that the contract lacks (and what it lists unused)
	$(GO) run ./cmd/contract check

contract-record: ## Record the contract from i-mux's e2e suite (slow)
	./scripts/contract.sh

fuzz: ## Run every fuzz target for FUZZTIME (default 10s)
	FUZZTIME=$(FUZZTIME) ./scripts/fuzz-short.sh

bench: ## Benchmarks
	$(GO) test -run '^$$' -bench . -benchmem $(PKGS)

ci: lint test difftest e2e ## What CI runs: lint, unit tests, the differential tests and the end-to-end scenarios

clean:
	$(GO) clean
	rm -rf bin
