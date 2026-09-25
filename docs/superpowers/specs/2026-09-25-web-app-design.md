# Web App Design

Status: draft for review. Extends `2026-09-21-meal-planner-design.md` (§5.1 to §5.3, §6, §7, §9 step 3, §10). Where the two disagree on the web app, this document wins and the parent spec is updated in the same change.

## 1. Goal

A Next.js web app on the existing Go API. It covers login and register plus the five product areas from parent spec §5.1: Today, Plan, Meals, Shopping and Profile. Shared shopping lists update live over SSE. The API contract (`openapi.yaml`) does not change for the web app, except where §9 says so.

Success: the Playwright flows in §7 pass against the real API, including a check-off in one browser context appearing live in a second.

Already locked by the parent spec: App Router, Tailwind, shadcn/ui, TanStack Query with optimistic updates on check-off and swaps, Vitest, Testing Library and Playwright, and the visual direction of §5.2 (palette and type are settled at implementation with the `frontend-design` skill).

## 2. Decisions

| Question | Decision |
|---|---|
| Where the browser keeps tokens | Nowhere in JS. A Next.js backend-for-frontend (BFF) holds both tokens in httpOnly cookies and proxies to the Go API. The browser only calls its own origin. |
| API client | `openapi-typescript` types generated from `openapi.yaml` and `openapi-fetch` for typed calls. Drift fails `make check-generated` and CI, like the Go code. |
| Micronutrient reference | Fixed FDA Daily Values for adults, as a typed constants file in `web/src/lib/nutrition`. Same for every user. No API or profile change. This closes the parent spec §10 item on reference values; hosting stays open. |
| Server-side data fetching | None in v1. `(app)` pages are client components on TanStack Query. Server-component prefetch can be added later where it pays off. |
| Delivery | One spec, four plans and four PRs (§10). Each later plan is written after the previous one merges, so it reflects what was built. |

## 3. BFF and auth

The Go API is bearer-only: `/auth/*` returns `access_token` and `refresh_token` in the JSON body and never sets cookies. The BFF adapts that to the browser.

**Cookies.** Two httpOnly cookies, `Secure` outside local development, `SameSite=Lax`: the 30-day refresh token, scoped to `/api`, and the 15-minute access token.

**Routes** (Node runtime, server-only code):

- `POST /api/auth/login`, `/api/auth/register` and `/api/auth/logout` are custom handlers. They call the Go API and set or clear the cookies. The response body to the browser carries the user, never a token.
- `/api/[...path]` is a catch-all that forwards to `API_BASE_URL` (`http://localhost:8080/v1` in dev) with `Authorization: Bearer`. On an expired access token or a `401` it refreshes once and retries. If the refresh fails, it clears the cookies and returns `401`, and the UI redirects to `/login`.
- Next.js middleware redirects page requests to `/login` when the refresh cookie is absent.

**Rotation race.** Replaying a used refresh token revokes the whole session family, so parallel requests must not each refresh. The proxy single-flights refresh per refresh token, in process, and keeps the resulting pair for about 30 seconds. A second in-flight request that still carries the old cookie gets the same new pair instead of replaying. This assumes one web instance in v1, which the deferred hosting decision has to respect; a multi-instance deploy needs a shared store first.

**CSRF.** The proxy rejects state-changing requests unless `Content-Type` is `application/json` and `Origin` matches the app's own origin.

**Client IP.** The API rate-limits by client IP (auth per IP, and the partner accept limiter per IP). Behind the BFF every user would share the web server's address. The proxy therefore forwards `X-Forwarded-For`, and the API runs with `TRUSTED_PROXY_COUNT=1` in Compose and in any deployment.

**Config.** `API_BASE_URL` is server-only and goes in `.env.example`. `WEB_ORIGIN` on the API side is not needed for browser calls, since they are same-origin, and stays as it is.

## 4. SSE through the proxy

The list stream requires the `Authorization` header and rejects tokens in the query string, so the browser's `EventSource` cannot call the API directly. The proxy exposes `GET /api/shopping-lists/{id}/events`, attaches the bearer, and streams the response without buffering. The browser then uses plain same-origin `EventSource`, which reconnects on its own.

Client rules come from the API's stream description: ignore an event whose `version` is at or below the cached item's version; refetch the list on every (re)open, on a version gap, and after `list_changed`; treat `list_deleted` as "not found" and navigate away. A proxied stream that the API closes (unlink, unshare, shutdown) ends on the browser side, and the reconnect gets `404` from the refetch.

## 5. App structure

Under `web/src`:

