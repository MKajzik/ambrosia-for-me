# Repo Scaffold Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up the monorepo foundation: repo hygiene, the initial OpenAPI contract, a minimal Go API that serves `/v1/healthz`, local Postgres via Docker Compose, path-filtered CI, and the `CLAUDE.md` / `AGENTS.md` files.

**Architecture:** One repo with `openapi.yaml` at the root as the contract, a `backend/` Go module (handler layer only for now), and root tooling (`Makefile`, `docker-compose.yml`, GitHub Actions). Web and iOS packages, their CI workflows and their `CLAUDE.md` files are added by their own plans.

**Tech Stack:** Go 1.24, Postgres 17 (Docker Compose), Redocly CLI 2.53.3 (OpenAPI lint), GitHub Actions, GNU Make.

**Spec:** `docs/superpowers/specs/2026-09-21-meal-planner-design.md` (sections 2.1, 2.3, 2.2 ops endpoints, 4.2, 7, 8, 9 step 1).

## Global Constraints

- Backend is Go + Postgres. Layers are `handler` → `service` → `store`; no SQL outside `store`.
- All routes are under `/v1`, JSON only. Everything except `auth/*` and health checks requires a Bearer token.
- `openapi.yaml` is the single source of truth and is edited by hand. API changes update the spec first.
- Errors use RFC 9457 `application/problem+json` with a stable machine-readable `code`.
- Configuration is 12-factor via environment variables; `.env.example` documents each variable; secrets are never committed.
- Tests come first for service logic and bug fixes (TDD).
- CI is path-filtered per package.
- Generated code is never hand-edited.

## Scope notes

- The spec's full endpoint list (section 4.1) is **not** written here. Each backend plan adds its endpoints to `openapi.yaml` before implementing them. This plan delivers the contract's skeleton: metadata, security scheme, `Problem` schema, and `/healthz`.
- `/readyz` needs a database connection, so it arrives with the backend plan that adds the `store` layer.
- Code-generation drift checks (spec section 2.3, step 4) arrive with the first plan that introduces a generator (`oapi-codegen`, in the backend auth plan).
- The Compose file runs only Postgres for now. The API and web services are added by their plans.
- Docker is not installed in the authoring environment, so the Compose file is validated by CI (Task 5) and by `make db-up` on a machine with Docker.

## File Structure

| File | Responsibility |
|---|---|
| `.gitignore`, `.editorconfig` | Repo hygiene for all packages |
| `Makefile` | The one entry point for build, lint, test and run commands |
| `openapi.yaml`, `redocly.yaml` | The API contract and its lint rules |
| `backend/go.mod` | Go module `github.com/InzKazik/mealplanner/backend` |
| `backend/internal/httpapi/health.go` | Router construction and the health handler |
| `backend/internal/httpapi/health_test.go` | Health handler test |
| `backend/cmd/api/main.go` | Process entry point: config, logging, server lifecycle |
| `docker-compose.yml`, `.env.example` | Local Postgres and documented configuration |
| `.github/workflows/api-contract.yml` | Lints `openapi.yaml` |
| `.github/workflows/backend.yml` | Vet, test and lint the backend |
| `.github/workflows/compose.yml` | Validates `docker-compose.yml` |
| `CLAUDE.md`, `AGENTS.md`, `backend/CLAUDE.md` | Conventions and agent guidance |

Work on a new branch: `git checkout -b feat/repo-scaffold` (from `spec/meal-planner-design` or from `master` once the spec is merged).

---

### Task 1: Repo hygiene and Makefile

**Files:**
- Create: `.gitignore`
- Create: `.editorconfig`
- Create: `Makefile`

**Interfaces:**
- Produces: `make help`, `make lint-api`, `make test-backend`, `make lint-backend`, `make run-api`, `make db-up`, `make db-down`, `make check`. Later tasks make these targets work; this task defines them all.

- [x] **Step 1: Write `.gitignore`**

