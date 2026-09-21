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
