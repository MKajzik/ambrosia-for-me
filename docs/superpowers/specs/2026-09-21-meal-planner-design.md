# Meal Planner: Design Spec

Date: 2026-09-21
Status: Approved in brainstorming, pending written-spec review

## 1. Overview

A modern meal planning product available as a **web app** and a **native iOS app**, backed by a single **Go API**. Users build meals from ingredients, assemble diets (reusable templates applied to calendar dates), see calories, macronutrients and micronutrients, generate categorized shopping lists, and connect with one partner to share meals, diets and live shopping lists.

### Decisions locked during brainstorming

| Topic | Decision |
|---|---|
| Backend | Go + Postgres |
| Nutrition data | USDA FoodData Central, curated subset imported into our DB; users may add custom ingredients |
| Web | Next.js (App Router) + React + Tailwind + shadcn/ui |
| iOS | Native SwiftUI, iOS 26+ (Liquid Glass), online-first with SwiftData cache |
| Repo shape | Monorepo, OpenAPI-first (`openapi.yaml` is the contract) |
| Sharing | 1:1 partner link; meals, diets and shopping lists shareable |
| Diet model | Templates that can be applied to calendar dates, then tweaked per day |
| Auth | Email/password + Sign in with Apple (no Google in v1) |
| Realtime | Server-Sent Events for shared shopping lists |

### Non-goals (out of v1)

Barcode scanning, recipe import from URLs, photo logging, meal-logging and progress tracking, push notifications, Android, multi-member households, Google sign-in, an iOS widget, offline editing on the web, metrics/error-tracking infrastructure, and choosing a production host.

## 2. Architecture

### 2.1 Repo layout

```
mealPlanner/
├── CLAUDE.md            # project-wide conventions, commands, decisions
├── AGENTS.md            # agent workflow, boundaries, definition of done
├── openapi.yaml         # API contract (single source of truth)
├── docker-compose.yml   # postgres + api + web for local dev
├── backend/             # Go API (own CLAUDE.md)
├── web/                 # Next.js app (own CLAUDE.md)
├── ios/                 # SwiftUI app (own CLAUDE.md)
└── docs/superpowers/{specs,plans}/
```

### 2.2 Backend (Go + Postgres)

- **Layers:** `handler` (HTTP, request shape) → `service` (business rules: nutrition math, sharing permissions, list generation) → `store` (SQL via `sqlc`; migrations via `goose`). Handlers never touch SQL; no SQL exists outside `store`.
- **Router:** `chi`. **Logging:** `slog`, structured, with request IDs propagated through context.
- **Auth:** JWT access tokens (15 min); rotating refresh tokens stored hashed with a `family_id`; reuse of a rotated token revokes the whole family. Passwords use argon2id. Sign in with Apple verifies Apple's identity token against Apple's JWKS.
- **Middleware:** rate limiting (strict per-IP on `auth/*`, per-user elsewhere), CORS allowlisted to the web origin, panic recovery, request ID.
- **Ops endpoints:** `/healthz` (liveness), `/readyz` (DB reachable).
- **USDA import:** `cmd/import-usda` loads the FoodData Central **Foundation Foods** dataset (whole/minimally-processed foods with lab-analyzed nutrients; excludes branded/packaged items, consistent with the no-barcode-scanning non-goal) into `ingredients` and `ingredient_nutrients`. It is idempotent, keyed by `usda_fdc_id`; rerunning picks up USDA data updates. USDA food groups map to our shopping categories via a hand-written lookup table in the importer; an unmapped group falls back to `other` and logs a warning rather than failing the import.
- **Realtime:** one SSE endpoint per shopping list. Writes always go through REST.

### 2.3 Contract flow

1. `openapi.yaml` is edited by hand and is the source of truth.
2. The backend generates server interfaces and types with `oapi-codegen`, and validates requests and responses against the spec, so drift is a compile or test error.
3. `web/` generates a typed client with `openapi-typescript`. `ios/` generates one with Swift OpenAPI Generator.
4. CI regenerates all clients and fails if the committed output differs.

### 2.4 Web (Next.js)

- Server components for layout and shell; client components with TanStack Query for data and optimistic updates.
- Tailwind with a single token set (colour, radius, spacing, type scale); shadcn/ui; Framer Motion; dark mode; PWA basics (installable, no offline editing).
- Auth: access token in memory; refresh token in an httpOnly, secure, sameSite cookie set by Next route handlers acting as a thin BFF for `auth/*` only. All other calls go directly to the API.