```gitignore
# Secrets and local config
.env
.env.*
!.env.example

# OS / editor
.DS_Store
.idea/
.vscode/

# Go
backend/bin/
*.test
*.out
coverage.out

# Web
node_modules/
.next/
web/coverage/
web/playwright-report/
web/test-results/

# iOS
ios/build/
DerivedData/
xcuserdata/
*.xcuserstate
```

- [x] **Step 2: Write `.editorconfig`**

```ini
root = true

[*]
charset = utf-8
end_of_line = lf
insert_final_newline = true
trim_trailing_whitespace = true
indent_style = space
indent_size = 2

[*.go]
indent_style = tab

[Makefile]
indent_style = tab

[*.swift]
indent_size = 4

[*.md]
trim_trailing_whitespace = false
```

- [x] **Step 3: Write `Makefile`**

Recipe lines **must** start with a tab character, not spaces.

```makefile
.DEFAULT_GOAL := help
REDOCLY := npx --yes @redocly/cli@2.53.3

.PHONY: help lint-api test-backend lint-backend run-api db-up db-down check

help: ## List available targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-16s %s\n", $$1, $$2}'

lint-api: ## Lint openapi.yaml
	$(REDOCLY) lint openapi.yaml

test-backend: ## Vet and test the Go backend
	cd backend && go vet ./... && go test ./...

lint-backend: ## Run golangci-lint on the Go backend
	cd backend && golangci-lint run ./...

run-api: ## Run the API locally on :8080
	cd backend && go run ./cmd/api

db-up: ## Start local Postgres and wait until healthy
	docker compose up -d --wait postgres

db-down: ## Stop local Postgres (data is kept)
	docker compose down

check: lint-api test-backend lint-backend ## Everything CI runs for the current packages
```

- [x] **Step 4: Verify `make help` lists all eight targets**

Run: `make help`
Expected output (exactly these eight lines):
```
  help             List available targets
  lint-api         Lint openapi.yaml
  test-backend     Vet and test the Go backend
  lint-backend     Run golangci-lint on the Go backend
  run-api          Run the API locally on :8080
  db-up            Start local Postgres and wait until healthy
  db-down          Stop local Postgres (data is kept)
  check            Everything CI runs for the current packages
```
If you see `*** missing separator`, the recipe lines were saved with spaces instead of tabs.

- [x] **Step 5: Commit**

```bash
git add .gitignore .editorconfig Makefile
git commit -m "chore: add repo hygiene files and Makefile"
```

---

### Task 2: Initial OpenAPI contract

**Files:**
- Create: `openapi.yaml`
- Create: `redocly.yaml`

**Interfaces:**
- Consumes: `make lint-api` (Task 1).
- Produces: a valid OpenAPI 3.1 document with server base `http://localhost:8080/v1`, a `bearerAuth` security scheme applied globally, `components.schemas.Problem` (`type`, `title`, `status`, `detail?`, `code`), `components.schemas.Health` (`status: "ok"`), `components.responses.Problem` (`application/problem+json`), and operation `getHealth` at `GET /healthz` with `security: []`. Later backend plans append paths and schemas and reuse `Problem`.

- [x] **Step 1: Write `redocly.yaml` (the lint rules)**

```yaml
extends:
  - recommended
rules:
  # /healthz has no meaningful client-error response; every other operation must declare one.
  operation-4xx-response: off
  # The only server entry is the local development URL.
  no-server-example.com: off
```

- [x] **Step 2: Write a deliberately invalid `openapi.yaml` and confirm the linter catches it**

This proves the lint step can fail, so a green result later means something.

```yaml
openapi: 3.1.0
info:
  title: Meal Planner API
paths: {}
```

Run: `make lint-api`
Expected: FAIL, reporting errors such as a missing `info.version`.

- [x] **Step 3: Replace `openapi.yaml` with the real contract**

