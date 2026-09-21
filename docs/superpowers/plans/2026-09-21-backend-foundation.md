# Backend Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the health-check skeleton into a production-shaped backend foundation: current toolchain, an OpenAPI-generated chi server, RFC 9457 errors, request-ID/logging/recovery/CORS middleware, Postgres with goose migrations, `/healthz` + `/readyz`, graceful shutdown, and CI that enforces generated code and runs real-Postgres tests.

**Architecture:** `openapi.yaml` (OpenAPI 3.0.3) is the contract; `oapi-codegen` generates `api.ServerInterface` and models into `backend/internal/api`, and the `httpapi` package implements it behind a chi router with shared middleware. `internal/db` owns the pgx pool and the embedded goose migrations; migrations are applied by a separate `cmd/migrate` command, never by the API. Integration tests start a real Postgres with testcontainers and validate every response against the OpenAPI document.

**Tech Stack:** Go 1.26, chi v5.3.2, go-chi/cors v1.2.2, pgx v5.11.0, goose v3.28.0, oapi-codegen v2.8.0, kin-openapi v0.149.0 (contract tests), testcontainers-go v0.44.0, golangci-lint v2.13.2, Redocly CLI 2.53.3, Postgres 17.

**Spec:** `docs/superpowers/specs/2026-09-21-meal-planner-design.md` (sections 2.2, 2.3, 4.2, 6, 7, 8). Builds on `docs/superpowers/plans/2026-09-21-repo-scaffold.md` (already merged). The next plan (backend auth) adds users, tokens and the auth endpoints on top of this foundation.

## Global Constraints

- Backend is Go + Postgres. Layers are `handler` → `service` → `store`; no SQL outside `store`.
- All routes are under `/v1`, JSON only. Everything except `auth/*` and health checks requires a Bearer token.
- `openapi.yaml` is the single source of truth and is edited by hand. API changes update the spec first.
- Never hand-edit generated code. The `oapi-codegen` output is committed and CI fails if it drifts.
- Errors use RFC 9457 `application/problem+json` with a stable machine-readable `code`.
- Configuration is 12-factor via environment variables; `.env.example` documents each variable; secrets are never committed.
- Tests come first for service logic and bug fixes (TDD). Integration tests run against a real Postgres via `testcontainers`; no database mocks.
- Migrations use `goose`, are forward-only in production, and are applied at deploy before the new API version starts (never by the API process itself).
- Logging uses `slog` (JSON to stdout) with request IDs propagated through the request context.
- CI is path-filtered per package.
- Every new environment variable is added to `.env.example` in the same commit.

## Decisions this plan makes (deviations from earlier text)

1. **Go 1.26 and golangci-lint v2.13.2** replace Go 1.24 and golangci-lint v1.64.8. Go supports only the two newest major versions (1.26 and 1.27 today), so 1.24 is end-of-life, and the current releases of pgx, testcontainers, oapi-codegen and their dependencies require Go 1.25 or newer. golangci-lint v1 cannot lint a module targeting a newer Go than the one it was built with, so v2 is required.
2. **OpenAPI 3.0.3** replaces 3.1.0. `oapi-codegen` does not support 3.1 (it warns and some features silently break), and the auth plan needs nullable fields (`nullable: true`), which are 3.0 syntax. `openapi-typescript` and Swift OpenAPI Generator both support 3.0.
3. **No license block in `openapi.yaml`**, and the `info-license` lint rule is turned off (proprietary API; OpenAPI 3.0 requires a license URL if the block exists).

## Scope notes

- Not in this plan (auth plan): users, refresh tokens, JWT, Sign in with Apple, auth middleware, rate limiting, `sqlc` and the `store`/`service` layers. This plan creates only the schema-independent migration `00001_init` (extensions and the `set_updated_at()` trigger function every later table uses).
- `/readyz` is added to `openapi.yaml` in this plan (the scaffold plan deferred it until the database layer existed).
- Docker must be running for `make test-backend` and `make check`. On WSL, enable Docker Desktop's WSL integration for the distro; `docker info` must succeed.
- The first `make lint-backend` and first `make generate` download the Go 1.26 toolchain and build their tools; allow a few minutes.
- The API process never applies migrations. Locally: `make db-up`, `make migrate`, `make run-api`.

## File Structure

| File | Responsibility |
|---|---|
| `backend/go.mod`, `backend/.golangci.yml` | Go 1.26 module and golangci-lint v2 config |
| `openapi.yaml`, `redocly.yaml`, `.redocly.lint-ignore.yaml` | Contract (3.0.3, adds `/readyz`) and its lint rules |
| `backend/internal/config/config.go` | Load and validate environment configuration |
| `backend/internal/httpapi/problem.go` | RFC 9457 writer and stable problem codes |
| `backend/migrations/embed.go`, `00001_init.sql` | Embedded goose migrations |
| `backend/internal/db/db.go` | pgx pool and migration runner |
| `backend/internal/testutil/postgres.go` | Fresh database per test on a shared testcontainers Postgres |
| `backend/internal/api/oapi.yaml`, `api.gen.go` | oapi-codegen config and generated server interface and models |
| `backend/internal/httpapi/middleware.go` | Request ID, request logger, panic recovery |
| `backend/internal/httpapi/server.go` | Implements `api.ServerInterface` (`GetHealth`, `GetReady`) |
| `backend/internal/httpapi/router.go` | chi router: middleware chain, CORS, 404/405 problems, `/v1` mount |
| `backend/internal/httpapi/*_test.go` | Contract, router, middleware and problem tests |
| `backend/cmd/api/main.go` | Config, logging, listener, `serve` with graceful shutdown |
| `backend/cmd/migrate/main.go` | Apply pending migrations and exit |
| `Makefile` | `generate`, `check-generated`, `migrate` and updated lint/check targets |
| `.github/workflows/backend.yml`, `.env.example`, `CLAUDE.md`, `backend/CLAUDE.md` | CI enforcement and documentation |

Work on a new branch from `master`: `git checkout -b feat/backend-foundation`.

---

### Task 1: Toolchain upgrade (Go 1.26, golangci-lint v2)

**Files:**
- Modify: `backend/go.mod`
- Create: `backend/.golangci.yml`
- Modify: `Makefile`
- Modify: `backend/internal/httpapi/health_test.go:20`
- Modify: `backend/CLAUDE.md`

**Interfaces:**
- Produces: `make lint-backend` runs golangci-lint v2.13.2 with the standard linters plus `errorlint`; module targets Go 1.26. Later tasks rely on both.

- [ ] **Step 1: Confirm the baseline is green**

Run: `make lint-backend`
Expected: exits 0 with no output (golangci-lint v1.64.8 still configured).

- [ ] **Step 2: Bump the Go version**

Edit `backend/go.mod` so it reads exactly:

```
module github.com/InzKazik/mealplanner/backend

go 1.26
```

- [ ] **Step 3: Write the golangci-lint v2 config**

```yaml
version: "2"
linters:
  default: standard
  enable:
    - errorlint
```

- [ ] **Step 4: Point the Makefile at golangci-lint v2**

In `Makefile`, replace the line

```
GOLANGCI := go run github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8
```

with

```
GOLANGCI := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2
```

- [ ] **Step 5: Run the linter and watch it fail**

Run: `make lint-backend`
Expected: FAIL. The first run downloads the Go 1.26 toolchain and builds the linter (a few minutes). The output must contain the line `internal/httpapi/health_test.go:20:23: Error return value of resp.Body.Close is not checked (errcheck)`. This proves the v2 linter is live and stricter than v1.

- [ ] **Step 6: Fix the finding**

In `backend/internal/httpapi/health_test.go`, replace line 20

```go
	defer resp.Body.Close()
```

with

```go
	defer func() { _ = resp.Body.Close() }()
```

- [ ] **Step 7: Update the toolchain mentions in `backend/CLAUDE.md`**

In `backend/CLAUDE.md`, change `Go 1.24.` to `Go 1.26.` in the first paragraph, and replace the bullet

```
- `make lint-backend`: runs golangci-lint v1.64.8 via `go run` (the same command CI uses)
```

with

