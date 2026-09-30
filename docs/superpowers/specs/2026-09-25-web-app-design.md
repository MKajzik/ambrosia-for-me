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

**Cookies.** Two httpOnly, `SameSite=Lax` cookies: the 15-minute access token, scoped to `/api` because only the API proxy reads it, and the 30-day refresh token, scoped to `/` because the page guard must see it on page requests. They are `Secure` in production builds; `COOKIE_SECURE=false` turns that off for a plain-http local stack (Docker Compose sets it).

**Routes** (Node runtime, server-only code):

- `POST /api/auth/login`, `/api/auth/register` and `/api/auth/logout` are custom handlers. They call the Go API and set or clear the cookies. The response body to the browser carries the user, never a token.
- `/api/[...path]` is a catch-all that forwards to `API_BASE_URL` (`http://localhost:8080/v1` in dev) with `Authorization: Bearer`. On an expired access token or a `401` it refreshes once and retries. If the refresh fails, it clears the cookies and returns `401`, and the UI redirects to `/login`.
- The page guard (`src/proxy.ts`, Next.js 16's renamed middleware) redirects a visitor with no refresh cookie to `/login?next=<page>`, and keeps a signed-in visitor off `/login` and `/register`. It only checks that the cookie exists; the first API call settles whether the session is still good and clears the cookies when it is not. `next` is only followed when it is an in-app path.
- Sign-out and a session the API reports as over both leave with a full page load, which drops every in-memory cache and cannot race a query that is about to refetch.

**Rotation race.** Replaying a used refresh token revokes the whole session family, so parallel requests must not each refresh. The proxy single-flights refresh per refresh token, in process, and keeps the resulting pair for about 30 seconds. A second in-flight request that still carries the old cookie gets the same new pair instead of replaying. This assumes one web instance in v1, which the deferred hosting decision has to respect; a multi-instance deploy needs a shared store first.

**CSRF.** The proxy and the auth routes reject a state-changing request unless its `Origin` header is present and its host equals the request's `Host`, and, when the request declares a content type at all, that type is JSON (a cross-site form cannot send it). Bodiless writes such as `DELETE` carry no content type and pass on the `Origin` check alone.

**Paths.** The catch-all refuses `auth/*` (only the three auth routes above reach the API's `/auth/*`) and any segment that is empty, `.`, `..` or contains a slash or backslash, so a request cannot climb out of `/v1`. Request bodies over 64 KiB (the API's own cap) are refused before they are buffered.

**Client IP.** The API rate-limits by client IP (auth per IP, and the partner accept limiter per IP), and with `TRUSTED_PROXY_COUNT=1` it takes the last `X-Forwarded-For` entry as the client. Next.js passes a client-supplied `X-Forwarded-For` through unchanged and never appends the real peer, so forwarding the header as received would let anyone dodge the limits by forging it. The BFF therefore trusts the header only as far as `WEB_TRUSTED_PROXY_COUNT` says: with N trusted proxies in front of the web server it takes the Nth entry from the right and sends that as the only `X-Forwarded-For` value, and with 0 (the default) it sends none, so the API sees the web server's own address. Consequence: without a trusted front proxy all users share one per-IP bucket, which is fine for development and E2E and not for a deployment; a deployment needs a front proxy that sets the header and `WEB_TRUSTED_PROXY_COUNT=1`, alongside the single-instance requirement above. Compose runs the API with `TRUSTED_PROXY_COUNT=1` and the web container with `WEB_TRUSTED_PROXY_COUNT=0`.

**Config.** `API_BASE_URL` is server-only and goes in `.env.example`. `WEB_ORIGIN` on the API side is not needed for browser calls, since they are same-origin, and stays as it is.

## 4. SSE through the proxy

The list stream requires the `Authorization` header and rejects tokens in the query string, so the browser's `EventSource` cannot call the API directly. The catch-all proxy already serves `GET /api/shopping-lists/{id}/events`: it attaches the bearer, streams the response without buffering, and cancels the API call when the browser goes away. The Foundation plan builds and tests that streaming behaviour; the `EventSource` hook itself belongs to the Shopping plan. The browser uses plain same-origin `EventSource`, which reconnects on its own.

Client rules come from the API's stream description: ignore an event whose `version` is at or below the cached item's version; refetch the list on every (re)open, on a version gap, and after `list_changed`; treat `list_deleted` as "not found" and navigate away. A proxied stream that the API closes (unlink, unshare, shutdown) ends on the browser side, and the reconnect gets `404` from the refetch.

## 5. App structure

Under `web/src`:

- `app/(auth)/login` and `register`; `app/(app)/today`, `plan`, `meals`, `meals/[id]`, `shopping`, `shopping/[id]`, `profile` (the parent spec's route list); `app/api/**` for the BFF.
- `features/{meals,plan,shopping,partner,profile}/`: query hooks and components per domain.
- `components/ui` (shadcn primitives) and `components/ingredient-search`, the one shared search component: type-ahead, category filter and a "create custom ingredient" fallback.
- `lib/api`: the typed client and a problem+json error type keyed on the stable `code`. `lib/nutrition`: the DV table and formatters.

**Data layer.** Query keys are per resource (`['meals', {cursor}]`, `['shopping-list', id]`, `['plan', from, to]`). Each mutation invalidates the keys it touches. Cursor lists use `useInfiniteQuery`.

**Optimistic updates** only where the parent spec asks for them: shopping check-off and quick-add, and plan swaps and portion changes. Each snapshots, applies, rolls back on error and refetches. A version conflict or an SSE version gap triggers a refetch, never a guess.

**Autosave.** The meal editor and the template editor have no Save button: they save 700 ms after the last edit (fields with `PATCH`, the ingredient or slot list with `PUT`). The meal editor shows the API's nutrition from the last save. The browser does not compute nutrition: a loaded meal carries only each ingredient's name and category, not its per-100 g data, and the API stays the one place the math lives.

**Plan and Today.** Dates are the person's local calendar day (`YYYY-MM-DD`), never UTC, and weeks start on Monday. Swaps, portion changes and slot clears are optimistic on the entries only: totals come from `GET /plan` and refetch when the write settles. The API addresses a snack only by date and slot (`PUT` adds, `DELETE` clears every snack of the day), so the plan offers "Add snack" and a confirmed "Clear snacks" and never edits one snack; template slots are replaced as a whole, so a template's snacks are editable. A partner's template is read-only; it is copied to change it or to apply it. Applying over days that already have meals asks before replacing (`409 plan_conflict`, then `overwrite: true`).

**Shopping and live lists.** A list is one query; check-off and quick-add are optimistic on it, and nothing else is. Shared lists stay live through a same-origin `EventSource`: the stream carries ids and versions, so the client refetches when an event is newer than what it holds or names an unknown item, and ignores an event at or below the cached version (its own changes echo back). A `404` on a list (unlinked, unshared, deleted) replaces it with "not available" even while items are cached. An item edit is saved against the version the form was opened on, so a concurrent change is a `409 version_conflict` (the other version comes back and the form restarts on it), never an overwrite, and a live update never wipes what is being typed. A checked item is attributed to the partner only when the partner checked it.

**Profile.** Targets are four optional numbers (a blank clears one; `0` calories is refused, `0` for a macro is allowed) and refresh Today and the plan's totals. The partner card shows an invite code once, when it is made, keeps it for the session while the invite is pending, and ends the link only after saying what ends. Custom ingredients are edited with all 18 nutrients sent, because the API replaces the whole set. Deleting the account needs the email typed and ends with a full page load.

**Shared resources.** `is_owner: false` renders read-only with a "Copy to my library" action. The partner's meals and templates come from `GET partner/*`. The Meals page has "Mine" and "Partner's" tabs; the partner tab is hidden while there is no partner (`404 partner_not_linked`).

**Errors.** Field validation errors show inline; everything else is a toast. Both are driven by the problem `code`. Loading is a skeleton; empty states carry a call to action.

**Shell.** Desktop: sidebar and a split-pane meal editor. Phone: bottom tab bar. Full dark mode. Reduced motion is respected. Keyboard navigation, labelled controls and WCAG AA contrast (parent spec §5.3).

## 6. Nutrition UI

- **Summary tier:** kcal, protein, carbs and fat against the profile targets. Each macro keeps one colour across the product. The Meals screens show the four as tiles (a meal has no target); the rings are on Today, against the targets `GET /plan` returns. The targets are set with `PATCH /me`; their Profile screen arrives with the Shopping and Profile plan.
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
- **Test data.** E2E creates custom ingredients through the API, so CI needs no `FDC_API_KEY`. Each test registers its own user.
- **Stack.** `make e2e-web` starts the full Compose stack under its own project name and ports (`mealplanner-e2e`, web on 53000), runs Playwright against it, and removes it and its volume afterwards, so it neither collides with a running dev stack nor touches its database.
- **Rate limits.** The API limits `/auth/*` to 10 requests per minute per client IP, and in the E2E stack every request comes from the web container. The Foundation plan therefore adds an optional `AUTH_RATE_LIMIT_PER_MINUTE` setting to the API (1 to 10000, unset keeps 10), which Docker Compose sets to 600. E2E runs with one worker. `UserPerMinute` (300) and `AcceptPerHour` (10) stay constants.

## 8. Delivery

- **CI.** A path-filtered `web.yml` covers `web/**`, `openapi.yaml`, the Makefile and itself. Every run: typecheck, lint, Vitest and the generated-client drift check. A Playwright job builds the API, starts Postgres, runs `cmd/migrate`, starts the API and the Next.js build, runs the flows and uploads traces on failure. `backend.yml`'s `check-generated` step also covers the TS client, since `openapi.yaml` is shared.
- **Local dev.** `docker-compose.yml` gains `migrate` (one-shot), `api` (built from a new `backend/Dockerfile`) and `web` (built from `web/Dockerfile`, Next.js standalone output) services with health checks; the web check goes through the proxy to the API's `/readyz`. New Makefile targets `generate-web`, `check-generated-web`, `lint-web`, `test-web`, `e2e-web` and `run-web`; `make check` runs `lint-web` and `test-web` (the E2E flows need Docker and run in CI's own job). Demo seed data is a follow-up; USDA data already comes from `make import-usda`.
- **Docs.** New `web/CLAUDE.md`. The root `CLAUDE.md` gets the web repo-map and command entries. The parent spec gets §5.3 (BFF and proxy), §10 (reference values decided) and a note on the single-instance and `TRUSTED_PROXY_COUNT` requirements.

## 9. Backend and contract changes

One, small and in the Foundation plan: the optional `AUTH_RATE_LIMIT_PER_MINUTE` setting from §7. It touches configuration only, not `openapi.yaml`. If the typed client later exposes an `openapi.yaml` gap (for example a missing response schema), that follows the usual order: spec first, then `make generate`.

## 10. Plans

One spec, four plans, four PRs, in order:

1. **Foundation.** `web/` scaffold and `web/CLAUDE.md`; generated TS types and drift check; the BFF (auth routes, API proxy with streaming, refresh single-flight, CSRF, path and client-IP handling); the page guard; design tokens and the app shell; login, register and sign-out; the `AUTH_RATE_LIMIT_PER_MINUTE` setting; Dockerfiles and Compose services; the auth Playwright flows (register, sign in and out, expired access token, parallel refresh); `web.yml` and Makefile targets.
2. **Meals.** The shared ingredient search; the meal library and editor with live nutrition; the partner's shared meals and copy.
3. **Plan and Today.** The diet template editor; the week calendar and apply; Today with macro rings and one-tap swaps.
4. **Shopping and Profile.** Lists, generate, check-off and quick-add; the SSE hook and its proxy route; the partner connection UI; targets, custom ingredients and account deletion; the remaining Playwright flows.

Plans 2 to 4 are written after the previous plan merges.

## 11. Out of scope

Hosting, server-component data fetching, offline support, i18n, personalised reference values, multi-instance web deployment, and the iOS client.

API gaps worked around in v1, to fix spec-first when they bite: no "mine only" filter on `GET /ingredients` (the custom ingredients page filters in the browser), and no entry-addressed plan endpoint (a single snack cannot be edited, only added or cleared).