```yaml
openapi: 3.1.0
info:
  title: Meal Planner API
  version: 0.1.0
  description: Contract for the Meal Planner API. This file is the single source of truth; the backend, web and iOS clients are generated from it.
  license:
    name: Proprietary
    identifier: LicenseRef-Proprietary
servers:
  - url: http://localhost:8080/v1
    description: Local development
security:
  - bearerAuth: []
tags:
  - name: Health
    description: Liveness checks.
paths:
  /healthz:
    get:
      tags: [Health]
      operationId: getHealth
      summary: Liveness probe
      description: Returns 200 when the process is up. Does not touch the database.
      security: []
      responses:
        '200':
          description: The service is alive.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Health'
        '500':
          $ref: '#/components/responses/Problem'
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
      bearerFormat: JWT
  schemas:
    Health:
      type: object
      required: [status]
      properties:
        status:
          type: string
          enum: [ok]
    Problem:
      type: object
      description: RFC 9457 problem details with a stable machine-readable code.
      required: [type, title, status, code]
      properties:
        type:
          type: string
          format: uri-reference
        title:
          type: string
        status:
          type: integer
        detail:
          type: string
        code:
          type: string
          description: Stable identifier clients map to localized text, e.g. partner_not_linked.
  responses:
    Problem:
      description: An error occurred.
      content:
        application/problem+json:
          schema:
            $ref: '#/components/schemas/Problem'
```

- [x] **Step 4: Run the linter and confirm it passes**

Run: `make lint-api`
Expected: `openapi.yaml: validated in ...ms` and `Woohoo! Your API description is valid.` with no warnings.

- [x] **Step 5: Commit**

```bash
git add openapi.yaml redocly.yaml
git commit -m "feat(api): add initial OpenAPI contract with health endpoint"
```

---

### Task 3: Backend skeleton with health endpoint (TDD)

**Files:**
- Create: `backend/go.mod`
- Create: `backend/internal/httpapi/health_test.go`
- Create: `backend/internal/httpapi/health.go`
- Create: `backend/cmd/api/main.go`

**Interfaces:**
- Consumes: the `getHealth` operation from Task 2 (`GET /v1/healthz` → `200 {"status":"ok"}`, `Content-Type: application/json`).
- Produces: `httpapi.NewRouter() http.Handler`, which later backend plans extend with more routes and middleware. Environment variable `API_ADDR` (default `:8080`).

- [x] **Step 1: Create the module**

`backend/go.mod`:

```
module github.com/InzKazik/mealplanner/backend

go 1.24
```

- [x] **Step 2: Write the failing test**

`backend/internal/httpapi/health_test.go`:

```go
package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/httpapi"
)

func TestHealthz(t *testing.T) {
	srv := httptest.NewServer(httpapi.NewRouter())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/v1/healthz")
	if err != nil {
		t.Fatalf("GET /v1/healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("status field = %q, want ok", body["status"])
	}
}
```

- [x] **Step 3: Run the test and confirm it fails**

Run: `cd backend && go test ./internal/httpapi/ -run TestHealthz -v`
Expected: FAIL to compile with an error like `undefined: httpapi.NewRouter` (or "no non-test Go files").

- [x] **Step 4: Write the minimal implementation**

`backend/internal/httpapi/health.go`:

```go
// Package httpapi holds the HTTP layer: routing, request decoding and response encoding.
package httpapi

import (
	"encoding/json"
	"net/http"
)

// NewRouter returns the root handler with all /v1 routes mounted.
func NewRouter() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/healthz", healthz)
	return mux
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
```

- [x] **Step 5: Run the test and confirm it passes**

Run: `cd backend && go test ./internal/httpapi/ -run TestHealthz -v`
Expected: `--- PASS: TestHealthz` and `ok`.

- [x] **Step 6: Write the process entry point**

`backend/cmd/api/main.go`:

```go
// Command api runs the Meal Planner HTTP API.
package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/InzKazik/mealplanner/backend/internal/httpapi"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	addr := os.Getenv("API_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           httpapi.NewRouter(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	logger.Info("api listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server failed", "err", err)
		os.Exit(1)
	}
}
```

- [x] **Step 7: Verify vet, tests and lint via the Makefile**

Run: `make test-backend lint-backend`
Expected: `ok  github.com/InzKazik/mealplanner/backend/internal/httpapi`, no vet output, and `golangci-lint` exits 0 with no output. (`golangci-lint` v1.x is what CI pins, see Task 5. If it is not installed locally, run `go run github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8 run ./...` from `backend/` instead.)