```
- `make lint-backend`: runs golangci-lint v2.13.2 via `go run` (the same command CI uses)
```

- [ ] **Step 8: Verify everything is green**

Run: `make lint-backend && make test-backend`
Expected: lint exits 0 silently; `go vet` clean; `ok  github.com/InzKazik/mealplanner/backend/internal/httpapi`.
Run: `grep '^go ' backend/go.mod`
Expected: `go 1.26`

- [ ] **Step 9: Commit**

```bash
git add backend/go.mod backend/.golangci.yml Makefile backend/internal/httpapi/health_test.go backend/CLAUDE.md
git commit -m "chore: move to Go 1.26 and golangci-lint v2"
```

---

### Task 2: OpenAPI 3.0.3 contract with `/readyz`

**Files:**
- Modify: `openapi.yaml`
- Modify: `redocly.yaml`
- Modify: `.redocly.lint-ignore.yaml`

**Interfaces:**
- Consumes: `make lint-api`.
- Produces: operations `getHealth` (`GET /healthz`) and `getReady` (`GET /readyz`, `200` `Health`, `503` `Problem`), schemas `Health` and `Problem`, response `Problem`. Task 6 generates code from this file; Task 7 implements it.

- [ ] **Step 1: Replace `redocly.yaml`**

```yaml
extends:
  - recommended
rules:
  # Every operation must declare a 4XX response; only /healthz and /readyz are exempt (see .redocly.lint-ignore.yaml).
  operation-4xx-response: error
  # The only server entry is the local development URL.
  no-server-example.com: off
  # Proprietary API: there is no public license to declare.
  info-license: off
```

- [ ] **Step 2: Replace `openapi.yaml`**

```yaml
openapi: 3.0.3
info:
  title: Meal Planner API
  version: 0.1.0
  description: Contract for the Meal Planner API. This file is the single source of truth; the backend, web and iOS clients are generated from it.
servers:
  - url: http://localhost:8080/v1
    description: Local development
security:
  - bearerAuth: []
tags:
  - name: Health
    description: Liveness and readiness checks.
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
  /readyz:
    get:
      tags: [Health]
      operationId: getReady
      summary: Readiness probe
      description: Returns 200 when the service can reach its database, 503 otherwise.
      security: []
      responses:
        '200':
          description: The service is ready to take traffic.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Health'
        '503':
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

- [ ] **Step 3: Run the linter and watch it fail**

Run: `make lint-api`
Expected: FAIL with one error from rule `operation-4xx-response` at `#/paths/~1readyz/get/responses` (`/readyz` has only 200 and 503, no 4XX). `/healthz` is already ignored by the existing ignore file. This proves the rule still enforces.

- [ ] **Step 4: Exempt `/readyz` by hand**

Never run `redocly lint --generate-ignore-file` here: it overwrites the file. Replace `.redocly.lint-ignore.yaml` with:

```yaml
# This file instructs Redocly's linter to ignore the rules contained for specific parts of your API.
# See https://redocly.com/docs/cli/ for more information.
openapi.yaml:
  operation-4xx-response:
    - '#/paths/~1healthz/get/responses'
    - '#/paths/~1readyz/get/responses'
```

- [ ] **Step 5: Run the linter and confirm it passes**

Run: `make lint-api`
Expected: `Woohoo! Your API description is valid.` and `2 problems are explicitly ignored.` with no warnings or errors.

- [ ] **Step 6: Commit**

```bash
git add openapi.yaml redocly.yaml .redocly.lint-ignore.yaml
git commit -m "feat(api): move contract to OpenAPI 3.0.3 and add /readyz"
```

---

### Task 3: Configuration loading (TDD)

**Files:**
- Create: `backend/internal/config/config_test.go`
- Create: `backend/internal/config/config.go`

**Interfaces:**
- Produces: `config.Config{Addr, DatabaseURL, WebOrigin string}` and `config.Load(getenv func(string) string) (Config, error)`. Defaults: `API_ADDR` → `:8080`, `WEB_ORIGIN` → `http://localhost:3000`; `DATABASE_URL` is required. Task 8 consumes it.

- [ ] **Step 1: Write the failing test**

```go
package config_test

import (
	"strings"
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/config"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    config.Config
		wantErr string
	}{
		{
			name: "defaults applied",
			env:  map[string]string{"DATABASE_URL": "postgres://x"},
			want: config.Config{Addr: ":8080", DatabaseURL: "postgres://x", WebOrigin: "http://localhost:3000"},
		},
		{
			name: "all set",
			env: map[string]string{
				"API_ADDR": "127.0.0.1:9000", "DATABASE_URL": "postgres://y", "WEB_ORIGIN": "https://app.example",
			},
			want: config.Config{Addr: "127.0.0.1:9000", DatabaseURL: "postgres://y", WebOrigin: "https://app.example"},
		},
		{
			name:    "missing database url",
			env:     map[string]string{},
			wantErr: "DATABASE_URL is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := config.Load(env(tt.env))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test and confirm it fails**

Run: `cd backend && go test ./internal/config/ -count=1`
Expected: FAIL to build: `no non-test Go files` or `undefined: config.Load`.

- [ ] **Step 3: Write the implementation**

```go
// Package config loads process configuration from environment variables.
package config

import (
	"errors"
	"fmt"
)

// Config is the API's runtime configuration.
type Config struct {
	// Addr is the listen address, for example ":8080".
	Addr string
	// DatabaseURL is the Postgres connection string.
	DatabaseURL string
	// WebOrigin is the single browser origin allowed by CORS.
	WebOrigin string
}

// Load reads configuration through getenv (normally os.Getenv). It returns an
// error naming every missing required variable.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		Addr:        withDefault(getenv("API_ADDR"), ":8080"),
		DatabaseURL: getenv("DATABASE_URL"),
		WebOrigin:   withDefault(getenv("WEB_ORIGIN"), "http://localhost:3000"),
	}
	var errs []error
	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if err := errors.Join(errs...); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return cfg, nil
}

func withDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
```

- [ ] **Step 4: Run the test and confirm it passes**

Run: `cd backend && go test ./internal/config/ -count=1 -v`
Expected: `--- PASS: TestLoad` with three passing subtests (`defaults_applied`, `all_set`, `missing_database_url`).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/config
git commit -m "feat(backend): add environment configuration loading"
```

---

### Task 4: RFC 9457 problem writer (TDD)

**Files:**
- Create: `backend/internal/httpapi/problem_test.go`
- Create: `backend/internal/httpapi/problem.go`

**Interfaces:**
- Produces: `httpapi.WriteProblem(w http.ResponseWriter, status int, code, detail string)` and the constants `CodeValidationFailed`, `CodeNotFound`, `CodeMethodNotAllowed`, `CodeNotReady`, `CodeInternal`. Output is `application/problem+json` with `type` (`urn:mealplanner:problem:<code>`), `title`, `status`, optional `detail`, and `code`. Tasks 7 and 8 and the auth plan use it.

- [ ] **Step 1: Write the failing test**

```go
package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/httpapi"
)

func TestWriteProblem(t *testing.T) {
	rec := httptest.NewRecorder()

	httpapi.WriteProblem(rec, http.StatusConflict, "ingredient_in_use", "used by 2 meals")

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", got)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := map[string]any{
		"type":   "urn:mealplanner:problem:ingredient_in_use",
		"title":  "Conflict",
		"status": float64(409),
		"detail": "used by 2 meals",
		"code":   "ingredient_in_use",
	}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("body[%q] = %v, want %v", k, body[k], v)
		}
	}
}

func TestWriteProblemOmitsEmptyDetail(t *testing.T) {
	rec := httptest.NewRecorder()

	httpapi.WriteProblem(rec, http.StatusNotFound, httpapi.CodeNotFound, "")

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["detail"]; ok {
		t.Errorf("detail present, want omitted: %v", body)
	}
}
```

- [ ] **Step 2: Run the test and confirm it fails**

Run: `cd backend && go test ./internal/httpapi/ -run TestWriteProblem -count=1`
Expected: FAIL to build with `undefined: httpapi.WriteProblem` and `undefined: httpapi.CodeNotFound`.