### 2.5 iOS (SwiftUI)

- MVVM with `@Observable` and Swift 6 concurrency. Layers: generated `APIClient` → `Repository` (network + SwiftData cache) → view models → views. Views never call the network directly.
- Tokens in the Keychain, refreshed transparently. Sign in with Apple via `AuthenticationServices`.
- Online-first: the API is the source of truth. The cache makes launches instant and keeps the shopping list readable and checkable offline.

## 3. Data model (Postgres)

All primary keys are UUIDs. Every table has `created_at` and `updated_at`. Deletes are hard unless stated otherwise.

### 3.1 Identity and sharing

- **users:** `email` (unique, citext), `password_hash` (nullable for Apple-only accounts), `apple_sub` (unique, nullable), `display_name`, daily targets `target_kcal`, `target_protein_g`, `target_carbs_g`, `target_fat_g` (all nullable).
- **refresh_tokens:** `user_id`, `token_hash`, `family_id`, `expires_at`, `revoked_at`.
- **partnerships:** `user_a` (the inviter), `user_b` (nullable until accepted), `status` (`pending` | `active`), `invite_code_hash` and `invite_expires_at` (set while pending, cleared on accept), `created_by`. The invite code has 8 characters from `23456789ABCDEFGHJKMNPQRSTUVWXYZ`, expires after 48 hours, is stored hashed and shown once. Partial unique indexes limit each user to one active row per column and one pending invite; `accept` locks both users so nobody ends up in two active rows. Details: `2026-09-23-backend-partner-sharing-design.md`.

### 3.2 Ingredients and nutrition

- **ingredients:** `name`, `category` (one of `produce`, `dairy_eggs`, `meat_seafood`, `grains_bread`, `legumes_nuts_seeds`, `condiments_oils`, `spices_herbs`, `beverages`, `sweets_snacks`, `other`; drives shopping-list grouping), `owner_id` (NULL = global/USDA, otherwise a custom ingredient), `usda_fdc_id` (nullable, unique), `grams_per_piece` (nullable), `density_g_per_ml` (nullable). Search uses `pg_trgm` on `name`.
- **ingredient_nutrients:** `ingredient_id`, `nutrient_key` (enum), `amount_per_100g`. Nutrients are rows, not columns, so adding one needs no migration.

**Nutrient set (v1):** calories (kcal), protein, carbohydrates, sugar, fibre, fat, saturated fat, sodium, potassium, calcium, iron, magnesium, zinc, vitamin A, vitamin C, vitamin D, vitamin B12, folate. Macronutrients are shown in the summary tier; the rest in the expandable full-nutrient tier against daily reference values.

### 3.3 Meals

- **meals:** `owner_id`, `name`, `notes`, `servings` (numeric, > 0), `shared_with_partner` (bool).
- **meal_ingredients:** `meal_id`, `ingredient_id`, `quantity` (> 0), `unit` (`g` | `ml` | `piece`), `position`.
- **Nutrition is computed on read, not stored:** convert each quantity to grams (`ml` via `density_g_per_ml`, `piece` via `grams_per_piece`), apply `amount_per_100g`, sum, divide by `servings`. A cached total may be added later only if profiling shows a need.
- Conversion fails with a stable error code when the ingredient lacks the data needed for the chosen unit (for example `unit_not_convertible`). The service layer rejects such rows at write time.

### 3.4 Diets and plan

- **diet_templates:** `owner_id`, `name`, `day_count` (>= 1; 7 for a week), `shared_with_partner`.
- **template_slots:** `template_id`, `day_index` (0-based, < `day_count`), `slot` (`breakfast` | `lunch` | `dinner` | `snack`), `meal_id`, `portion` (numeric, default 1). Multiple `snack` rows per day are allowed; other slots are unique per day.
- **plan_entries:** `owner_id`, `date`, `slot`, `meal_id`, `portion`, `from_template_id` (nullable, provenance only).
- **Applying a template** copies its slots into `plan_entries` starting at the chosen date. After that, entries are independent: swapping Tuesday's meal changes one row and never alters the template. Applying over dates that already have entries returns a conflict unless the client passes `overwrite: true`.
- Daily and weekly totals are computed from `plan_entries` with the same math as meal nutrition and compared against the user's targets.

### 3.5 Shopping lists