- [x] **Step 8: Smoke-test the running server**

Run in one terminal: `make run-api`
Run in another: `curl -si http://localhost:8080/v1/healthz`
Expected: `HTTP/1.1 200 OK`, `Content-Type: application/json`, body `{"status":"ok"}`. Stop the server with Ctrl+C.

- [x] **Step 9: Commit**

```bash
git add backend
git commit -m "feat(backend): add API skeleton with /v1/healthz"
```

---

### Task 4: Local Postgres via Docker Compose

**Files:**
- Create: `docker-compose.yml`
- Create: `.env.example`

**Interfaces:**
- Consumes: `make db-up` / `make db-down` (Task 1).
- Produces: a Postgres 17 service named `postgres`, reachable at `localhost:${POSTGRES_PORT}` (default 5432), database `mealplanner`, user `mealplanner`, with a healthcheck so `docker compose up --wait` blocks until ready. Environment variables `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`, `POSTGRES_PORT`, `API_ADDR`.

- [x] **Step 1: Write `.env.example`**

Every variable here is read by something in the repo. Later plans add variables (for example `DATABASE_URL`, `JWT_SECRET`) when code first reads them.

```dotenv
# Copy to .env and adjust. .env is git-ignored; never commit secrets.

# Local Postgres (docker-compose.yml)
POSTGRES_USER=mealplanner
POSTGRES_PASSWORD=mealplanner
POSTGRES_DB=mealplanner
POSTGRES_PORT=5432

# API (backend/cmd/api)
API_ADDR=:8080
```

- [x] **Step 2: Write `docker-compose.yml`**

```yaml
services:
  postgres:
    image: postgres:17-alpine
    environment:
      POSTGRES_USER: ${POSTGRES_USER:-mealplanner}
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD:-mealplanner}
      POSTGRES_DB: ${POSTGRES_DB:-mealplanner}
    ports:
      - "${POSTGRES_PORT:-5432}:5432"
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U $${POSTGRES_USER} -d $${POSTGRES_DB}"]
      interval: 5s
      timeout: 3s
      retries: 10

volumes:
  pgdata:
```

- [x] **Step 3: Validate the file**

Run: `docker compose config -q`
Expected: no output and exit code 0. If Docker is not installed on this machine, skip this step and Step 4; CI validates the file in Task 5.

- [x] **Step 4: Start Postgres and check it accepts connections**

Run: `cp .env.example .env && make db-up`
Expected: the `postgres` container reports `Healthy`.
Run: `docker compose exec postgres pg_isready -U mealplanner -d mealplanner`
Expected: `/var/run/postgresql:5432 - accepting connections`
Run: `make db-down`
Expected: the container stops. `.env` stays untracked (`git status` must not list it).

- [x] **Step 5: Commit**

```bash
git add docker-compose.yml .env.example
git commit -m "chore: add local Postgres via Docker Compose"
```

---

### Task 5: Path-filtered CI workflows

**Files:**
- Create: `.github/workflows/api-contract.yml`
- Create: `.github/workflows/backend.yml`
- Create: `.github/workflows/compose.yml`

**Interfaces:**
- Consumes: the commands behind `make lint-api`, `make test-backend`, `make lint-backend`, and `docker compose config` (Tasks 1–4).
- Produces: three workflows that run on pull requests and on pushes to `master`, each only when its own paths change. Web and iOS workflows are added by their plans.

- [x] **Step 1: Write `.github/workflows/api-contract.yml`**

```yaml
name: api-contract

on:
  pull_request:
    paths:
      - openapi.yaml
      - redocly.yaml
      - .github/workflows/api-contract.yml
  push:
    branches: [master]
    paths:
      - openapi.yaml
      - redocly.yaml
      - .github/workflows/api-contract.yml

jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: 22
      - run: npx --yes @redocly/cli@2.53.3 lint openapi.yaml
```

- [x] **Step 2: Write `.github/workflows/backend.yml`**

The lint step runs golangci-lint through `go run` at a pinned version so CI and local runs match.

