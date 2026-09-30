# Web (Next.js)

Next.js 16 App Router, TypeScript strict, Tailwind 4, shadcn/ui (radix base), TanStack Query. Design: `docs/superpowers/specs/2026-09-25-web-app-design.md`. Read before changing auth, proxy or session behaviour.

## Layout

- `src/app/(auth)/{login,register}`, `src/app/(app)/{today,plan,meals,shopping,profile}`: pages. `(app)` pages are client components on TanStack Query; no server-side data fetching in v1.
- `src/app/api/`: BFF route handlers, thin re-exports. `auth/{login,register,logout}` plus catch-all `[...path]`.
- `src/server/`: BFF logic, `import "server-only"`, unit tested with fake `fetch`. `forward.ts` (API proxy), `auth.ts`, `auth-response.ts` (AuthResponse parser), `refresh.ts` (refresh single-flight), `body.ts` (bounded body reader), `relay.ts`, `cookies.ts`, `origin.ts` (CSRF), `client-ip.ts`, `upstream.ts`, `env.ts`.
- `src/proxy.ts`: page guard (Next 16 renamed `middleware.ts` to `proxy.ts`). Redirects on refresh-cookie presence only.
- `src/lib/api/`: `schema.gen.ts` (generated, never hand-edit), typed client `api` + `unwrap`, `ApiError` + `problemMessage`.
- `src/features/<area>/`: hooks + components per area. `src/components/`: shell, providers, `ui/` (shadcn).
- `src/features/meals/`: `queries.ts` (hooks, keys `mealKeys`), `meal-list`/`meals-page` (library, Mine and Partner's tabs), `meal-editor` (autosave), `draft.ts` (pure validate + diff of the editor's draft), `meal-view` (read-only partner meal), `meal-detail`, `new-meal-form`.
- `src/features/plan/`: `queries.ts` (`usePlan`, optimistic `useSetPlanEntry` / `useClearPlanSlot`, `useApplyTemplate`), `plan-cache.ts` (pure optimistic edits of a cached plan, `SLOTS`), `today-view`, `plan-view` (week + totals), `apply-template-dialog`, `day-meals` / `slot-row` / `meal-picker` (shared by Today and Plan), `template-queries.ts`, `template-list` / `templates-page` / `new-template-form` / `template-view` / `template-detail` / `template-editor` (autosave), `template-draft.ts` (pure validate + diff). `src/components/macro-rings.tsx`: the four rings against targets.
- `src/features/shopping/`: `queries.ts` (lists, one list, optimistic `useCheckItem` / `useAddItem`, conflict-aware `useEditItem`), `list-cache.ts` (pure edits of a cached list, `isStaleEvent`), `list-events.ts` + `use-list-events.ts` (the SSE rules), `items.ts` (aisle grouping, quantity text), `shopping-page` / `shopping-lists` / `new-list-dialog` / `generate-list-dialog` / `range.ts`, `shopping-list-view` / `item-row` / `quick-add` / `edit-item-dialog` / `list-settings-dialog`.
- `src/features/profile/`: `targets.ts` + `targets-form`, `delete-account`. `src/features/partner/`: `partner-card` (invite code, enter a code, unlink), `invite-code.ts`. `src/features/ingredients/`: custom ingredient management (`edit-ingredient.ts` builds the update).
- `src/lib/dates.ts`: local calendar dates as `YYYY-MM-DD`, weeks start Monday. `src/lib/use-today.ts`, `src/lib/use-autosave.ts` (shared by both editors), `src/lib/use-load-all-pages.ts`.
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
- Dates are the person's local calendar day, `YYYY-MM-DD`. Never `toISOString()` (that is the UTC day: wrong for part of every day in most zones); use `src/lib/dates.ts`. Move by days with `addDays` (`setDate`), never by adding 24 h, so a daylight-saving change cannot skip or repeat a date. Tests build dates with `new Date(y, m, d, h)` (local), and fake only `Date` (`vi.useFakeTimers({ toFake: ["Date"] })`) so Testing Library's polling keeps working.
- Plan writes are optimistic on the entries only. Day and week totals come from `GET /plan` and refetch when the write settles; the rings and totals dim (`aria-busy`) meanwhile. The API addresses a snack only by date and slot (`PUT` adds, `DELETE` clears every snack of the day): the plan offers "Add snack" and a confirmed "Clear snacks" and never edits one snack. Template slots are replaced as a whole, so a template's snacks are editable.
- A partner's template is read-only and cannot be applied (`404`): copy it first. `POST /diet-templates/{id}/apply` answers `409 plan_conflict` unless `overwrite: true`; the dialog asks first.
- Targets (`target_kcal`, ...) come back on `GET /plan`; they are set with `PATCH /me` (the Profile screen for them arrives in the Shopping and Profile plan). A `null` or non-positive target is "No target set", never a ring at 0%.
- Put `onError` on the hook (`useMutation({ onError })`), not on `mutate(vars, { onError })`: the latter only fires for the latest call, so rapid taps would lose errors.
- In fake-API tests, make the fake server reflect a write: an optimistic edit is followed by a refetch, which restores the old state if the fake still serves it.
- Live lists are a plain same-origin `EventSource` on `/api/shopping-lists/{id}/events` (the proxy attaches the bearer). The stream says what changed, never to what, so `useListEvents` refetches instead of patching, except for an event at or below the cached item's version (often your own change: ignored) and a deleted item (removed). A closed connection (`readyState` 2) is a refusal: refetch, and the `404` says the list is gone. Tests drive it with `FakeEventSource` (`src/test/fake-event-source.ts`).
- A `404` from a list fetch replaces the list even while items are cached: access is gone (unlinked, unshared, deleted). Other load failures keep showing what is cached.
- Item edits go against the version the form was opened on, not the latest: someone else's change is then a `409 version_conflict`, whose `current` item is on `ApiError.body`, never a silent overwrite. Check-off needs no version. With two quick taps in flight an earlier answer never overrides the later tap (`useCheckItem` leaves the screen alone while another is pending).
- `PATCH /ingredients/{id}` with `nutrients` replaces the whole set. Anything that edits an ingredient through a form that shows only some nutrients must send all 18 (`edit-ingredient.ts`), or it silently erases the rest.
- `ApiError.body` is the whole problem document. Use it for members the UI does not model (`current`), never to parse messages.
- Irreversible actions ask in the UI even when the API does not (`DELETE /me` needs no re-authentication): the account's email is typed. Sign out and account deletion end with `hardNavigate`, a full page load.
- The API has no "only my custom ingredients" filter, so that view loads every page and filters in the browser (opt-in). If the catalogue grows, add a query parameter to `openapi.yaml` first.
