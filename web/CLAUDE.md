# Web (Next.js)

Next.js 16 App Router, TypeScript strict, Tailwind 4, shadcn/ui (radix base), TanStack Query. Design: `docs/superpowers/specs/2026-09-25-web-app-design.md`. Read before changing auth, proxy or session behaviour.

## Layout

- `src/app/(auth)/{login,register}`, `src/app/(app)/{today,plan,meals,shopping,profile}`: pages. `(app)` pages are client components on TanStack Query; no server-side data fetching in v1.
- `src/app/api/`: BFF route handlers, thin re-exports. `auth/{login,register,logout}` plus catch-all `[...path]`.
- `src/server/`: BFF logic, `import "server-only"`, unit tested with fake `fetch`. `forward.ts` (API proxy), `auth.ts`, `auth-response.ts` (AuthResponse parser), `refresh.ts` (refresh single-flight), `body.ts` (bounded body reader), `relay.ts`, `cookies.ts`, `origin.ts` (CSRF), `client-ip.ts`, `upstream.ts`, `env.ts`.
- `src/proxy.ts`: page guard (Next 16 renamed `middleware.ts` to `proxy.ts`). Redirects on refresh-cookie presence only.
- `src/lib/api/`: `schema.gen.ts` (generated, never hand-edit), typed client `api` + `unwrap`, `ApiError` + `problemMessage`.
- `src/features/<area>/`: hooks + components per area. `src/components/`: shell, providers, `ui/` (shadcn).
- `src/features/meals/`: `queries.ts` (hooks, keys `mealKeys`), `meal-list`/`meals-page` (library, Mine and Partner's tabs), `meal-editor` (autosave), `draft.ts` (pure validate + diff of the editor's draft), `use-autosave.ts`, `meal-view` (read-only partner meal), `meal-detail`, `new-meal-form`.
- `src/components/ingredient-search/`: the one shared ingredient search (type-ahead, category filter, custom-ingredient dialog). `src/components/nutrition-panel.tsx`: macro tiles + expandable 18-nutrient tier.
- `src/lib/nutrition/`: nutrient catalog, FDA Daily Values, formatters. `null` renders "—", never 0. `src/lib/parse-number.ts`: the one decimal parser (comma = decimal point, blank = nothing).
- `src/test/`: `fake-api.ts` (stubs `fetch`; routes like `"GET /meals/:id"`, throws on an unrouted call), `render.tsx`, `fixtures.ts`.
- `e2e/`: Playwright flows, run against the full Compose stack.

## BFF rules

- Browser never sees a token. Tokens = httpOnly cookies: `mp_access` (path `/api`), `mp_refresh` (path `/`, page guard needs it). Login/register/logout only via `/api/auth/*`; catch-all refuses `auth/*`.
- Refresh token rotates; replaying a used one revokes the session family. `createRefresher` shares one rotation per token in process and keeps result 30 s. Single web instance only until a shared store exists. Never call `/auth/refresh` any other way.
- Writes need same-origin `Origin` (host equals `Host`) and JSON content type when one is declared.
- Request bodies capped at 64 KiB while streaming (`body.ts`), never buffered first.
- Client IP for API per-IP limits: never forward raw `X-Forwarded-For` (Next passes client value through). Only `WEB_TRUSTED_PROXY_COUNT`-th entry from right, else none.
- Session over (API 401 after refresh, or logout): `hardNavigate` full page load, not `router.replace`; a mounted query would refetch and race.

## Commands

From repo root: `make lint-web` (typecheck + eslint), `make test-web` (Vitest), `make e2e-web` (Compose stack + Playwright, needs Docker), `make run-web` (dev server :3000, API on :8080 first), `make generate-web` (types from `openapi.yaml`), `make check-generated-web`.

## Gotchas

- Node 24 (`.nvmrc`). Vitest 5 rejects odd Node majors in `engines`.
- Pinned on purpose: TypeScript 5.9 (`openapi-typescript` peers `^5`), ESLint 9 (`eslint-plugin-react` in `eslint-config-next` breaks on 10). Check before bumping.
- Generate types from `web/`, not repo root: `openapi-typescript` reads root `redocly.yaml` lint rules and fails on `/healthz`.
- `openapi-fetch` reads response body itself: build errors from `error`, not `response.json()`. Client resolves `/api` against `window.location.origin` (`Request` needs absolute URL in jsdom/Node) and looks up `fetch` per call so tests can stub it.
- Component tests opt into jsdom with `// @vitest-environment jsdom`; server tests run in node.
- Next route announcer is a `role="alert"` on every page: narrow Playwright `getByRole("alert")` with text.
- shadcn: `npx shadcn@4.21.0 add <component>`. It imports `cn` from the `cn` package.
- Tokens (palette, radius, macro colours) live in `src/app/globals.css` only.
- The meal editor has no Save button: it autosaves 700 ms after the last edit (`PATCH` for fields, `PUT` for the ingredient list) and shows the API's `nutrition_per_serving` from the last save. Nutrition is not computed in the browser: a loaded meal carries no per-100 g data. The draft is copied from the meal once; background refetches only feed the nutrition panel.
- `Field` uses its `name` as the DOM id. Give fields in a dialog their own prefix (`ci-*` in the custom-ingredient form) so ids and `getByLabel` never collide with the page behind it.
- A native `<select>`'s `<option>`s have role `option` too: scope combobox tests to the listbox (`within(screen.getByRole("listbox"))`).
- Radix Dialog and Tabs need real pointer events in jsdom: open and switch them with `userEvent.click`, not `fireEvent`. Use `fireEvent.change` for text edits that must arrive as one change.
- E2E setup calls the API with `page.evaluate(fetch)` (`e2e/support.ts` `api`): the browser adds `Origin`, which the proxy's CSRF check requires and `page.request` does not, and a JSON body needs `content-type: application/json`.
