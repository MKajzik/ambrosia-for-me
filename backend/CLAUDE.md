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

- `make test-backend`: `go vet ./...` and `go test ./...`. **Needs Docker** (integration tests start Postgres with testcontainers). Without Docker they skip locally but fail when `CI` is set, so CI cannot pass silently without them.
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

## Decide first in the auth plan

Findings from the foundation's final review. They are design inputs, not bugs on this branch.

- **Auth enforcement has no seam yet.** `oapi-codegen` v2.8.0 emits no per-route security information (`enable-auth-scopes-on-context` is deprecated upstream). Use `github.com/oapi-codegen/nethttp-middleware` with an `AuthenticationFunc` (kin-openapi validates each request against the spec's `security` blocks), passed through `api.ChiServerOptions.Middlewares`, so routes added later fail closed. Do not rely on path-prefix matching.
- **chi constraint.** `api.HandlerWithOptions` registers routes on the root mux, and chi panics if `r.Use` runs after routes exist. Per-route middleware (for example a strict auth rate limit) must go through `ChiServerOptions.Middlewares` and can branch on `chi.RouteContext(r.Context()).RoutePattern()`, or be a root-level middleware.
- **Problem shape.** `Problem` only has a free-text `detail`. Add an optional `errors: [{field, code}]` member and a `WriteValidationProblem` before the first validation endpoint ships: a contract change is expensive once the web and iOS clients are generated. Also add shared `components/responses` (`Unauthorized`, `NotFound`, ...) so 404/405 and auth errors are in the contract and covered by the contract tests.
- **Client IP.** No client IP is extracted or logged. Add a trusted-proxy setting, an IP-extraction middleware (never trust `X-Forwarded-For` blindly) and `remote_ip` in the request log before per-IP rate limiting.
- **Tests.** Add `testutil.NewMigratedDatabase(t)` (a migrated template database, `CREATE DATABASE x TEMPLATE y`) so store tests do not each migrate. Grow the contract helper in `internal/httpapi/contract_test.go` (request body, headers, bearer token; load the spec once with `sync.OnceValue`; validate 404/405 problems). Add a real trigger test for `set_updated_at()` with the first table that uses it.
- **Consider `strict-server: true`** in `internal/api/oapi.yaml` before writing many handlers: response-shape drift becomes a compile error. It changes every handler signature, so decide once, early.
- **Hardening to schedule:** `goose` session locker in `db.Migrate` before any multi-replica deploy; `ErrorHandlerFunc` must stop echoing `err.Error()` once routes take parameters; `NewRouter` should fail fast on a nil `Logger` or `Ready`; `/readyz` needs a check timeout (about 2s); a second SIGINT/SIGTERM is swallowed during shutdown drain (call `stop()` after `<-ctx.Done()`); `cmd/api/main_test.go` polls with an untimed `http.Get`; log 5xx at error level with `duration_ms`; `recoverer` after a partial write appends a problem body; enable the `bodyclose`, `gosec` and `sqlclosecheck` linters.
- **`make check-generated`** uses `git diff --exit-code`, which ignores untracked files. When `sqlc` output lands (a generated package nothing imports yet could slip through), switch to `git add -AN -- <dir> && git diff --exit-code -- <dir>`.
- **Later (shopping-list SSE plan):** the 30s server `WriteTimeout` cuts event streams: override it per handler with `http.NewResponseController(w).SetWriteDeadline(time.Time{})` (chi's response wrapper supports `Unwrap`). `Server.Shutdown` does not cancel request contexts and a live stream never goes idle, so set `BaseContext` or `RegisterOnShutdown` so streams end on shutdown, otherwise every shutdown with a connected client burns the full 10s and exits 1.
