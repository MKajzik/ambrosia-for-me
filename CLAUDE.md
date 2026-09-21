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
- **Auth:** email/password (built) and Sign in with Apple (own plan). HS256 JWT access tokens (15 min) + opaque rotating refresh tokens (30 days, stored hashed; replaying a used one revokes the session family). Enforcement comes from the `security` blocks in `openapi.yaml`: global `bearerAuth`, `security: []` opts a route out, so a new route is protected by default.
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
| `make generate` | Regenerate backend code: oapi-codegen from `openapi.yaml`, sqlc from migrations and queries |
| `make check-generated` | Fail if committed generated code is stale |
| `make db-up` / `make db-down` | Start / stop local Postgres |
| `make migrate` | Apply migrations to the local database |
| `make run-api` | Run the API on `:8080` |

Copy `.env.example` to `.env` for Docker Compose (`make db-up`). The API reads its variables (`API_ADDR`, `DATABASE_URL`, `JWT_SECRET`, ...) from the shell environment and does not load `.env`; the Makefile defaults `DATABASE_URL` to the compose database and, for `make run-api` only, `JWT_SECRET` to a development-only value. Never commit `.env` or secrets.

## Conventions

- Tests first for service logic and bug fixes.
- Never hand-edit generated code (`backend/internal/api`, `backend/internal/store/sqlc`). Regenerate it with `make generate`.
- Keep commits small; one logical change each.
- Any new environment variable must be added to `.env.example` in the same commit.