```yaml
name: backend

on:
  pull_request:
    paths:
      - backend/**
      - .github/workflows/backend.yml
  push:
    branches: [master]
    paths:
      - backend/**
      - .github/workflows/backend.yml

defaults:
  run:
    working-directory: backend

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: backend/go.mod
      - run: go vet ./...
      - run: go test ./...
      - run: go run github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8 run ./...
```

- [x] **Step 3: Write `.github/workflows/compose.yml`**

```yaml
name: compose

on:
  pull_request:
    paths:
      - docker-compose.yml
      - .github/workflows/compose.yml
  push:
    branches: [master]
    paths:
      - docker-compose.yml
      - .github/workflows/compose.yml

jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: docker compose config -q
```

- [x] **Step 4: Check the workflow YAML parses**

Run: `python3 -c "import sys, yaml; [yaml.safe_load(open(f)) for f in sys.argv[1:]]; print('ok')" .github/workflows/*.yml`
Expected: `ok`. (If PyYAML is missing: `python3 -m pip install --user pyyaml`.)

- [x] **Step 5: Commit**

```bash
git add .github
git commit -m "ci: add path-filtered workflows for contract, backend and compose"
```

- [x] **Step 6: Confirm on GitHub (only if a remote exists)**

If `git remote -v` shows an `origin`, push the branch, open a pull request, and confirm that all three workflows run and pass. If there is no remote yet, note this in the task report; the workflows are then unverified until the repo is pushed.

---

### Task 6: CLAUDE.md and AGENTS.md

**Files:**
- Create: `CLAUDE.md`
- Create: `AGENTS.md`
- Create: `backend/CLAUDE.md`

**Interfaces:**
- Consumes: every command and path from Tasks 1–5. Every command written in these files must exist and work.
- Produces: root `CLAUDE.md` (loaded every session, kept short), `AGENTS.md` (single home for agent workflow, referenced from `CLAUDE.md`), and `backend/CLAUDE.md`. `web/CLAUDE.md` and `ios/CLAUDE.md` are created by the web and iOS plans when those packages exist.

- [x] **Step 1: Write `CLAUDE.md`**

````markdown
# Meal Planner

Meal planning product: a web app (Next.js) and a native iOS app (SwiftUI) on one Go API. Users build meals from ingredients, assemble diets (templates applied to calendar dates), see calories, macros and micronutrients, generate categorized shopping lists, and link with one partner to share meals, diets and live shopping lists.

Design spec: `docs/superpowers/specs/2026-09-21-meal-planner-design.md`. Read it before changing behaviour. Implementation plans live in `docs/superpowers/plans/`. Agent workflow and boundaries: `AGENTS.md`.

## Locked decisions

- **Backend:** Go + Postgres. Layers `handler` → `service` → `store`. No SQL outside `store`.
- **Contract:** `openapi.yaml` is the source of truth, edited by hand. Change the spec first, then the code.
- **Nutrition:** USDA FoodData Central subset imported into our DB. Per-100 g values stored as rows; meal and plan nutrition is **computed on read**, never stored.
- **Sharing:** 1:1 partnership. Partner access to meals and diets is read-only + copy; shopping lists are editable by both. Unseen resources return `404`, never `403`. Rules live in the service layer only.
- **Diets:** templates copy into per-date `plan_entries`; after applying, entries are independent of the template.
- **Web:** Next.js App Router, Tailwind, shadcn/ui. **iOS:** SwiftUI, iOS 26+, online-first with a SwiftData cache.
- **Auth:** email/password + Sign in with Apple. JWT access tokens (15 min) + rotating refresh tokens.
- **Errors:** RFC 9457 `application/problem+json` with a stable `code`.

## Repo map

- `openapi.yaml`, `redocly.yaml`: API contract and lint rules
- `backend/`: Go API (see `backend/CLAUDE.md`)
- `web/`, `ios/`: added by their own plans
- `docker-compose.yml`: local Postgres (API and web services are added later)
- `.github/workflows/`: path-filtered CI

## Commands

Run `make help` for the list. Most used:

