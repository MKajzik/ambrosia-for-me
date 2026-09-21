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
- `make lint-backend`: runs golangci-lint v1.64.8 via `go run` (the same command CI uses)
- `make run-api`: run on `API_ADDR` from the process environment (default `:8080`; `.env` is not loaded)

## Conventions

- Routes live under `/v1` and must exist in `openapi.yaml` first.
- Errors map to RFC 9457 `application/problem+json` with a stable `code`.
- Logging uses `slog` (JSON to stdout). No `fmt.Println` in non-test code.
- Tests are table-driven where there are several cases. Handler tests use `httptest`; integration tests use a real Postgres, not mocks.
- Configuration comes from environment variables and is documented in `.env.example`.

## Known gaps (fix in the first backend plan)

- `cmd/api/main.go` has no graceful shutdown: add `signal.NotifyContext` plus `srv.Shutdown`, and compare with `errors.Is(err, http.ErrServerClosed)`.
- The server sets only `ReadHeaderTimeout`: add `ReadTimeout`, `WriteTimeout` and `IdleTimeout`.
- The `slog` logger is not installed with `slog.SetDefault` and is not passed to the router.
- No `/readyz` yet: it needs the `store` layer and a database connection.
- No RFC 9457 error writer: `http.ServeMux` answers unknown paths and wrong methods with `text/plain` 404/405. Add `httpapi.writeProblem` plus `NotFound` / `MethodNotAllowed` fallbacks when the router moves to `chi`.
- No request-ID middleware (spec section 2.2): add it with the first middleware and propagate the ID through logs.
