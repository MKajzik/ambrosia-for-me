.DEFAULT_GOAL := help
REDOCLY := npx --yes @redocly/cli@2.53.3
GOLANGCI := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2
OAPICODEGEN := go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0

# Local development database; matches the docker-compose.yml defaults.
export DATABASE_URL ?= postgres://mealplanner:mealplanner@localhost:5432/mealplanner?sslmode=disable

.PHONY: help lint-api test-backend lint-backend generate check-generated migrate run-api db-up db-down check

help: ## List available targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-16s %s\n", $$1, $$2}'

lint-api: ## Lint openapi.yaml
	$(REDOCLY) lint openapi.yaml

test-backend: ## Vet and test the Go backend (needs Docker)
	cd backend && go vet ./... && go test ./...

lint-backend: ## Run golangci-lint on the Go backend
	cd backend && $(GOLANGCI) run ./...

generate: ## Regenerate backend code from openapi.yaml
	cd backend && $(OAPICODEGEN) -config internal/api/oapi.yaml ../openapi.yaml

check-generated: generate ## Fail if the committed generated code is out of date
	git diff --exit-code -- backend/internal/api

migrate: ## Apply database migrations to DATABASE_URL
	cd backend && go run ./cmd/migrate

run-api: ## Run the API locally on :8080
	cd backend && go run ./cmd/api

db-up: ## Start local Postgres and wait until healthy
	docker compose up -d --wait postgres

db-down: ## Stop local Postgres (data is kept)
	docker compose down

check: lint-api test-backend lint-backend check-generated ## Everything CI runs, except the compose workflow
