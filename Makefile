.DEFAULT_GOAL := help
REDOCLY := npx --yes @redocly/cli@2.53.3
GOLANGCI := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2
OAPICODEGEN := go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0
SQLC := go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
GENERATED := backend/internal/api backend/internal/store/sqlc

# Local development database; matches the docker-compose.yml defaults.
migrate run-api: export DATABASE_URL ?= postgres://mealplanner:mealplanner@localhost:5432/mealplanner?sslmode=disable

# Development-only signing secret so `make run-api` works out of the box. Never use it anywhere else.
run-api: export JWT_SECRET ?= dev-only-secret-change-me-0123456789

.PHONY: help lint-api test-backend lint-backend generate check-generated migrate run-api db-up db-down check

help: ## List available targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-16s %s\n", $$1, $$2}'

lint-api: ## Lint openapi.yaml
	$(REDOCLY) lint openapi.yaml

test-backend: ## Vet and test the Go backend (needs Docker)
	cd backend && go vet ./... && go test ./...

lint-backend: ## Run golangci-lint on the Go backend
	cd backend && $(GOLANGCI) run ./...

generate: ## Regenerate backend code (oapi-codegen from openapi.yaml, sqlc from migrations and queries)
	cd backend && $(OAPICODEGEN) -config internal/api/oapi.yaml ../openapi.yaml
	cd backend && $(SQLC) generate

check-generated: generate ## Fail if the committed generated code is out of date
	git add -AN -- $(GENERATED)
	git diff --exit-code -- $(GENERATED)

migrate: ## Apply database migrations to DATABASE_URL
	cd backend && go run ./cmd/migrate

run-api: ## Run the API locally on :8080
	cd backend && go run ./cmd/api

db-up: ## Start local Postgres and wait until healthy
	docker compose up -d --wait postgres

db-down: ## Stop local Postgres (data is kept)
	docker compose down

check: lint-api test-backend lint-backend check-generated ## Everything CI runs, except the compose workflow
