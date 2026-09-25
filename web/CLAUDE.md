# Web (Next.js)

Next.js 16 App Router, TypeScript strict, Tailwind 4, shadcn/ui (radix base), TanStack Query. Design: `docs/superpowers/specs/2026-09-25-web-app-design.md`. Read before changing auth, proxy or session behaviour.

## Layout

- `src/app/(auth)/{login,register}`, `src/app/(app)/{today,plan,meals,shopping,profile}`: pages. `(app)` pages are client components on TanStack Query; no server-side data fetching in v1.
- `src/app/api/`: BFF route handlers, thin re-exports. `auth/{login,register,logout}` plus catch-all `[...path]`.
- `src/server/`: BFF logic, `import "server-only"`, unit tested with fake `fetch`. `forward.ts` (API proxy), `auth.ts`, `auth-response.ts` (AuthResponse parser), `refresh.ts` (refresh single-flight), `body.ts` (bounded body reader), `relay.ts`, `cookies.ts`, `origin.ts` (CSRF), `client-ip.ts`, `upstream.ts`, `env.ts`.
- `src/proxy.ts`: page guard (Next 16 renamed `middleware.ts` to `proxy.ts`). Redirects on refresh-cookie presence only.
- `src/lib/api/`: `schema.gen.ts` (generated, never hand-edit), typed client `api` + `unwrap`, `ApiError` + `problemMessage`.
- `src/features/<area>/`: hooks + components per area. `src/components/`: shell, providers, `ui/` (shadcn).
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
