# photo-server — build/test for the offline photo appliance.
# Requires Go (see docs/DEV_HANDOFF.md §5.1). libvips is only needed
# from kgu.12 onward, not for this skeleton.

GO      ?= go
BIN     ?= photo-server
PKG     := ./...
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X main.version=$(VERSION)

.PHONY: build test vet fmt tidy run clean check

build: ## Build the single binary
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/photo-server

test: ## Run all tests
	$(GO) test $(PKG)

vet: ## Static checks
	$(GO) vet $(PKG)

fmt: ## Format all packages
	$(GO) fmt $(PKG)

tidy: ## Tidy go.mod/go.sum
	$(GO) mod tidy

check: vet test ## Pre-commit quality gate

run: build ## Build and run locally (data in ./data)
	./$(BIN)

clean: ## Remove build artifacts
	rm -f $(BIN)
