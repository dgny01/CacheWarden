SHELL := /usr/bin/env bash
.DEFAULT_GOAL := help

GO ?= go
COMPOSE ?= docker compose
BIN_DIR := $(CURDIR)/bin
GO_SOURCES := $(shell find victim aggressor tools/loadgen -type f -name '*.go' 2>/dev/null)

.PHONY: help build test race lint fmt vet run-victim run-aggressor compose-up compose-interference compose-down benchmark clean

help: ## Show available targets.
	@awk 'BEGIN {FS = ":.*## "}; /^[a-zA-Z0-9_-]+:.*## / {printf "  %-24s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build all three Go binaries.
	@mkdir -p "$(BIN_DIR)"
	cd victim && $(GO) build -trimpath -o "$(BIN_DIR)/victim" ./cmd/victim
	cd aggressor && $(GO) build -trimpath -o "$(BIN_DIR)/aggressor" ./cmd/aggressor
	cd tools/loadgen && $(GO) build -trimpath -o "$(BIN_DIR)/loadgen" ./cmd/loadgen

test: ## Run unit and integration tests in every Go module.
	cd victim && $(GO) test ./...
	cd aggressor && $(GO) test ./...
	cd tools/loadgen && $(GO) test ./...

race: ## Run all tests with the Go race detector.
	cd victim && $(GO) test -race ./...
	cd aggressor && $(GO) test -race ./...
	cd tools/loadgen && $(GO) test -race ./...

lint: ## Check Go formatting and run go vet.
	@unformatted="$$(gofmt -l $(GO_SOURCES))"; \
	if [[ -n "$${unformatted}" ]]; then \
		echo "The following files are not formatted:"; \
		echo "$${unformatted}"; \
		exit 1; \
	fi
	@$(MAKE) --no-print-directory vet

fmt: ## Format all Go source files.
	gofmt -w $(GO_SOURCES)

vet: ## Run go vet in every Go module.
	cd victim && $(GO) vet ./...
	cd aggressor && $(GO) vet ./...
	cd tools/loadgen && $(GO) vet ./...

run-victim: ## Run the victim service locally.
	cd victim && $(GO) run ./cmd/victim

run-aggressor: ## Run the aggressor locally with conservative defaults.
	cd aggressor && $(GO) run ./cmd/aggressor

compose-up: ## Build and start only the victim service.
	$(COMPOSE) up --build victim

compose-interference: ## Build and start the victim and opt-in aggressor.
	$(COMPOSE) --profile interference up --build

compose-down: ## Stop this project's Compose services.
	$(COMPOSE) --profile interference down --remove-orphans

benchmark: ## Run baseline and interference experiments.
	./scripts/benchmark.sh

clean: ## Remove build artifacts while preserving benchmark results.
	rm -rf -- "$(BIN_DIR)"

