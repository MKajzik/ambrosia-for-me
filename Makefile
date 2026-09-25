.DEFAULT_GOAL := help
REDOCLY := npx --yes @redocly/cli@2.53.3
GOLANGCI := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2
OAPICODEGEN := go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0
SQLC := go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
OPENAPI_TS := npx --yes openapi-typescript@7.13.0
WEB_GENERATED := web/src/lib/api/schema.gen.ts
GENERATED := backend/internal/api backend/internal/store/sqlc $(WEB_GENERATED)

# The Playwright flows run against their own Compose project on their own ports, so they neither
# collide with a running dev stack nor touch its database volume.
E2E_COMPOSE := POSTGRES_PORT=55432 API_PORT=58080 WEB_PORT=53000 docker compose -p mealplanner-e2e

# Local development database; matches the docker-compose.yml defaults.
migrate run-api: export DATABASE_URL ?= postgres://mealplanner:mealplanner@localhost:5432/mealplanner?sslmode=disable

# The development signing secret is public. The API accepts it only with ALLOW_DEV_JWT_SECRET=1, and
# `make run-api` sets both so it works out of the box. Never use either anywhere else.
run-api: export JWT_SECRET ?= dev-only-secret-change-me-0123456789
run-api: export ALLOW_DEV_JWT_SECRET ?= 1

.PHONY: help lint-api test-backend lint-backend lint-web test-web e2e-web run-web generate generate-web check-generated check-generated-web migrate run-api import-usda db-up db-down check

help: ## List available targets
	@grep -E '^[a-z0-9-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-22s %s\n", $$1, $$2}'

lint-api: ## Lint openapi.yaml
	$(REDOCLY) lint openapi.yaml

test-backend: ## Vet and test the Go backend (needs Docker)
	cd backend && go vet ./... && go test ./...

lint-backend: ## Run golangci-lint on the Go backend
	cd backend && $(GOLANGCI) run ./...

# Run from web/ so openapi-typescript does not pick up the repo's redocly.yaml lint rules.
generate-web: ## Regenerate the web TypeScript API types from openapi.yaml
	cd web && $(OPENAPI_TS) ../openapi.yaml -o src/lib/api/schema.gen.ts

generate: generate-web ## Regenerate generated code (oapi-codegen and sqlc for the backend, TypeScript types for web)
	cd backend && $(OAPICODEGEN) -config internal/api/oapi.yaml ../openapi.yaml
	cd backend && $(SQLC) generate

check-generated: generate ## Fail if the committed generated code is out of date
	git add -AN -- $(GENERATED)
	git diff --exit-code -- $(GENERATED)

check-generated-web: generate-web ## Fail if the committed web API types are out of date
	git add -AN -- $(WEB_GENERATED)
	git diff --exit-code -- $(WEB_GENERATED)

web/node_modules: web/package-lock.json
	cd web && npm ci
	touch web/node_modules

lint-web: web/node_modules ## Typecheck and lint the web app
	cd web && npm run typecheck && npm run lint

test-web: web/node_modules ## Run the web unit and component tests
	cd web && npm test

e2e-web: web/node_modules ## Run the Playwright flows against the full Compose stack (needs Docker)
	$(E2E_COMPOSE) up -d --build --wait; rc=$$?; \
	if [ $$rc -eq 0 ]; then (cd web && npx playwright install chromium && E2E_BASE_URL=http://localhost:53000 npx playwright test); rc=$$?; fi; \
	$(E2E_COMPOSE) down -v; exit $$rc

run-web: web/node_modules ## Run the web app on :3000 (start the API with `make run-api` first)
	cd web && npm run dev

migrate: ## Apply database migrations to DATABASE_URL
	cd backend && go run ./cmd/migrate

run-api: ## Run the API locally on :8080
	cd backend && go run ./cmd/api

import-usda: ## Run the USDA Foundation Foods import (needs FDC_API_KEY and DATABASE_URL)
	cd backend && go run ./cmd/import-usda

db-up: ## Start local Postgres and wait until healthy
	docker compose up -d --wait postgres

db-down: ## Stop local Postgres (data is kept)
	docker compose down

check: lint-api test-backend lint-backend check-generated lint-web test-web ## Everything CI runs, except the compose workflow and the web E2E flows