- [ ] **Step 3: Write the implementation**

```go
package httpapi

import (
	"encoding/json"
	"net/http"
)

// Stable, machine-readable problem codes. Clients map these to localized text.
const (
	CodeValidationFailed = "validation_failed"
	CodeNotFound         = "not_found"
	CodeMethodNotAllowed = "method_not_allowed"
	CodeNotReady         = "not_ready"
	CodeInternal         = "internal_error"
)

// problem is an RFC 9457 problem details document plus a stable code.
type problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
	Code   string `json:"code"`
}

// WriteProblem writes an application/problem+json response. detail may be empty.
func WriteProblem(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem{
		Type:   "urn:mealplanner:problem:" + code,
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
		Code:   code,
	})
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `cd backend && go test ./internal/httpapi/ -count=1 -v`
Expected: `--- PASS: TestWriteProblem`, `--- PASS: TestWriteProblemOmitsEmptyDetail`, and the existing `--- PASS: TestHealthz`.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/httpapi/problem.go backend/internal/httpapi/problem_test.go
git commit -m "feat(backend): add RFC 9457 problem writer"
```

---

### Task 5: Database pool, migrations and Postgres test helper (TDD, needs Docker)

**Files:**
- Modify: `backend/go.mod`, `backend/go.sum`
- Create: `backend/migrations/embed.go`
- Create: `backend/migrations/00001_init.sql`
- Create: `backend/internal/testutil/postgres.go`
- Create: `backend/internal/db/db_test.go`
- Create: `backend/internal/db/db.go`

**Interfaces:**
- Produces:
  - `db.Connect(ctx context.Context, url string) (*pgxpool.Pool, error)` (opens the pool and pings).
  - `db.Migrate(ctx context.Context, url string) (int, error)` (applies pending migrations, returns how many).
  - `testutil.NewDatabase(t *testing.T) string`, the URL of a fresh empty database on a Postgres 17 container shared by the test package. It skips the test when Docker is unavailable and drops the database on cleanup.
  - Migration `00001_init` creates extensions `citext` and `pg_trgm` and the trigger function `set_updated_at()`. Every later table's `updated_at` trigger uses it.

- [ ] **Step 1: Check Docker works**

Run: `docker info --format 'server={{.ServerVersion}}'`
Expected: prints `server=<version>`. If it prints "could not be found in this WSL 2 distro", enable Docker Desktop's WSL integration for this distro and retry. Stop and report BLOCKED if Docker cannot be made to work.

- [ ] **Step 2: Add the dependencies**

```bash
cd backend
go get github.com/jackc/pgx/v5@v5.11.0 github.com/pressly/goose/v3@v3.28.0 \
  github.com/testcontainers/testcontainers-go@v0.44.0 \
  github.com/testcontainers/testcontainers-go/modules/postgres@v0.44.0
```

- [ ] **Step 3: Write the migrations package**

`backend/migrations/embed.go`:

```go
// Package migrations embeds the goose SQL migrations.
package migrations

import "embed"

// FS holds every *.sql migration in this directory.
//
//go:embed *.sql
var FS embed.FS
```

`backend/migrations/00001_init.sql`:

```sql
-- +goose Up
CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- +goose StatementBegin
CREATE FUNCTION set_updated_at() RETURNS trigger AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION set_updated_at();
DROP EXTENSION pg_trgm;
DROP EXTENSION citext;
```

- [ ] **Step 4: Write the test helper**

```go
// Package testutil holds helpers shared by integration tests.
package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

var (
	startOnce sync.Once
	adminURL  string
	startErr  error
)

// NewDatabase returns the URL of a fresh, empty database inside a Postgres
// container shared by every test in the package. The database is dropped when
// the test ends. The test is skipped when Docker is not available.
func NewDatabase(t *testing.T) string {
	t.Helper()
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	startOnce.Do(func() {
		var ctr *tcpostgres.PostgresContainer
		ctr, startErr = tcpostgres.Run(ctx, "postgres:17-alpine",
			tcpostgres.WithDatabase("postgres"),
			tcpostgres.WithUsername("test"),
			tcpostgres.WithPassword("test"),
			tcpostgres.BasicWaitStrategies(),
		)
		if startErr != nil {
			return
		}
		// The container is removed by testcontainers' reaper when the test
		// process exits.
		adminURL, startErr = ctr.ConnectionString(ctx, "sslmode=disable")
	})
	if startErr != nil {
		t.Fatalf("start postgres container: %v", startErr)
	}

	name := "t_" + randomHex(t)
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("connect to admin database: %v", err)
	}
	defer func() { _ = admin.Close(ctx) }()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		c, err := pgx.Connect(ctx, adminURL)
		if err != nil {
			return
		}
		defer func() { _ = c.Close(ctx) }()
		_, _ = c.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})

	u, err := url.Parse(adminURL)
	if err != nil {
		t.Fatalf("parse admin url: %v", err)
	}
	u.Path = "/" + name
	return u.String()
}

func randomHex(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random: %v", err)
	}
	return hex.EncodeToString(b)
}
```

- [ ] **Step 5: Write the failing test**

```go
package db_test

import (
	"context"
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/db"
	"github.com/InzKazik/mealplanner/backend/internal/testutil"
)

func TestMigrateAppliesAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	url := testutil.NewDatabase(t)

	applied, err := db.Migrate(ctx, url)
	if err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if applied != 1 {
		t.Errorf("first Migrate applied %d migrations, want 1", applied)
	}

	again, err := db.Migrate(ctx, url)
	if err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if again != 0 {
		t.Errorf("second Migrate applied %d migrations, want 0", again)
	}

	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer pool.Close()

	var n int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM pg_extension WHERE extname IN ('citext', 'pg_trgm')`).Scan(&n)
	if err != nil {
		t.Fatalf("query extensions: %v", err)
	}
	if n != 2 {
		t.Errorf("found %d of 2 required extensions", n)
	}

	var hasFn bool
	err = pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_proc WHERE proname = 'set_updated_at')`).Scan(&hasFn)
	if err != nil {
		t.Fatalf("query function: %v", err)
	}
	if !hasFn {
		t.Error("set_updated_at() trigger function missing")
	}
}

func TestConnectFailsForUnreachableDatabase(t *testing.T) {
	_, err := db.Connect(context.Background(), "postgres://u:p@127.0.0.1:1/none?sslmode=disable&connect_timeout=1")
	if err == nil {
		t.Fatal("Connect succeeded against an unreachable database, want error")
	}
}
```

- [ ] **Step 6: Run the test and confirm it fails**

Run: `cd backend && go test ./internal/db/ -count=1`
Expected: FAIL to build with `undefined: db.Migrate` and `undefined: db.Connect` (or `no non-test Go files`).

- [ ] **Step 7: Write the implementation**

```go
// Package db opens Postgres connections and applies schema migrations.
package db

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/InzKazik/mealplanner/backend/migrations"
)

// Connect opens a pgx pool and verifies the database is reachable.
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// Migrate applies every pending migration in backend/migrations and returns
// how many it applied.
func Migrate(ctx context.Context, url string) (int, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return 0, fmt.Errorf("parse database url: %w", err)
	}
	sqlDB := stdlib.OpenDB(*cfg.ConnConfig)
	defer func() { _ = sqlDB.Close() }()

	return migrate(ctx, sqlDB)
}

func migrate(ctx context.Context, sqlDB *sql.DB) (int, error) {
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	if err != nil {
		return 0, fmt.Errorf("create migration provider: %w", err)
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return 0, fmt.Errorf("apply migrations: %w", err)
	}
	return len(results), nil
}
```

- [ ] **Step 8: Tidy and run the tests**

Run: `cd backend && go mod tidy && grep '^go ' go.mod`
Expected: `go 1.26` or `go 1.26.0`. If it shows a higher version, stop and report: a dependency raised the Go floor.
Run: `cd backend && go test ./internal/db/ -count=1 -v`
Expected: `--- PASS: TestMigrateAppliesAndIsIdempotent` (about 5 seconds; the first run pulls `postgres:17-alpine` and the testcontainers reaper image) and `--- PASS: TestConnectFailsForUnreachableDatabase`. Container start-up log lines are expected noise under `-v`.

