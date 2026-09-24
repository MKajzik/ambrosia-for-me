# Meal Planner

Meal planning product: web app (Next.js) + native iOS app (SwiftUI) on one Go API. Users build meals from ingredients, assemble diets (templates applied to calendar dates), see calories, macros, micronutrients, generate categorized shopping lists, link with one partner to share meals, diets, live shopping lists.

Design spec: `docs/superpowers/specs/2026-09-21-meal-planner-design.md`. Read before changing behaviour. Implementation plans in `docs/superpowers/plans/`. Agent workflow + boundaries: `AGENTS.md`.

## Locked decisions

- **Backend:** Go + Postgres. Layers `handler` (`internal/httpapi`) → `service` → `store`. No SQL outside `store`.
- **Toolchain:** Go 1.26, golangci-lint v2. Go supports only two newest major versions; check support window before pinning older Go.
- **Contract:** `openapi.yaml` (OpenAPI **3.0.3**: `oapi-codegen` no support for 3.1; use `nullable: true`) = source of truth, hand-edited. Change spec first, regenerate (`make generate`), then implement.
- **Nutrition:** USDA FoodData Central subset imported into our DB. Per-100 g values stored as rows; meal + plan nutrition **computed on read**, never stored.
- **Sharing:** 1:1 partnership. Partner access to meals + diets read-only + copy; shopping lists editable by both. Unseen resources return `404`, never `403`. Rules in service layer only.
- **Diets:** templates copy into per-date `plan_entries`; after apply, entries independent of template.
- **Web:** Next.js App Router, Tailwind, shadcn/ui. **iOS:** SwiftUI, iOS 26+, online-first, SwiftData cache.
- **Auth:** email/password (built) + Sign in with Apple (own plan). HS256 JWT access tokens (15 min) + opaque rotating refresh tokens (30 days, stored hashed; replaying used one revokes session family). Enforcement from `security` blocks in `openapi.yaml`: global `bearerAuth`, `security: []` opts route out, so new route protected by default.
- **Errors:** RFC 9457 `application/problem+json` with stable `code`.
- **Migrations:** goose, forward-only in production, applied by `cmd/migrate` before new API starts. API never migrates.

## Repo map

- `openapi.yaml`, `redocly.yaml`: API contract + lint rules
- `.redocly.lint-ignore.yaml`: only lint exemption (`/healthz` and `/readyz` exempt from "every operation declares a 4XX response" rule; all other operations must declare one)
- `backend/`: Go API (see `backend/CLAUDE.md`)
- `web/`, `ios/`: added by own plans
- `docker-compose.yml`: local Postgres (API + web services added later)
- `.github/workflows/`: path-filtered CI

## Commands

Run `make help` for list. Most used:

| Command | What it does |
|---|---|
| `make check` | All CI runs, except compose workflow (tests need Docker) |
| `make lint-api` | Lint `openapi.yaml` |
| `make test-backend` | `go vet` + `go test` for backend (needs Docker) |
| `make lint-backend` | `golangci-lint` for backend |
| `make generate` | Regenerate backend code: oapi-codegen from `openapi.yaml`, sqlc from migrations + queries |
| `make check-generated` | Fail if committed generated code stale |
| `make db-up` / `make db-down` | Start / stop local Postgres |
| `make migrate` | Apply migrations to local DB |
| `make run-api` | Run API on `:8080` |

Copy `.env.example` to `.env` for Docker Compose (`make db-up`). API reads vars (`API_ADDR`, `DATABASE_URL`, `JWT_SECRET`, ...) from shell env, does not load `.env`; Makefile defaults `DATABASE_URL` to compose DB and, for `make run-api` only, `JWT_SECRET` to public dev-only value plus `ALLOW_DEV_JWT_SECRET=1` (API refuses that secret otherwise). Never commit `.env` or secrets.

## Conventions

- Tests first for service logic + bug fixes.
- Never hand-edit generated code (`backend/internal/api`, `backend/internal/store/sqlc`). Regenerate with `make generate`.
- Small commits; one logical change each.
- New env var → add to `.env.example` in same commit.

## Agent skills

### Issue tracker

Issues in repo's GitHub Issues (github.com/MKajzik/ambrosia-for-me), via `gh` CLI. See `docs/agents/issue-tracker.md`.

### Triage labels

Default five-role vocabulary (needs-triage, needs-info, ready-for-agent, ready-for-human, wontfix). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `CONTEXT.md` + `docs/adr/` at repo root. See `docs/agents/domain.md`.