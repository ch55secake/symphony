.DEFAULT_GOAL := help

KURRENTDB_URL ?= kurrentdb://localhost:2113?tls=false

.PHONY: help build build-cli ui-install ui-test ui-build test vet fmt compose-validate kurrentdb-up kurrentdb-down integration-test check

help: ## Show available commands.
	@printf '%s\n' \
		'Available targets:' \
		'  build            Build the Symphony CLI and OpenTUI child.' \
		'  build-cli        Build the Symphony CLI with an existing OpenTUI child.' \
		'  ui-install       Install pinned OpenTUI dependencies.' \
		'  ui-test          Typecheck the OpenTUI application.' \
		'  ui-build         Compile the standalone OpenTUI application.' \
		'  test             Run Go tests.' \
		'  vet              Run Go vet.' \
		'  fmt              Format tracked Go source files.' \
		'  compose-validate Validate the Docker Compose configuration.' \
		'  kurrentdb-up     Start KurrentDB and wait until healthy.' \
		'  kurrentdb-down   Stop the local KurrentDB stack.' \
		'  integration-test Run KurrentDB integration tests.' \
		'  check            Run build, tests, vet, and Compose validation.'

ui-install: ## Install pinned OpenTUI dependencies.
	bun install --cwd ui --frozen-lockfile

ui-test: ui-install ## Typecheck the OpenTUI application.
	bun run --cwd ui typecheck

ui-build: ui-install ## Compile the standalone OpenTUI application.
	bun run --cwd ui build

build: ui-build ## Build the Symphony CLI.
	go build ./cmd/symphony

build-cli: ## Build the Symphony CLI with an existing OpenTUI child.
	go build ./cmd/symphony

test: ## Run Go tests.
	go test ./...

vet: ## Run Go vet.
	go vet ./...

fmt: ## Format tracked Go source files.
	gofmt -w $$(git ls-files '*.go')

compose-validate: ## Validate the Docker Compose configuration.
	docker compose config --quiet

kurrentdb-up: ## Start KurrentDB and wait until healthy.
	docker compose up -d --wait

kurrentdb-down: ## Stop the local KurrentDB stack.
	docker compose down

integration-test: ## Run KurrentDB integration tests.
	KURRENTDB_URL='$(KURRENTDB_URL)' go test ./internal/store/kurrentdb

check: ui-test build test vet compose-validate ## Run build, tests, vet, and Compose validation.