- [ ] **Step 9: Lint and commit**

Run: `make lint-backend`
Expected: exits 0 silently.

```bash
git add backend
git commit -m "feat(backend): add pgx pool, goose migrations and Postgres test helper"
```

---

### Task 6: Code generation and drift check

**Files:**
- Modify: `backend/go.mod`, `backend/go.sum`
- Create: `backend/internal/api/oapi.yaml`
- Create: `backend/internal/api/api.gen.go` (generated)
- Modify: `Makefile`

**Interfaces:**
- Consumes: `openapi.yaml` from Task 2.
- Produces: package `api` with `api.ServerInterface { GetHealth(w, r); GetReady(w, r) }`, `api.Health{Status api.HealthStatus}`, `api.HealthStatusOk`, `api.ChiServerOptions{BaseURL string; BaseRouter chi.Router; ErrorHandlerFunc func(w http.ResponseWriter, r *http.Request, err error)}` and `api.HandlerWithOptions(si ServerInterface, options ChiServerOptions) http.Handler`. `make generate` regenerates it; `make check-generated` fails when committed output differs from the spec. Task 7 implements the interface.

- [ ] **Step 1: Write the generator config**

`backend/internal/api/oapi.yaml`:

```yaml
package: api
output: internal/api/api.gen.go
generate:
  chi-server: true
  models: true
compatibility:
  always-prefix-enum-values: true
```

`always-prefix-enum-values` makes enum constants collision-proof (`HealthStatusOk`, not `Ok`).

- [ ] **Step 2: Replace the Makefile with the version that adds `generate` and `check-generated`**

Recipe lines **must** start with a real tab character.

```makefile
.DEFAULT_GOAL := help
REDOCLY := npx --yes @redocly/cli@2.53.3
GOLANGCI := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2
OAPICODEGEN := go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0

.PHONY: help lint-api test-backend lint-backend generate check-generated run-api db-up db-down check

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

run-api: ## Run the API locally on :8080
	cd backend && go run ./cmd/api

db-up: ## Start local Postgres and wait until healthy
	docker compose up -d --wait postgres

db-down: ## Stop local Postgres (data is kept)
	docker compose down

check: lint-api test-backend lint-backend check-generated ## Everything CI runs, except the compose workflow
```

- [ ] **Step 3: Add the chi dependency and generate**

```bash
cd backend && go get github.com/go-chi/chi/v5@v5.3.2 && cd ..
make generate
```
Expected: the first run prints a `switching to go1.26.x` line (toolchain download) and creates `backend/internal/api/api.gen.go` starting with `// Code generated by github.com/oapi-codegen/oapi-codegen/v2 version v2.8.0 DO NOT EDIT.` It must contain `HealthStatusOk`, `type ServerInterface interface`, `GetHealth`, `GetReady` and `func HandlerWithOptions`.
Also expected: no warning about OpenAPI 3.1 (the contract is 3.0.3).

- [ ] **Step 4: Tidy and confirm the module builds**

Run: `cd backend && go mod tidy && go build ./... && go vet ./... && grep '^go ' go.mod`
Expected: builds and vets clean; go line is `go 1.26` or `go 1.26.0`.

- [ ] **Step 5: Commit the generated code**

```bash
git add Makefile backend
git commit -m "feat(backend): generate server interface from openapi.yaml"
```

- [ ] **Step 6: Confirm the drift check passes on committed output**

Run: `make check-generated`
Expected: exits 0 (regeneration produces no diff).

- [ ] **Step 7: Prove the drift check fails when the spec changes without regenerating**

Temporarily add a property under `components.schemas.Health.properties` in `openapi.yaml`:

```yaml
        version:
          type: string
```

Run: `make check-generated`
Expected: exits non-zero and prints a diff for `backend/internal/api/api.gen.go` (a new `Version` field).
Then restore both files: `git checkout openapi.yaml backend/internal/api/api.gen.go`
Run: `make check-generated`
Expected: exits 0 again, and `git status --short` prints nothing.

---

### Task 7: Router, middleware and handlers (TDD)

**Files:**
- Modify: `backend/go.mod`, `backend/go.sum`
- Create: `backend/internal/httpapi/contract_test.go`
- Create: `backend/internal/httpapi/router_test.go`
- Create: `backend/internal/httpapi/recoverer_test.go`
- Create: `backend/internal/httpapi/middleware.go`
- Create: `backend/internal/httpapi/server.go`
- Create: `backend/internal/httpapi/router.go`
- Delete: `backend/internal/httpapi/health.go`
- Delete: `backend/internal/httpapi/health_test.go`
- Modify: `backend/cmd/api/main.go` (minimal edit so the module keeps building; Task 8 rewrites it)

**Interfaces:**
- Consumes: `api.ServerInterface`, `api.HandlerWithOptions`, `api.ChiServerOptions`, `api.Health`, `api.HealthStatusOk` (Task 6); `WriteProblem` and the `Code*` constants (Task 4).
- Produces:
  - `httpapi.Deps{Logger *slog.Logger; Ready func(ctx context.Context) error; WebOrigin string}`.
  - `httpapi.NewRouter(d Deps) http.Handler`, replacing the old no-argument version.
  - `httpapi.RequestID(ctx context.Context) string`.
  - Every response carries `X-Request-Id`; every request is logged as one JSON line with `request_id`, `method`, `path`, `status`, `bytes`, `duration`; panics become `500` problem responses; CORS allows only `Deps.WebOrigin`; unknown paths return a `404` problem and wrong methods a `405` problem with an `Allow` header.

- [ ] **Step 1: Add the dependencies**

```bash
cd backend
go get github.com/go-chi/cors@v1.2.2 github.com/getkin/kin-openapi@v0.149.0
```

- [ ] **Step 2: Write the failing contract tests**

They send requests through the real router and fail unless request and response both conform to `openapi.yaml`.

```go
package httpapi_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/InzKazik/mealplanner/backend/internal/httpapi"
)

const specPath = "../../../openapi.yaml"

func newRouter(t *testing.T, ready func(context.Context) error) http.Handler {
	t.Helper()
	return httpapi.NewRouter(httpapi.Deps{
		Logger:    slog.New(slog.DiscardHandler),
		Ready:     ready,
		WebOrigin: "http://localhost:3000",
	})
}

func alwaysReady(context.Context) error { return nil }

// contract sends the request through handler and fails the test unless both
// the request and the response conform to openapi.yaml. It returns the response.
func contract(t *testing.T, handler http.Handler, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	ctx := context.Background()

	doc, err := openapi3.NewLoader().LoadFromFile(specPath)
	if err != nil {
		t.Fatalf("load %s: %v", specPath, err)
	}
	if err := doc.Validate(ctx); err != nil {
		t.Fatalf("openapi.yaml is not a valid OpenAPI document: %v", err)
	}
	oaRouter, err := gorillamux.NewRouter(doc)
	if err != nil {
		t.Fatalf("build spec router: %v", err)
	}

	req := httptest.NewRequest(method, "http://localhost:8080/v1"+path, nil)
	route, pathParams, err := oaRouter.FindRoute(req)
	if err != nil {
		if errors.Is(err, routers.ErrPathNotFound) {
			t.Fatalf("%s %s is not declared in openapi.yaml", method, path)
		}
		t.Fatalf("find route: %v", err)
	}
	opts := &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc}
	in := &openapi3filter.RequestValidationInput{Request: req, PathParams: pathParams, Route: route, Options: opts}
	if err := openapi3filter.ValidateRequest(ctx, in); err != nil {
		t.Fatalf("request does not match contract: %v", err)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	out := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: in,
		Status:                 rec.Code,
		Header:                 rec.Header(),
		Body:                   io.NopCloser(bytes.NewReader(rec.Body.Bytes())),
		Options:                &openapi3filter.Options{IncludeResponseStatus: true},
	}
	if err := openapi3filter.ValidateResponse(ctx, out); err != nil {
		t.Fatalf("response %d %q does not match contract: %v", rec.Code, rec.Body.String(), err)
	}
	return rec
}

func TestHealthzMatchesContract(t *testing.T) {
	rec := contract(t, newRouter(t, alwaysReady), http.MethodGet, "/healthz")

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestReadyzMatchesContract(t *testing.T) {
	tests := []struct {
		name       string
		ready      func(context.Context) error
		wantStatus int
		wantCT     string
	}{
		{"ready", alwaysReady, http.StatusOK, "application/json"},
		{
			"database down",
			func(context.Context) error { return errors.New("connection refused") },
			http.StatusServiceUnavailable,
			"application/problem+json",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := contract(t, newRouter(t, tt.ready), http.MethodGet, "/readyz")

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Content-Type"); got != tt.wantCT {
				t.Errorf("Content-Type = %q, want %q", got, tt.wantCT)
			}
		})
	}
}
```