| Command | What it does |
|---|---|
| `make check` | Everything CI runs for the current packages |
| `make lint-api` | Lint `openapi.yaml` |
| `make test-backend` | `go vet` + `go test` for the backend |
| `make lint-backend` | `golangci-lint` for the backend |
| `make run-api` | Run the API on `:8080` |
| `make db-up` / `make db-down` | Start / stop local Postgres |

Copy `.env.example` to `.env` for local configuration. Never commit `.env` or secrets.

## Conventions

- Tests first for service logic and bug fixes.
- Never hand-edit generated code. Regenerate it from `openapi.yaml`.
- Keep commits small; one logical change each.
- Any new environment variable must be added to `.env.example` in the same commit.
````

- [x] **Step 2: Write `AGENTS.md`**

````markdown
# Agent Guide

Tool-neutral guidance for any coding agent working in this repo. `CLAUDE.md` holds the project facts and commands; this file holds how to work.

## Workflow

1. **Spec first.** Non-trivial work traces to the design spec in `docs/superpowers/specs/`. If behaviour is not specified, propose a spec change before coding.
2. **Plan.** Work is executed from a plan in `docs/superpowers/plans/`, task by task.
3. **Tests first.** Write the failing test, watch it fail, write the minimum code, watch it pass. Applies to all service logic and every bug fix.
4. **Contract first.** If an API shape changes, edit `openapi.yaml` and run `make lint-api` before touching code.
5. **Verify before claiming done.** Run the commands and read the output. No "should work".
6. **Commit small.** One logical change per commit, with a clear message.

## Skills to use

| Area | Skills |
|---|---|
| Any new feature or design | `superpowers:brainstorming`, then `superpowers:writing-plans` |
| Executing a plan | `superpowers:subagent-driven-development` or `superpowers:executing-plans` |
| Any bug or failing test | `superpowers:systematic-debugging` |
| Writing code | `superpowers:test-driven-development` |
| Finishing work | `superpowers:verification-before-completion`, `superpowers:requesting-code-review` |
| Web UI | `frontend-design` |
| iOS | `apple-skills:*` (`swiftui`, `swift`, `swiftdata`, `design`, `testing`, `security`) and `apple:*` for build, test, accessibility and release |

## Boundaries

- Never hand-edit generated code (API clients, `sqlc` output). Change the source and regenerate.
- Never commit secrets, `.env` files or tokens.
- Never change the API without updating `openapi.yaml` in the same change.
- Never put SQL outside `backend/internal/store`, and never put business or sharing rules in handlers.
- Never store computed nutrition; compute it on read.
- Do not add features listed as non-goals in the spec (section 1) without an approved spec change.
- Do not force-push, rewrite shared history, or delete branches you did not create.

## Definition of done

- The behaviour matches the spec, and the spec and `openapi.yaml` match the code.
- New logic has tests that failed before the change and pass after.
- `make check` passes, and package-specific checks pass for the package touched.
- No unrelated changes in the diff.
- `CLAUDE.md` / `AGENTS.md` updated if commands, conventions or decisions changed.
````

- [x] **Step 3: Write `backend/CLAUDE.md`**

````markdown
# Backend (Go)

Module: `github.com/InzKazik/mealplanner/backend`. Go 1.24. Standard library `net/http` router for now; `chi` arrives with the first plan that needs middleware.

## Layout

- `cmd/api/`: process entry point (config, logging, server lifecycle). No business logic.
- `internal/httpapi/`: HTTP layer. Routing, decoding, encoding, error mapping. Calls services only.
- `internal/service/`: business rules (nutrition math, sharing permissions, list generation). Added by later plans.
- `internal/store/`: all SQL (`sqlc`) and migrations (`goose`). Added by later plans.

Dependencies point one way: `httpapi` → `service` → `store`. A package never imports one to its left.

## Commands (from repo root)

- `make test-backend`: `go vet ./...` and `go test ./...`
- `make lint-backend`: `golangci-lint run ./...` (CI pins v1.64.8)
- `make run-api`: run on `API_ADDR` (default `:8080`)

## Conventions

