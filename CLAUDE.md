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
- `.redocly.lint-ignore.yaml`: the only lint exemption (`/healthz` is exempt from the "every operation declares a 4XX response" rule; every other operation must declare one)
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