- [ ] **Step 3: Write the failing router tests**

```go
package httpapi_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/httpapi"
)

func do(t *testing.T, h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json", got)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	return body
}

func TestUnknownPathReturnsProblem(t *testing.T) {
	rec := do(t, newRouter(t, alwaysReady), httptest.NewRequest(http.MethodGet, "/v1/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if code := decodeProblem(t, rec)["code"]; code != httpapi.CodeNotFound {
		t.Errorf("code = %v, want %s", code, httpapi.CodeNotFound)
	}
}

func TestWrongMethodReturnsProblem(t *testing.T) {
	rec := do(t, newRouter(t, alwaysReady), httptest.NewRequest(http.MethodPost, "/v1/healthz", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if code := decodeProblem(t, rec)["code"]; code != httpapi.CodeMethodNotAllowed {
		t.Errorf("code = %v, want %s", code, httpapi.CodeMethodNotAllowed)
	}
	if got := rec.Header().Get("Allow"); got != "GET" {
		t.Errorf("Allow = %q, want %q (RFC 9110 requires it on 405)", got, "GET")
	}
}

func TestRequestIDIsGeneratedAndReplacedWhenInvalid(t *testing.T) {
	h := newRouter(t, alwaysReady)
	hex32 := regexp.MustCompile(`^[0-9a-f]{32}$`)

	rec := do(t, h, httptest.NewRequest(http.MethodGet, "/v1/healthz", nil))
	if id := rec.Header().Get("X-Request-Id"); !hex32.MatchString(id) {
		t.Errorf("generated X-Request-Id = %q, want 32 hex chars", id)
	}

	bad := httptest.NewRequest(http.MethodGet, "/v1/healthz", nil)
	bad.Header.Set("X-Request-Id", "has spaces and \"quotes\"")
	if id := do(t, h, bad).Header().Get("X-Request-Id"); !hex32.MatchString(id) {
		t.Errorf("invalid inbound ID was kept: %q", id)
	}
}

func TestRequestIDIsEchoedWhenValid(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/healthz", nil)
	req.Header.Set("X-Request-Id", "trace-abc.123")

	rec := do(t, newRouter(t, alwaysReady), req)

	if id := rec.Header().Get("X-Request-Id"); id != "trace-abc.123" {
		t.Errorf("X-Request-Id = %q, want the inbound value", id)
	}
}

func TestRequestIsLoggedWithRequestID(t *testing.T) {
	var buf bytes.Buffer
	h := httpapi.NewRouter(httpapi.Deps{
		Logger:    slog.New(slog.NewJSONHandler(&buf, nil)),
		Ready:     alwaysReady,
		WebOrigin: "http://localhost:3000",
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/healthz", nil)
	req.Header.Set("X-Request-Id", "log-me")

	do(t, h, req)

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("log line is not JSON: %q (%v)", buf.String(), err)
	}
	want := map[string]any{"msg": "request", "request_id": "log-me", "method": "GET", "path": "/v1/healthz", "status": float64(200)}
	for k, v := range want {
		if line[k] != v {
			t.Errorf("log[%q] = %v, want %v", k, line[k], v)
		}
	}
}

func TestCORS(t *testing.T) {
	h := newRouter(t, alwaysReady)
	preflight := func(origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodOptions, "/v1/healthz", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", "GET")
		req.Header.Set("Access-Control-Request-Headers", "authorization")
		return do(t, h, req)
	}

	allowed := preflight("http://localhost:3000")
	if got := allowed.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Errorf("allowed origin: Allow-Origin = %q, want the origin echoed", got)
	}
	if got := allowed.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(strings.ToLower(got), "authorization") {
		t.Errorf("Allow-Headers = %q, want it to include Authorization", got)
	}

	denied := preflight("https://evil.example")
	if got := denied.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("disallowed origin got Allow-Origin = %q, want none", got)
	}
}
```

- [ ] **Step 4: Write the failing recoverer tests**

This file is in package `httpapi` (not `httpapi_test`) because it tests the unexported middleware.

```go
package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRecovererReturnsProblemAndLogsPanic(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	h := recoverer(logger)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["code"] != CodeInternal {
		t.Errorf("code = %v, want %s", body["code"], CodeInternal)
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Errorf("panic value leaked to the client: %s", rec.Body.String())
	}
	if !strings.Contains(buf.String(), "boom") || !strings.Contains(buf.String(), "stack") {
		t.Errorf("panic and stack were not logged: %s", buf.String())
	}
}

func TestRecovererRepanicsOnAbortHandler(t *testing.T) {
	h := recoverer(slog.New(slog.DiscardHandler))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	defer func() {
		if recover() != http.ErrAbortHandler { //nolint:errorlint // identity comparison per net/http docs
			t.Error("http.ErrAbortHandler was swallowed, want it re-panicked")
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}
```

- [ ] **Step 5: Run the tests and confirm they fail**

Run: `cd backend && go test ./internal/httpapi/ -count=1`
Expected: FAIL to build with errors such as `undefined: httpapi.Deps` and `undefined: recoverer`: the package does not compile against the new tests yet.

- [ ] **Step 6: Write the middleware**

```go
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

const requestIDHeader = "X-Request-Id"

type ctxKey int

const requestIDKey ctxKey = iota

// A caller-supplied request ID is only trusted when it is short and printable.
var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// RequestID returns the request ID stored in ctx, or "" when there is none.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// requestID assigns every request an ID, exposes it as X-Request-Id, and
// stores it in the request context.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if !validRequestID.MatchString(id) {
			id = newRequestID()
		}
		w.Header().Set(requestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

func newRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error on supported platforms
	return hex.EncodeToString(b)
}

// requestLogger logs one structured line per request, after it completes.
func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
			}
			logger.LogAttrs(r.Context(), slog.LevelInfo, "request",
				slog.String("request_id", RequestID(r.Context())),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", status),
				slog.Int("bytes", ww.BytesWritten()),
				slog.Duration("duration", time.Since(start)),
			)
		})
	}
}

// recoverer turns a handler panic into a 500 problem response and logs the stack.
func recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if rec == http.ErrAbortHandler { //nolint:errorlint // http.ErrAbortHandler is compared by identity per net/http docs
					panic(rec)
				}
				logger.ErrorContext(r.Context(), "panic in handler",
					slog.String("request_id", RequestID(r.Context())),
					slog.Any("panic", rec),
					slog.String("stack", string(debug.Stack())),
				)
				WriteProblem(w, http.StatusInternalServerError, CodeInternal, "")
			}()
			next.ServeHTTP(w, r)
		})
	}
}
```

- [ ] **Step 7: Write the handlers**

```go
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/InzKazik/mealplanner/backend/internal/api"
)

// server implements api.ServerInterface.
type server struct {
	logger *slog.Logger
	ready  func(context.Context) error
}

var _ api.ServerInterface = (*server)(nil)

func (s *server) GetHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, api.Health{Status: api.HealthStatusOk})
}

func (s *server) GetReady(w http.ResponseWriter, r *http.Request) {
	if err := s.ready(r.Context()); err != nil {
		s.logger.WarnContext(r.Context(), "readiness check failed",
			slog.String("request_id", RequestID(r.Context())),
			slog.Any("err", err),
		)
		WriteProblem(w, http.StatusServiceUnavailable, CodeNotReady, "database is not reachable")
		return
	}
	writeJSON(w, http.StatusOK, api.Health{Status: api.HealthStatusOk})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
```