- Routes live under `/v1` and must exist in `openapi.yaml` first.
- Errors map to RFC 9457 `application/problem+json` with a stable `code`.
- Logging uses `slog` (JSON to stdout). No `fmt.Println` in non-test code.
- Tests are table-driven where there are several cases. Handler tests use `httptest`; integration tests use a real Postgres, not mocks.
- Configuration comes from environment variables and is documented in `.env.example`.
````

- [x] **Step 4: Check every command the docs mention exists**

Run: `make help`
Expected: it lists `check`, `lint-api`, `test-backend`, `lint-backend`, `run-api`, `db-up`, `db-down`, all of which `CLAUDE.md` mentions.
Run: `ls openapi.yaml redocly.yaml backend docker-compose.yml .github/workflows .env.example docs/superpowers/specs docs/superpowers/plans`
Expected: no "No such file or directory" errors.

- [x] **Step 5: Commit**

```bash
git add CLAUDE.md AGENTS.md backend/CLAUDE.md
git commit -m "docs: add CLAUDE.md, AGENTS.md and backend guidance"
```

---

### Task 7: Final verification

**Files:** none changed unless a check fails.

**Interfaces:**
- Consumes: everything from Tasks 1–6.

- [x] **Step 1: Run the full local check**

Run: `make check`
Expected: `lint-api` prints "Woohoo! Your API description is valid." with no warnings; `test-backend` shows `ok`; `lint-backend` exits 0 silently.

- [x] **Step 2: Confirm the working tree is clean and contains only intended files**

Run: `git status --short && git ls-files`
Expected: `git status --short` prints nothing. `git ls-files` lists exactly: `.editorconfig`, `.env.example`, `.github/workflows/{api-contract,backend,compose}.yml`, `.gitignore`, `AGENTS.md`, `CLAUDE.md`, `Makefile`, `backend/CLAUDE.md`, `backend/cmd/api/main.go`, `backend/go.mod`, `backend/internal/httpapi/health.go`, `backend/internal/httpapi/health_test.go`, `docker-compose.yml`, `openapi.yaml`, `redocly.yaml`, plus the spec and this plan under `docs/superpowers/`.

- [x] **Step 3: Check the contract matches the implementation**

Run: `make run-api &` then `curl -s http://localhost:8080/v1/healthz`, then stop the server.
Expected: `{"status":"ok"}`, matching the `Health` schema and the `getHealth` operation in `openapi.yaml`.

- [x] **Step 4: Report**

State what passed, and explicitly list anything that could not be verified locally (Docker Compose validation and GitHub Actions runs if there is no Docker or remote).

---

## Self-review against the spec

| Spec requirement | Covered by |
|---|---|
| 2.1 Repo layout: `CLAUDE.md`, `AGENTS.md`, `openapi.yaml`, `docker-compose.yml`, `backend/` | Tasks 1–6 (`web/`, `ios/` deferred to their plans) |
| 2.2 `/healthz` liveness | Tasks 2, 3 (`/readyz` deferred: needs the DB layer) |
| 2.2 `slog` structured logging | Task 3 (`main.go`) |
| 2.3 Contract is hand-edited source of truth; lint | Tasks 2, 5 |
| 2.3 Codegen and drift check in CI | **Deferred** to the backend auth plan, when `oapi-codegen` is introduced |
| 4.2 RFC 9457 errors with stable `code` | Task 2 (`Problem` schema) |
| 7 CI path-filtered per package | Task 5 (backend, contract, compose; web and iOS later) |
| 7 Docker Compose, `.env.example`, no committed secrets | Tasks 1, 4 |
| 8 Root `CLAUDE.md`, `AGENTS.md`, per-package `CLAUDE.md` | Task 6 (backend now; web and iOS with their plans) |
| 9 Build order step 1 | This plan |

Type and name consistency: `httpapi.NewRouter` (Tasks 3 and `backend/CLAUDE.md`), `API_ADDR` (Tasks 3, 4, 6), `getHealth` / `Health` / `Problem` (Task 2 and Task 7), make target names (Tasks 1, 5, 6) all match.