- `app/(auth)/login` and `register`; `app/(app)/today`, `plan`, `meals`, `meals/[id]`, `shopping`, `shopping/[id]`, `profile` (the parent spec's route list); `app/api/**` for the BFF.
- `features/{meals,plan,shopping,partner,profile}/`: query hooks and components per domain.
- `components/ui` (shadcn primitives) and `components/ingredient-search`, the one shared search component: type-ahead, category filter and a "create custom ingredient" fallback.
- `lib/api`: the typed client and a problem+json error type keyed on the stable `code`. `lib/nutrition`: the DV table and formatters.

**Data layer.** Query keys are per resource (`['meals', {cursor}]`, `['shopping-list', id]`, `['plan', from, to]`). Each mutation invalidates the keys it touches. Cursor lists use `useInfiniteQuery`.

**Optimistic updates** only where the parent spec asks for them: shopping check-off and quick-add, and plan swaps and portion changes. Each snapshots, applies, rolls back on error and refetches. A version conflict or an SSE version gap triggers a refetch, never a guess.

**Shared resources.** `is_owner: false` renders read-only with a "Copy to my library" action. The partner's meals and templates come from `GET partner/*`. The Meals page has "Mine" and "Partner's" tabs; the partner tab is hidden while there is no partner (`404 partner_not_linked`).

**Errors.** Field validation errors show inline; everything else is a toast. Both are driven by the problem `code`. Loading is a skeleton; empty states carry a call to action.

**Shell.** Desktop: sidebar and a split-pane meal editor. Phone: bottom tab bar. Full dark mode. Reduced motion is respected. Keyboard navigation, labelled controls and WCAG AA contrast (parent spec §5.3).

## 6. Nutrition UI

- **Summary tier:** kcal, protein, carbs and fat as rings against the profile targets. Each macro keeps one colour across the product.
- **Full tier:** an expandable panel with the 18 nutrients. Micronutrients show a percentage of the FDA Daily Value.
- **Unknown values.** The API returns `null` for a nutrient when any ingredient lacks it, and a meal with no ingredients reports 0. The UI shows "—" with a "some ingredients lack data" hint for `null`, and never renders it as 0.
- Meal nutrition is per serving; the client multiplies by `servings` for a meal total (backend contract).

## 7. Testing

- **Vitest and Testing Library.**
  - `lib/nutrition`: DV percentages and `null` handling, with golden values for a few known meals.
  - `lib/api`: problem+json mapping.
  - Proxy handlers: single-flight refresh (two concurrent expired requests make one refresh and both succeed), cookie flags, the `Origin` check, and cookie clearing on a failed refresh.
  - Hooks: optimistic check-off with rollback, and SSE ordering against a fake `EventSource` (stale-event drop, version-gap refetch).
  - Components: ingredient search and the nutrition panel.
- **Playwright, against the real API and Postgres.**
  1. Register, then log in.
  2. Build a meal.
  3. Apply a template to a date.
  4. Generate a shopping list.
  5. Check off a shared item across two browser contexts; the second sees it live (also proves the SSE proxy does not buffer).
  6. Unlink while the partner has the list open: the stream closes and the page shows not found.
  7. A session survives an expired access token.
- **Test data.** E2E creates custom ingredients through the API, so CI needs no `FDC_API_KEY`.
- **Rate limits.** The API's limits (`AuthPerMinute` 10 per IP, `UserPerMinute` 300, `AcceptPerHour` 10) are constants in `httpapi/ratelimit.go` and are not env-configurable. E2E runs with one worker to stay under them; if that proves too tight, the Foundation plan adds env overrides for the E2E stack.

## 8. Delivery

- **CI.** A path-filtered `web.yml` covers `web/**`, `openapi.yaml`, the Makefile and itself. Every run: typecheck, lint, Vitest and the generated-client drift check. A Playwright job builds the API, starts Postgres, runs `cmd/migrate`, starts the API and the Next.js build, runs the flows and uploads traces on failure. `backend.yml`'s `check-generated` step also covers the TS client, since `openapi.yaml` is shared.
- **Local dev.** `docker-compose.yml` gains `api` (built from `backend/`, migrations first, `TRUSTED_PROXY_COUNT=1`) and `web` services. New Makefile targets `lint-web`, `test-web`, `e2e-web` and `run-web`, all in `make check`. Demo seed data is a follow-up; USDA data already comes from `make import-usda`.
- **Docs.** New `web/CLAUDE.md`. The root `CLAUDE.md` gets the web repo-map and command entries. The parent spec gets §5.3 (BFF and proxy), §10 (reference values decided) and a note on the single-instance and `TRUSTED_PROXY_COUNT` requirements.

## 9. Backend and contract changes

None are required for the web app. Two small ones may appear inside the Foundation plan: the E2E rate-limit overrides from §7, and any `openapi.yaml` gap the typed client exposes (for example a missing response schema). Either follows the usual order: spec first, then `make generate`.

## 10. Plans

One spec, four plans, four PRs, in order:

1. **Foundation.** `web/` scaffold and `web/CLAUDE.md`; generated TS client and drift check; the BFF (auth routes, proxy, refresh single-flight, CSRF and client-IP handling); design tokens and the app shell; login and register; `web.yml`; Compose services and Makefile targets.
2. **Meals.** The shared ingredient search; the meal library and editor with live nutrition; the partner's shared meals and copy.
3. **Plan and Today.** The diet template editor; the week calendar and apply; Today with macro rings and one-tap swaps.
4. **Shopping and Profile.** Lists, generate, check-off and quick-add; the SSE hook and its proxy route; the partner connection UI; targets, custom ingredients and account deletion; the remaining Playwright flows.

Plans 2 to 4 are written after the previous plan merges.

## 11. Out of scope

Hosting, server-component data fetching, offline support, i18n, personalised reference values, multi-instance web deployment, and the iOS client.