- [ ] **Step 8: Write the router and remove the old health files**

```go
// Package httpapi holds the HTTP layer: routing, request decoding and response
// encoding. It calls services and never touches SQL.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"

	"github.com/InzKazik/mealplanner/backend/internal/api"
)

// Deps are the collaborators the router needs.
type Deps struct {
	Logger *slog.Logger
	// Ready reports whether the service can take traffic (for example the
	// database answers). It backs GET /v1/readyz.
	Ready func(ctx context.Context) error
	// WebOrigin is the single browser origin allowed by CORS.
	WebOrigin string
}

// NewRouter returns the root handler with every /v1 route and all middleware.
func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()

	r.Use(requestID)
	r.Use(requestLogger(d.Logger))
	r.Use(recoverer(d.Logger))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{d.WebOrigin},
		AllowedMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowedHeaders: []string{"Authorization", "Content-Type"},
		ExposedHeaders: []string{requestIDHeader},
		MaxAge:         300,
	}))

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		WriteProblem(w, http.StatusNotFound, CodeNotFound, "")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		if allowed := allowedMethods(r, req.URL.Path); len(allowed) > 0 {
			w.Header().Set("Allow", strings.Join(allowed, ", "))
		}
		WriteProblem(w, http.StatusMethodNotAllowed, CodeMethodNotAllowed, "")
	})

	api.HandlerWithOptions(&server{logger: d.Logger, ready: d.Ready}, api.ChiServerOptions{
		BaseURL:    "/v1",
		BaseRouter: r,
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			WriteProblem(w, http.StatusBadRequest, CodeValidationFailed, err.Error())
		},
	})
	return r
}

// allowedMethods lists the methods mux has a handler for at path, so a 405
// response can carry the Allow header RFC 9110 requires.
func allowedMethods(mux *chi.Mux, path string) []string {
	var allowed []string
	for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if mux.Match(chi.NewRouteContext(), m, path) {
			allowed = append(allowed, m)
		}
	}
	return allowed
}
```

Then delete the old skeleton, whose behaviour the contract tests now cover:

```bash
git rm backend/internal/httpapi/health.go backend/internal/httpapi/health_test.go
```

- [ ] **Step 9: Keep `cmd/api` compiling with a minimal edit**

In `backend/cmd/api/main.go`, add `"context"` to the imports (before `"log/slog"`), and replace the line

```go
		Handler:           httpapi.NewRouter(),
```

with

```go
		Handler: httpapi.NewRouter(httpapi.Deps{
			Logger:    logger,
			Ready:     func(context.Context) error { return nil },
			WebOrigin: "http://localhost:3000",
		}),
```

This is temporary. Task 8 replaces the whole file.

- [ ] **Step 10: Tidy, format and run the tests**

Run: `cd backend && go mod tidy && gofmt -l . && go vet ./... && go test ./internal/httpapi/ -count=1 -v`
Expected: `gofmt -l` prints nothing; go line still `go 1.26`/`go 1.26.0`; these pass: `TestHealthzMatchesContract`, `TestReadyzMatchesContract/ready`, `TestReadyzMatchesContract/database_down`, `TestUnknownPathReturnsProblem`, `TestWrongMethodReturnsProblem`, `TestRequestIDIsGeneratedAndReplacedWhenInvalid`, `TestRequestIDIsEchoedWhenValid`, `TestRequestIsLoggedWithRequestID`, `TestCORS`, `TestRecovererReturnsProblemAndLogsPanic`, `TestRecovererRepanicsOnAbortHandler`, `TestWriteProblem`, `TestWriteProblemOmitsEmptyDetail`.

- [ ] **Step 11: Prove the contract test can fail**

Temporarily change the body written by `GetHealth` in `backend/internal/httpapi/server.go` to `writeJSON(w, http.StatusOK, map[string]string{"state": "ok"})`.
Run: `cd backend && go test ./internal/httpapi/ -run TestHealthzMatchesContract -count=1`
Expected: FAIL with a message containing `does not match contract` and `property "status" is missing`.
Revert the temporary edit by restoring the original line, `writeJSON(w, http.StatusOK, api.Health{Status: api.HealthStatusOk})`, then re-run `cd backend && go test ./internal/httpapi/ -count=1` and confirm it is green again.

- [ ] **Step 12: Lint and commit**

Run: `make lint-backend`
Expected: exits 0 silently.

```bash
git add backend
git commit -m "feat(backend): add chi router, middleware and contract-tested health endpoints"
```

---

### Task 8: Server entry point, graceful shutdown and migrate command (TDD, needs Docker)

**Files:**
- Create: `backend/cmd/api/main_test.go`
- Modify: `backend/cmd/api/main.go` (full replacement)
- Create: `backend/cmd/migrate/main.go`
- Modify: `Makefile` (final version)

**Interfaces:**
- Consumes: `config.Load`, `db.Connect`, `db.Migrate`, `httpapi.NewRouter`/`httpapi.Deps`, `testutil.NewDatabase`.
- Produces:
  - `serve(ctx context.Context, cfg config.Config, logger *slog.Logger, ln net.Listener) error` in package `main`. It returns nil after a clean shutdown and an error if the database is unreachable.
  - Server timeouts: `ReadHeaderTimeout` 5s, `ReadTimeout` 15s, `WriteTimeout` 30s, `IdleTimeout` 60s; shutdown drain 10s; `slog.SetDefault` installed.
  - `cmd/migrate`: reads `DATABASE_URL`, applies pending migrations, logs the count, exits 0 (exit 2 if the variable is missing, 1 on failure).
  - `make migrate`; `make run-api` and `make migrate` default `DATABASE_URL` to the compose database.

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/InzKazik/mealplanner/backend/internal/config"
	"github.com/InzKazik/mealplanner/backend/internal/testutil"
)

func TestServeReportsReadyThenShutsDownCleanly(t *testing.T) {
	cfg := config.Config{DatabaseURL: testutil.NewDatabase(t), WebOrigin: "http://localhost:3000"}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- serve(ctx, cfg, slog.New(slog.DiscardHandler), ln) }()

	url := "http://" + ln.Addr().String() + "/v1/readyz"
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(url)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
			t.Fatalf("GET %s = %d, want 200", url, resp.StatusCode)
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never became reachable: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve returned %v after cancel, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return within 5s of cancellation")
	}

	if conn, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second); err == nil {
		_ = conn.Close()
		t.Error("port still accepts connections after shutdown")
	}
}

func TestServeFailsWhenDatabaseIsUnreachable(t *testing.T) {
	cfg := config.Config{
		DatabaseURL: "postgres://u:p@127.0.0.1:1/none?sslmode=disable&connect_timeout=1",
		WebOrigin:   "http://localhost:3000",
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	if err := serve(context.Background(), cfg, slog.New(slog.DiscardHandler), ln); err == nil {
		t.Fatal("serve succeeded against an unreachable database, want error")
	}
}
```

- [ ] **Step 2: Run the test and confirm it fails**

Run: `cd backend && go test ./cmd/api/ -count=1`
Expected: FAIL to build with `undefined: serve`.

- [ ] **Step 3: Replace `main.go`**

```go
// Command api runs the Meal Planner HTTP API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/InzKazik/mealplanner/backend/internal/config"
	"github.com/InzKazik/mealplanner/backend/internal/db"
	"github.com/InzKazik/mealplanner/backend/internal/httpapi"
)

const shutdownTimeout = 10 * time.Second

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "api:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Addr, err)
	}
	logger.Info("api listening", "addr", ln.Addr().String())

	return serve(ctx, cfg, logger, ln)
}

