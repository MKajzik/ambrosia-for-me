.DEFAULT_GOAL := help
REDOCLY := npx --yes @redocly/cli@2.53.3
GOLANGCI := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2

.PHONY: help lint-api test-backend lint-backend run-api db-up db-down check

help: ## List available targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-16s %s\n", $$1, $$2}'

lint-api: ## Lint openapi.yaml
	$(REDOCLY) lint openapi.yaml

test-backend: ## Vet and test the Go backend
	cd backend && go vet ./... && go test ./...

lint-backend: ## Run golangci-lint on the Go backend
	cd backend && $(GOLANGCI) run ./...

run-api: ## Run the API locally on :8080
	cd backend && go run ./cmd/api

db-up: ## Start local Postgres and wait until healthy
	docker compose up -d --wait postgres

db-down: ## Stop local Postgres (data is kept)
	docker compose down

check: lint-api test-backend lint-backend ## Everything CI runs that does not need Docker