- **shopping_lists:** `owner_id`, `name`, `shared_with_partner`, optional `source_from` / `source_to` dates.
- **shopping_items:** `list_id`, `ingredient_id` (nullable for free-text items), `name`, `quantity`, `unit`, `category`, `checked`, `checked_by`, `position`, `version`, `origin` (`generated` | `manual`).
- **Generation:** read `plan_entries` in the range, convert to grams (or keep ml/piece where units cannot merge), sum per ingredient, group by category, write items with `origin = generated`. Regenerating a list replaces `generated` items and keeps `manual` items.

### 3.6 Sharing rule

Enforced once, in the service layer:

- A resource is readable by its owner.
- It is also readable by the owner's **active** partner when `shared_with_partner` is true.
- Partners have **read-only + copy** access to meals and diet templates.
- Shopping lists are **editable by both** partners; a partner checking an item emits an SSE event.
- Unlinking ends access immediately. Lists owned by the departing partner stay with their owner.
- A resource the caller cannot see returns `404`, never `403`, so IDs do not leak.

## 4. API

All routes are under `/v1`, JSON only. Everything except `auth/*` and health checks requires a Bearer token.

### 4.1 Endpoints

- **Auth:** `POST auth/register`, `auth/login`, `auth/apple`, `auth/refresh`, `auth/logout`; `GET/PATCH me` (profile and targets); `DELETE me` (account deletion, required by App Store rules).
- **Ingredients:** `GET ingredients?q=&category=`; `POST`, `PATCH`, `DELETE ingredients/{id}` (custom only; deleting one in use returns `ingredient_in_use`).
- **Meals:** `GET/POST meals`; `GET/PATCH/DELETE meals/{id}` (GET includes computed nutrition); `PUT meals/{id}/ingredients` (atomic replace of the ingredient list); `POST meals/{id}/copy`.
- **Diet templates:** `GET/POST diet-templates`; `GET/PATCH/DELETE diet-templates/{id}`; `PUT diet-templates/{id}/slots`; `POST diet-templates/{id}/apply` (`{start_date, overwrite?}`); `POST diet-templates/{id}/copy`.
- **Plan:** `GET plan?from=&to=` (entries, daily totals, target comparison); `PUT/DELETE plan/{date}/{slot}` (set or swap a meal, or change the portion).
- **Shopping lists:** `GET/POST shopping-lists`; `GET/PATCH/DELETE shopping-lists/{id}`; `POST shopping-lists/generate` (`{from, to}`); `POST shopping-lists/{id}/items`; `PATCH/DELETE shopping-lists/{id}/items/{item}`; `GET shopping-lists/{id}/events` (SSE).
- **Partner:** `POST partner/invite` (returns a code); `POST partner/accept`; `GET partner`; `DELETE partner`; `GET partner/meals`; `GET partner/diet-templates`; `GET partner/shopping-lists`. `Meal`, `DietTemplate` and `ShoppingList` carry `is_owner`, so clients can render a partner's resource read-only.

### 4.2 Conventions

- **Pagination:** cursor-based (`?limit=&cursor=`) on lists that can grow (meals, ingredients).
- **Errors:** RFC 9457 `application/problem+json` with a stable machine-readable `code` (for example `partner_not_linked`, `ingredient_in_use`, `unit_not_convertible`). Clients map codes to localized text.
- **Validation:** the OpenAPI schema enforces shape; the service layer enforces rules (positive quantities, convertible units, slots within `day_count`).

### 4.3 Shopping-list sync

- Item edits carry the item's `version`. A stale write returns `409` with the current item, and the client re-applies its change.
- Checking an item is idempotent (`{checked: true}`), so retries and offline replays are safe.
- The iOS offline queue stores intents (check, uncheck, add, remove) and replays them in order on reconnect. Checked state is last-write-wins; adds and removes merge.
- SSE events carry list id, item id and new version. Clients patch local state, or refetch the list when they detect a version gap. Clients also refetch on foreground and on reconnect, so a dropped stream never causes lasting drift.

## 5. Clients

### 5.1 Product structure (both clients)

1. **Today:** today's slots with calorie and macro rings against targets; swap a meal or change a portion in one tap.
2. **Plan:** week calendar; apply a template to a date; swap meals; daily and weekly totals.
3. **Meals:** own library plus the partner's shared meals; editor with ingredient list, quantity and unit steppers, a live nutrition panel, and ingredient swap via search.
4. **Shopping:** lists grouped by aisle category; check-off, quick-add, generate from a plan date range; a partner badge and live updates on shared lists.
5. **Profile:** targets, partner connection (invite code or accept), custom ingredients, account deletion.

Ingredient search is one shared component (type-ahead, category filter, "create custom ingredient" fallback). Nutrition has two tiers: a summary (kcal, protein, carbs, fat) and an expandable full-nutrient view.