// serve runs the API on ln until ctx is cancelled, then drains in-flight
// requests for up to shutdownTimeout. It returns nil after a clean shutdown.
func serve(ctx context.Context, cfg config.Config, logger *slog.Logger, ln net.Listener) error {
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	srv := &http.Server{
		Handler: httpapi.NewRouter(httpapi.Deps{
			Logger:    logger,
			Ready:     pool.Ping,
			WebOrigin: cfg.WebOrigin,
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		return fmt.Errorf("server stopped unexpectedly: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("server: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `cd backend && go vet ./... && go test ./cmd/api/ -count=1 -v`
Expected: `--- PASS: TestServeReportsReadyThenShutsDownCleanly` and `--- PASS: TestServeFailsWhenDatabaseIsUnreachable`.

- [ ] **Step 5: Write the migrate command**

```go
// Command migrate applies pending database migrations and exits.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/InzKazik/mealplanner/backend/internal/db"
)

func main() {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		fmt.Fprintln(os.Stderr, "migrate: DATABASE_URL is required")
		os.Exit(2)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	applied, err := db.Migrate(context.Background(), url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
	logger.Info("migrations applied", "count", applied)
}
```

- [ ] **Step 6: Replace the Makefile with the final version**

Recipe lines **must** start with a real tab character.

```makefile
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
```

- [ ] **Step 7: Verify `make help`**

Run: `make help`
Expected: eleven lines: `help`, `lint-api`, `test-backend`, `lint-backend`, `generate`, `check-generated`, `migrate`, `run-api`, `db-up`, `db-down`, `check`.

- [ ] **Step 8: Smoke-test the real binary against compose Postgres**

Shell state does not persist between commands, so run the whole script below in ONE command. It needs Docker.

```bash
set -u
DB='postgres://mealplanner:mealplanner@localhost:5432/mealplanner?sslmode=disable'
make db-up
make migrate                       # expect a JSON log line with "count":1
make migrate                       # expect "count":0
(cd backend && go build -o /tmp/mealplanner-api ./cmd/api)
DATABASE_URL="$DB" /tmp/mealplanner-api > /tmp/mealplanner-api.log 2>&1 &
API_PID=$!
sleep 2
echo "--- healthz";  curl -si localhost:8080/v1/healthz
echo "--- readyz";   curl -s localhost:8080/v1/readyz
echo "--- 404";      curl -si localhost:8080/v1/nope
echo "--- 405";      curl -si -X POST localhost:8080/v1/healthz
echo "--- CORS allowed"; curl -si -X OPTIONS localhost:8080/v1/healthz -H 'Origin: http://localhost:3000' -H 'Access-Control-Request-Method: GET'
echo "--- CORS denied";  curl -si -X OPTIONS localhost:8080/v1/healthz -H 'Origin: https://evil.example' -H 'Access-Control-Request-Method: GET'
docker compose stop postgres
echo "--- readyz with the database stopped"; curl -s -w ' [%{http_code}]\n' localhost:8080/v1/readyz
kill -TERM "$API_PID"; wait "$API_PID"; echo "api exit=$?"
grep '"shutting down"' /tmp/mealplanner-api.log
echo "--- startup with the database down"
DATABASE_URL="$DB" /tmp/mealplanner-api; echo "api exit=$?"
docker compose down -v
```

Expected:
- healthz: `HTTP/1.1 200 OK`, `Content-Type: application/json`, an `X-Request-Id` header, body `{"status":"ok"}`.
- readyz: `{"status":"ok"}`.
- 404: `Content-Type: application/problem+json` and `"code":"not_found"`.
- 405: `Allow: GET` and `"code":"method_not_allowed"`.
- CORS allowed: contains `Access-Control-Allow-Origin: http://localhost:3000`. CORS denied: no `Access-Control-Allow-Origin` header.
- readyz with the database stopped: a `not_ready` problem body followed by `[503]`.
- After SIGTERM: `api exit=0` and the `"shutting down"` log line.
- Startup with the database down: prints `api: ping database: ...connection refused` and `api exit=1`.
- Port 8080 must be free at the end (`ss -ltn | grep -c ':8080 '` prints 0).

- [ ] **Step 9: Lint and commit**

Run: `make lint-backend`
Expected: exits 0 silently.

```bash
git add Makefile backend
git commit -m "feat(backend): serve with graceful shutdown and add migrate command"
```

---

### Task 9: CI enforcement and documentation

**Files:**
- Modify: `.github/workflows/backend.yml`
- Modify: `.env.example`
- Modify: `CLAUDE.md`
- Modify: `backend/CLAUDE.md`

**Interfaces:**
- Consumes: `make check-generated`, `make lint-backend`, the tests (which need Docker; GitHub's `ubuntu-latest` runners have it).
- Produces: a backend workflow that also runs when `openapi.yaml` changes and fails on generated-code drift; documentation that matches the new commands, layout and decisions.

- [ ] **Step 1: Replace `.github/workflows/backend.yml`**

```yaml
name: backend

on:
  pull_request:
    paths:
      - backend/**
      - openapi.yaml
      - Makefile
      - .github/workflows/backend.yml
  push:
    branches: [master]
    paths:
      - backend/**
      - openapi.yaml
      - Makefile
      - .github/workflows/backend.yml

permissions:
  contents: read

concurrency:
  group: ${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: true

defaults:
  run:
    working-directory: backend

jobs:
  test:
    runs-on: ubuntu-latest
    timeout-minutes: 20
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: backend/go.mod
          cache-dependency-path: backend/go.sum
      - run: go vet ./...
      - run: go test ./...
      - run: make lint-backend
        working-directory: .
      - run: make check-generated
        working-directory: .
```

- [ ] **Step 2: Replace `.env.example`**

```dotenv
# Docker Compose reads .env: copy this file to .env and adjust. Other processes (including the API) read the shell environment only.

# Local Postgres (docker-compose.yml)
POSTGRES_USER=mealplanner
POSTGRES_PASSWORD=mealplanner
POSTGRES_DB=mealplanner
POSTGRES_PORT=5432

# API (backend/cmd/api, backend/cmd/migrate): read from the shell environment, not from .env
# `make run-api` and `make migrate` default DATABASE_URL to the compose database below.
API_ADDR=:8080
DATABASE_URL=postgres://mealplanner:mealplanner@localhost:5432/mealplanner?sslmode=disable
WEB_ORIGIN=http://localhost:3000
```

- [ ] **Step 3: Replace `backend/CLAUDE.md`**

````markdown
# Backend (Go)

Module: `github.com/InzKazik/mealplanner/backend`. Go 1.26, `chi` router, `pgx` and `goose` for Postgres.

## Layout

- `cmd/api/`: process entry point. Loads config, connects to Postgres, serves HTTP, shuts down gracefully on SIGINT/SIGTERM. No business logic.
- `cmd/migrate/`: applies pending migrations and exits. The API never applies migrations.
- `internal/config/`: environment configuration.
- `internal/api/`: **generated** from `openapi.yaml` (`api.gen.go`, never hand-edit) plus its `oapi-codegen` config (`oapi.yaml`).
- `internal/httpapi/`: HTTP layer. Router, middleware (request ID, logging, panic recovery, CORS), the problem+json writer, and the handlers that implement `api.ServerInterface`. Calls services only.
- `internal/db/`: pgx pool and the goose migration runner.
- `internal/testutil/`: integration-test helpers (a fresh Postgres database per test on a shared testcontainers container).
- `migrations/`: embedded goose SQL migrations.
- `internal/service/`, `internal/store/`: business rules and all SQL (`sqlc`). Added by the auth plan.

Dependencies point one way: `httpapi` → `service` → `store`. A package never imports one to its left.

## Commands (from repo root)

- `make test-backend`: `go vet ./...` and `go test ./...`. **Needs Docker** (integration tests start Postgres with testcontainers; without Docker they skip).
- `make lint-backend`: runs golangci-lint v2.13.2 via `go run` (the same command CI uses)
- `make generate`: regenerate `internal/api/api.gen.go` from `openapi.yaml`
- `make check-generated`: fail if the committed generated code differs from the spec
- `make migrate`: apply migrations to `DATABASE_URL` (defaults to the compose database)
- `make run-api`: run on `API_ADDR` from the process environment (default `:8080`; `.env` is not loaded). Needs `make db-up` and `make migrate` first.

## Conventions

- Add or change a route by editing `openapi.yaml` first, run `make generate`, then implement the new `api.ServerInterface` method. The build fails until you do.
- Every response shape is checked against `openapi.yaml` by the contract tests in `internal/httpapi/contract_test.go`; new endpoints get a contract test.
- Errors use `httpapi.WriteProblem` (RFC 9457 `application/problem+json`) with a stable `code` constant. Add new codes to `problem.go` and to the spec.
- Every response carries `X-Request-Id`. Log with `slog` (JSON to stdout) and include `httpapi.RequestID(ctx)`. No `fmt.Println` in non-test code.
- Migrations are goose SQL files named `NNNNN_description.sql` in `migrations/`, forward-only in production. Every table has `created_at` and `updated_at`, and an `updated_at` trigger that uses `set_updated_at()`.
- Tests are table-driven where there are several cases. Handler tests use `httptest`; integration tests use a real Postgres via `testutil.NewDatabase`, never mocks.
- Configuration comes from environment variables and is documented in `.env.example`.

## Not built yet (auth plan)

- Users, refresh tokens, JWT, Sign in with Apple, the auth middleware and the `/v1/auth/*` and `/v1/me` endpoints.
- Rate limiting (strict per-IP on `auth/*`, per-user elsewhere).
- The `service` and `store` layers and `sqlc`.
````

- [ ] **Step 4: Replace `CLAUDE.md`**

````markdown
# Meal Planner

Meal planning product: a web app (Next.js) and a native iOS app (SwiftUI) on one Go API. Users build meals from ingredients, assemble diets (templates applied to calendar dates), see calories, macros and micronutrients, generate categorized shopping lists, and link with one partner to share meals, diets and live shopping lists.

Design spec: `docs/superpowers/specs/2026-09-21-meal-planner-design.md`. Read it before changing behaviour. Implementation plans live in `docs/superpowers/plans/`. Agent workflow and boundaries: `AGENTS.md`.

## Locked decisions

- **Backend:** Go + Postgres. Layers `handler` (`internal/httpapi`) → `service` → `store`. No SQL outside `store`.
- **Toolchain:** Go 1.26, golangci-lint v2. Go supports only the two newest major versions, so check the support window before pinning an older Go.
- **Contract:** `openapi.yaml` (OpenAPI **3.0.3**: `oapi-codegen` does not support 3.1; use `nullable: true`) is the source of truth, edited by hand. Change the spec first, then regenerate (`make generate`), then implement.
- **Nutrition:** USDA FoodData Central subset imported into our DB. Per-100 g values stored as rows; meal and plan nutrition is **computed on read**, never stored.
- **Sharing:** 1:1 partnership. Partner access to meals and diets is read-only + copy; shopping lists are editable by both. Unseen resources return `404`, never `403`. Rules live in the service layer only.
- **Diets:** templates copy into per-date `plan_entries`; after applying, entries are independent of the template.
- **Web:** Next.js App Router, Tailwind, shadcn/ui. **iOS:** SwiftUI, iOS 26+, online-first with a SwiftData cache.
- **Auth:** email/password + Sign in with Apple. JWT access tokens (15 min) + rotating refresh tokens.
- **Errors:** RFC 9457 `application/problem+json` with a stable `code`.
- **Migrations:** goose, forward-only in production, applied by `cmd/migrate` before the new API starts. The API never migrates.

## Repo map

- `openapi.yaml`, `redocly.yaml`: API contract and lint rules
- `.redocly.lint-ignore.yaml`: the only lint exemption (`/healthz` and `/readyz` are exempt from the "every operation declares a 4XX response" rule; every other operation must declare one)
- `backend/`: Go API (see `backend/CLAUDE.md`)
- `web/`, `ios/`: added by their own plans
- `docker-compose.yml`: local Postgres (API and web services are added later)
- `.github/workflows/`: path-filtered CI

## Commands

Run `make help` for the list. Most used:

| Command | What it does |
|---|---|
| `make check` | Everything CI runs, except the compose workflow (needs Docker for the tests) |
| `make lint-api` | Lint `openapi.yaml` |
| `make test-backend` | `go vet` + `go test` for the backend (needs Docker) |
| `make lint-backend` | `golangci-lint` for the backend |
| `make generate` | Regenerate backend code from `openapi.yaml` |
| `make check-generated` | Fail if committed generated code is stale |
| `make db-up` / `make db-down` | Start / stop local Postgres |
| `make migrate` | Apply migrations to the local database |
| `make run-api` | Run the API on `:8080` |

Copy `.env.example` to `.env` for Docker Compose (`make db-up`). The API reads its variables (for example `API_ADDR`, `DATABASE_URL`) from the shell environment and does not load `.env`; the Makefile defaults `DATABASE_URL` to the compose database. Never commit `.env` or secrets.

## Conventions

- Tests first for service logic and bug fixes.
- Never hand-edit generated code. Regenerate it from `openapi.yaml`.
- Keep commits small; one logical change each.
- Any new environment variable must be added to `.env.example` in the same commit.
````

- [ ] **Step 5: Verify the workflow and compose files parse and the workflow shape is right**

Run:

```bash
python3 - <<'EOF'
import yaml
wf = yaml.safe_load(open('.github/workflows/backend.yml'))
on = wf[True]
assert 'openapi.yaml' in on['pull_request']['paths'] and 'openapi.yaml' in on['push']['paths']
steps = wf['jobs']['test']['steps']
runs = [s.get('run') for s in steps if 'run' in s]
assert runs == ['go vet ./...', 'go test ./...', 'make lint-backend', 'make check-generated'], runs
assert steps[2]['with']['cache-dependency-path'] == 'backend/go.sum'
assert wf['jobs']['test']['timeout-minutes'] == 20
print('backend.yml ok')
EOF
```
Expected: `backend.yml ok`.

- [ ] **Step 6: Run the whole check and confirm the docs are accurate**

Run: `make check`
Expected: `lint-api` valid with `2 problems are explicitly ignored`; `go vet` clean and every package `ok` (the database tests actually ran, not skipped: run `cd backend && go test ./internal/db/ ./cmd/api/ -count=1 -v 2>&1 | grep -E 'SKIP|PASS'` and confirm PASS, no SKIP); `lint-backend` exits 0; `check-generated` exits 0.
Run: `git status --short`
Expected: prints nothing.
Confirm every `make` target named in `CLAUDE.md` and `backend/CLAUDE.md` exists in `make help`.

- [ ] **Step 7: Commit**

```bash
git add .github/workflows/backend.yml .env.example CLAUDE.md backend/CLAUDE.md
git commit -m "ci: enforce generated code and document the backend foundation"
```

---

## Self-review against the spec

| Spec requirement | Covered by |
|---|---|
| 2.2 Layers, `chi`, `slog` | Tasks 7, 8 |
| 2.2 Middleware: request ID, CORS, panic recovery, logging | Task 7 (rate limiting deferred to the auth plan) |
| 2.2 `/healthz` and `/readyz` | Tasks 2, 7 |
| 2.2 goose migrations, forward-only, applied before the API starts | Tasks 5, 8 (`cmd/migrate`, `make migrate`) |
| 2.3 `oapi-codegen` server interfaces and types; drift is a compile error | Task 6 (interface), Task 7 (implements it) |
| 2.3 CI regenerates and fails on drift | Tasks 6, 9 (`make check-generated` in CI) |
| 2.3 Responses validated against the spec | Task 7 contract tests (kin-openapi) |
| 4.2 RFC 9457 errors with stable codes | Tasks 4, 7 (404/405/500/503 all problem+json) |
| 6 Real Postgres via testcontainers, no DB mocks | Tasks 5, 8 |
| 7 CI path-filtered; 12-factor config; `.env.example` | Tasks 3, 8, 9 |
| 8 CLAUDE.md kept current | Tasks 1, 9 |
| Scaffold "Known gaps" list (shutdown, timeouts, slog default, `/readyz`, problem writer, request ID) | Tasks 4, 7, 8 |

Type and name consistency: `WriteProblem`/`Code*` (Tasks 4, 7), `Deps`/`NewRouter`/`RequestID` (Task 7, used by Task 8), `db.Connect`/`db.Migrate` (Task 5, used by Task 8), `testutil.NewDatabase` (Task 5, used by Tasks 5 and 8), `config.Config`/`config.Load` (Task 3, used by Task 8), `api.*` (Task 6, used by Task 7) all match across tasks.