### 5.2 Visual direction

- Calm, food-forward: generous whitespace, large rounded cards, one confident accent colour, warm neutral surfaces, full dark mode.
- Each macro keeps one consistent colour across the product.
- Motion carries meaning only (rings animate on change, check-off ticks, sheets feel physical); reduced-motion is respected.
- iOS uses native Liquid Glass materials, SF Symbols and system typography. Web uses a matching token set defined once in the Tailwind config. The two feel like one product without imitating each other.
- Exact palette and type choices are settled at implementation with the `frontend-design` skill; mockups may come first.

### 5.3 Web specifics

- Routes: `/login`, `/register`, `/today`, `/plan`, `/meals`, `/meals/[id]`, `/shopping`, `/shopping/[id]`, `/profile`.
- TanStack Query owns server state with optimistic updates on check-off and swaps, and subscribes to SSE on an open shared list.
- Responsive: desktop has a sidebar and split-pane meal editor; phone has a bottom tab bar.
- Accessibility: keyboard navigable, WCAG AA contrast, labelled controls.

### 5.4 iOS specifics

- `TabView` with the five areas; each has a `NavigationStack`; meal and ingredient editors are sheets.
- The shopping list is the offline-critical screen: it reads from SwiftData, queues intents, and shows a subtle syncing state.
- VoiceOver labels, Dynamic Type and reduced motion are supported.

## 6. Testing

- **Backend unit tests** cover the service layer: unit conversion, nutrition math, shopping-list merging, sharing permissions, token rotation and reuse.
- **Backend integration tests** run handlers against a real Postgres via `testcontainers` (no DB mocks), with table-driven cases and a fixture of two users, a partnership and a shared list.
- **Contract test** validates responses against `openapi.yaml`.
- **Nutrition golden tests** check totals for a few hand-verified real recipes to the gram.
- **Web:** Vitest and Testing Library for components and hooks; Playwright for critical flows (register, build a meal, apply a template, generate a shopping list, check off a shared item across two browser contexts).
- **iOS:** Swift Testing for view models and repositories, including offline-queue replay; XCUITest for sign-in, meal editing and shopping check-off; the `apple:accessibility` audit before release.
- **TDD** applies: tests come first for service logic and bug fixes.

## 7. Delivery

- **CI (GitHub Actions), path-filtered per package:** backend (`go vet`, `golangci-lint`, tests); web (typecheck, lint, unit tests, Playwright); iOS (build and tests on a macOS runner); all (regenerate API clients and fail on drift).
- **Local dev:** Docker Compose runs Postgres, API and web with seed data. Configuration is 12-factor via environment variables; `.env.example` documents each variable; secrets are never committed.
- **Migrations:** `goose`, forward-only in production, applied at deploy before the new API version starts.
- **Hosting:** deferred. The API ships as a container and the web app as a standard Next.js build, so neither is tied to a host; the host is a separate decision once the API exists.
- **iOS release:** via the `apple:testflight` and `apple:ship` skills, including the privacy manifest and in-app account deletion.
- **Observability:** structured logs with request IDs in v1; metrics and error tracking are a follow-up.

## 8. CLAUDE.md and AGENTS.md

- **Root `CLAUDE.md`:** what the app is; the locked decisions in section 1; the repo map; exact commands to build, test, lint, regenerate clients and run locally; conventions (`openapi.yaml` changes come first, tests first for service logic, no SQL outside `store`). Kept short, since it loads every session.
- **Per-package `CLAUDE.md`** in `backend/`, `web/` and `ios/`: layout, idioms and gotchas for that package.
- **`AGENTS.md`:** tool-neutral agent guidance. Workflow (spec → plan → tests → implement → verify); which skills apply where (`frontend-design` for web; `apple-skills` and `apple:*` for iOS); boundaries (never hand-edit generated code, never commit secrets, never change the API without updating the spec); definition of done. `CLAUDE.md` points to it so guidance lives in one place.

## 9. Build order

Each step gets its own implementation plan.

1. Repo scaffold: `CLAUDE.md` and `AGENTS.md`, Compose, CI skeleton, initial `openapi.yaml`.
2. Backend, in this order: auth; ingredients with the USDA import; meals; diets and plan; shopping lists; partner and sharing with SSE.
3. Web app.
4. iOS app.

## 10. Open items to resolve during planning

- Daily reference values used for micronutrient percentages (source and whether they vary by user).
- Production hosting choice (post-API).
