# Backend Auth Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Email/password accounts for the API: registration, login, rotating refresh-token sessions with reuse detection, logout, and the signed-in user's own profile (`/me`), enforced by the OpenAPI spec itself, with rate limiting, per-field validation errors, and a real users/refresh-tokens schema.

**Architecture:** `openapi.yaml` gains the auth and profile operations. A single request-validator middleware (driven by the spec's own `security` blocks) authenticates access tokens and validates request bodies, so a route added to the spec is protected by default. Handlers call an `auth.Service` (business rules) that uses a `store` (sqlc queries over goose-migrated `users` and `refresh_tokens` tables). Passwords use argon2id; access tokens are 15-minute HS256 JWTs; refresh tokens are opaque, hashed at rest, rotated on every use, and replaying a used one revokes the whole session family.

**Tech Stack:** Go 1.26, sqlc v1.31.1, golang-jwt v5.3.1, alexedwards/argon2id v1.0.0, oapi-codegen v2.8.0 (+ nullable v1.2.0, runtime v1.7.0), oapi-codegen/nethttp-middleware v1.2.0, go-chi/httprate v0.16.0, google/uuid v1.6.0; everything from the foundation plan (chi, pgx, goose, kin-openapi, testcontainers, golangci-lint v2.13.2).

**Spec:** `docs/superpowers/specs/2026-09-21-meal-planner-design.md` (sections 2.2, 3.1, 4.1 auth and `me` rows, 4.2, 6, 7). Builds on `docs/superpowers/plans/2026-09-21-backend-foundation.md` (merged to `master` in PR #1; its CI run passed).

## Execution notes: where execution deviated from the embedded code

Written after the plan was executed with subagent-driven development (each task reviewed; security-focused reviews on Tasks 4, 5, 7, 8, 9; a final whole-branch review). The code blocks in this document are what was prototyped and embedded. Reviews found real problems in some of them, and the implementation was corrected by ruling. **The repository history is the source of truth; where a block below differs from the code, the code wins.** Do not copy the superseded blocks.

| Task | Embedded block that was superseded | What changed and why | Commits |
|---|---|---|---|
| 1 | `openapi.yaml` | The seven auth/account operations now declare `500` (`Problem`), so the contract tests can exercise handler failures. Plan decision 2 ("contract tests validate every response") contradicted operations that emit 500 without declaring it. | 9134682 |
| 2 | `testutil/postgres.go` | The template database name is recorded only after `Migrate` succeeds (`templateErr`), so a failed template build fails later tests with the real error. | 65c4470 |
| 3 | migrations `00002`, `00003` | Amended in place (nothing deployed): explicit `users_email_key` / `users_apple_sub_key` names, `refresh_tokens_expires_at_idx`; `schema_test.go` asserts them. | 9134682 |
| 4 | `token_test.go`, `password` tests | Deterministic signature tamper (the original `[:len-2]+"xx"` was a no-op about 1 run in 1000), a positive control, and an in-package test that `Burn`'s dummy hash carries the hasher's parameters. | 65c4470 |
| 5 | `service/auth.go` `Logout`; `auth_test.go` | `Logout` runs lookup and family revoke in one transaction (the embedded version could return success while a concurrently rotated token stayed live). Added a concurrent-refresh test, a Logout-vs-Refresh race test and a deterministic row-lock test (a transaction holds the lock; `Refresh` must block). Carbs and fat targets are now asserted. Reuse-branch error wrapped. | eeb5d35, 4cceb1c, 65c4470 |
| 6 | `config.go`, `config_test.go` | Boundary tests (31/32 bytes, 10/11 proxies); the public dev JWT secret is refused unless `ALLOW_DEV_JWT_SECRET=1` (`make run-api` sets it). | 65c4470, 2b53491 |
| 7 | `clientip.go`, `clientip_test.go` | The client IP is canonicalised (lower-case IPv6, IPv4-mapped IPv6 to IPv4) and `rateLimitKey` buckets IPv6 by /64, because a limiter keyed on a full /128 address is dodged by rotating addresses inside the client's /64. | 7faf3ee |
| 8 | `validation.go`, `validation_test.go` | Missing or unsupported `Content-Type` is a 400, not a 500; body read errors are mapped explicitly (`request body is too large`); concrete-type switches instead of `errors.As` (which unwraps through `RequestError`); echoed unknown-field names capped at 64 runes and errors at 20; duplicates removed. | 8f8114d |
| 9 | `ratelimit.go`, `router.go`, `auth.go`, tests | `authIPLimiter` keys on `rateLimitKey(ClientIP(...))`; `SchemaErrorDetailsDisabled = true` (kin-openapi otherwise appends the offending value to error text); `bodyLimit` runs before the request logger; more than one `Authorization` header is rejected; `NewRouter` panics on an empty or `*` `WebOrigin`; extra tests (HEAD/OPTIONS 405, limiter path variants, per-user buckets, concurrency, "rejected requests do not count toward the user limit"). CORS exposes `X-RateLimit-*`. | e292ab1, d42b94c, 65c4470 |
| 9 to 10 | (ordering) | After Task 9, `cmd/api` tests fail: `NewRouter` now panics without `Auth`/`Tokens`, and `main.go` is wired only in Task 10 (whose RED step relies on that panic). Accepted as a one-task window; the better order wires `main.go` in Task 9. | e292ab1, dee3005 |
| 10 | none | | dee3005 |
| 11 | `contract_test.go`, `backend/CLAUDE.md`, `.env.example` | One `//nolint:gosec` on the test constant `validToken2` (added by a review fix before gosec was enabled); extra documentation bullets (rate-limit gaps, proxies, personal data, header and body edge cases); a `.env.example` warning about exporting an empty `JWT_SECRET`. | d60e56b |
| final | CI | `backend.yml` runs `go test -race ./...` (three tests exist only to catch races) with a 25-minute timeout. | 2b53491 |

Open design decisions (recorded in `backend/CLAUDE.md`, "Decide before the domain plans"): what happens to access tokens after logout or account deletion, whether `DELETE /me` needs re-authentication, whether registration should keep revealing that an email exists, and a memory bound on concurrent argon2id hashing.

## Global Constraints

- Backend is Go + Postgres. Layers are `handler` → `service` → `store`; no SQL outside `store`.
- All routes are under `/v1`, JSON only. Everything except `auth/*` and health checks requires a Bearer token.
- `openapi.yaml` is the single source of truth and is edited by hand. API changes update the spec first.
- Never hand-edit generated code (`backend/internal/api`, `backend/internal/store/sqlc`). CI fails if it drifts.
- Errors use RFC 9457 `application/problem+json` with a stable machine-readable `code`.
- Configuration is 12-factor via environment variables; `.env.example` documents each variable; secrets are never committed.
- Tests come first for service logic and bug fixes (TDD). Integration tests run against a real Postgres via `testcontainers`; no database mocks.
- Migrations use `goose`, are forward-only in production, and are applied by `cmd/migrate` before the new API version starts (never by the API itself). Every table has `created_at` and `updated_at`.
- Auth: JWT access tokens (15 min) + rotating refresh tokens; passwords use argon2id.
- Rate limiting: strict per-IP on `auth/*`, per-user elsewhere.
- Logging uses `slog` (JSON to stdout) with request IDs. Secrets, tokens, passwords and query strings never appear in logs or error responses.
- Go 1.26 (`go` directive stays `go 1.26` or `go 1.26.0`); golangci-lint v2.13.2.
- CI is path-filtered per package. Every new environment variable is added to `.env.example` in the same commit.

## Decisions this plan makes

1. **Auth enforcement is the request validator, driven by the spec.** `oapi-codegen` emits no per-route security information, so `nethttp-middleware`'s `OapiRequestValidator` (kin-openapi) runs on every routed request with an `AuthenticationFunc`. The global `bearerAuth` requirement protects every operation and `security: []` opts one out, so a route added later fails closed. Authentication failure wins over body-validation errors, so an unauthenticated caller learns nothing about a schema.
2. **Handlers stay non-strict** (`w, r`), not `strict-server`. The contract tests validate every response against the spec and the validator validates every request, which covers the drift a strict server would catch at compile time, without a typed response object per problem variant.
3. **Validation errors are per-field.** `Problem` gains an optional `errors: [{field, code}]`, filled from the schema validator (`required`, `too_short`, `too_long`, `invalid_format`, `invalid_type`, `out_of_range`, `unknown_field`, `invalid_value`). `format: email` is enforced by registering an email validator with kin-openapi.
4. **HS256 with a shared secret** (`JWT_SECRET`, at least 32 bytes) is enough for a single API service. Rotating the secret signs everyone out. Move to asymmetric keys only if another service must verify tokens.
5. **Refresh tokens:** 32 random bytes (base64url), stored as SHA-256, 30-day lifetime, one row per issued token sharing a `family_id`. A used or revoked token presented again revokes the whole family. Concurrent refreshes with one token therefore count as reuse; clients must serialize refreshes.
6. **Client IP:** `TRUSTED_PROXY_COUNT` (default 0) says how many proxies append to `X-Forwarded-For`; the client is the Nth entry from the right, so forged left-hand entries are ignored. With 0 the header is ignored entirely.
7. **Rate limits are in memory** (`httprate`): 10 auth requests per minute per client IP, 300 requests per minute per user. With several replicas the effective limit is per replica; move to a shared store when the API scales out.
8. **Sign in with Apple is not in this plan.** It needs Apple Developer credentials (Service ID, keys) to verify against, so it gets its own plan. `users.apple_sub` and the credential constraint are already in the schema so that plan needs no migration for the column.
9. **Numbers:** OpenAPI numbers use `format: double` (otherwise oapi-codegen generates `float32`); daily targets are `double precision` columns; PATCH uses `nullable.Nullable[float64]` so absent, `null` (clear) and a value are distinct.

## Scope notes

- Not in this plan: Sign in with Apple, email verification, password reset, deleting expired refresh tokens (scheduled cleanup), the domain (ingredients, meals, diets, plan, lists, partners).
- Docker must be running for `make test-backend`, `make check` and every database test. `make generate` downloads and builds `sqlc` and `oapi-codegen` on first use (a minute or two).
- Tasks 1 to 3 keep the repo green by temporarily embedding `api.Unimplemented` in `server` (Task 1 adds it, Task 9 removes it once every operation has a handler).
- Every task checks that `grep '^go ' backend/go.mod` still reads `go 1.26` or `go 1.26.0`. If a dependency raises it, stop and report.

## File Structure

| File | Responsibility |
|---|---|
| `openapi.yaml` | Auth and profile operations, `User`, request and response schemas, `Problem.errors`, shared error responses |
| `backend/internal/api/oapi.yaml`, `api.gen.go` | Generator config (adds embedded spec and nullable types) and generated code |
| `backend/internal/testutil/postgres.go` | Adds `NewMigratedDatabase` (template database per package) |
| `backend/migrations/00002_users.sql`, `00003_refresh_tokens.sql` | The two tables |
| `backend/sqlc.yaml`, `backend/internal/store/queries/*.sql`, `internal/store/sqlc/` | sqlc config, queries, generated code |
| `backend/internal/store/store.go` | `Store`: queries plus transactions and error helpers |
| `backend/internal/auth/password.go`, `token.go` | argon2id hashing, JWT access tokens, refresh-token generation and hashing |
| `backend/internal/service/auth.go` | Register, login, refresh, logout, get/update/delete user |
| `backend/internal/config/config.go` | Adds `JWT_SECRET` and `TRUSTED_PROXY_COUNT` |
| `backend/internal/httpapi/clientip.go` | Client IP from the peer and `X-Forwarded-For` |
| `backend/internal/httpapi/problem.go`, `validation.go` | Problem with field errors; kin-openapi errors to field errors |
| `backend/internal/httpapi/auth.go`, `ratelimit.go`, `account.go` | Validator middleware and auth state; rate limiters; account handlers |
| `backend/internal/httpapi/router.go`, `server.go`, `middleware.go` | Wiring, fail-fast `Deps`, log fields, body limit |
| `backend/cmd/api/main.go` | Wires the store, tokens and service; second-signal handling |
| `Makefile`, `.env.example`, `backend/.golangci.yml` | sqlc in `generate`, stronger `check-generated`, dev `JWT_SECRET`, new variables, extra linters |
| `CLAUDE.md`, `backend/CLAUDE.md`, `AGENTS.md` | Documentation |

Work on a new branch from the merged `master`: `git fetch origin && git checkout -b feat/backend-auth origin/master`.

---

### Task 1: Contract and code generation

**Files:**
- Modify: `openapi.yaml`
- Modify: `backend/internal/api/oapi.yaml`
- Modify (generated): `backend/internal/api/api.gen.go`
- Modify: `backend/internal/httpapi/server.go` (temporary embed)
- Modify: `backend/go.mod`, `backend/go.sum`

**Interfaces:**
- Produces: operations `registerUser` (`POST /auth/register`), `loginUser` (`POST /auth/login`), `refreshSession` (`POST /auth/refresh`), `logoutUser` (`POST /auth/logout`), `getMe` / `updateMe` / `deleteMe` (`/me`); schemas `User`, `RegisterRequest`, `LoginRequest`, `RefreshRequest`, `AuthResponse`, `UpdateProfileRequest`, `FieldError`, `Problem.errors`; responses `BadRequest`, `Unauthorized`, `Conflict`, `TooManyRequests`. Generated Go: `api.ServerInterface` with methods `RegisterUser`, `LoginUser`, `RefreshSession`, `LogoutUser`, `GetMe`, `UpdateMe`, `DeleteMe` (each `func(w http.ResponseWriter, r *http.Request)`), `api.GetSpec() (*openapi3.T, error)` (the embedded spec), `api.RegisterRequest{Email openapi_types.Email; Password, DisplayName string}`, `api.LoginRequest`, `api.RefreshRequest{RefreshToken string}`, `api.UpdateProfileRequest` (`DisplayName *string`, four `nullable.Nullable[float64]` targets), `api.User`, `api.AuthResponse`, `api.AuthResponseTokenTypeBearer`.

- [ ] **Step 1: Replace `openapi.yaml`**

```yaml
openapi: 3.0.3
info:
  title: Meal Planner API
  version: 0.1.0
  description: Contract for the Meal Planner API. This file is the single source of truth; the backend, web and iOS clients are generated from it.
servers:
  - url: http://localhost:8080/v1
    description: Local development
security:
  - bearerAuth: []
tags:
  - name: Health
    description: Liveness and readiness checks.
  - name: Auth
    description: Registration, login and refresh-token sessions.
  - name: Profile
    description: The signed-in user's own profile and account.
paths:
  /healthz:
    get:
      tags: [Health]
      operationId: getHealth
      summary: Liveness probe
      description: Returns 200 when the process is up. Does not touch the database.
      security: []
      responses:
        '200':
          description: The service is alive.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Health'
        '500':
          $ref: '#/components/responses/Problem'
  /readyz:
    get:
      tags: [Health]
      operationId: getReady
      summary: Readiness probe
      description: Returns 200 when the service can reach its database, 503 otherwise.
      security: []
      responses:
        '200':
          description: The service is ready to take traffic.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Health'
        '503':
          $ref: '#/components/responses/Problem'
  /auth/register:
    post:
      tags: [Auth]
      operationId: registerUser
      summary: Create an account
      description: Creates an account with email and password and signs the user in.
      security: []
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/RegisterRequest'
      responses:
        '201':
          description: The account was created.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/AuthResponse'
        '400':
          $ref: '#/components/responses/BadRequest'
        '409':
          $ref: '#/components/responses/Conflict'
        '429':
          $ref: '#/components/responses/TooManyRequests'
  /auth/login:
    post:
      tags: [Auth]
      operationId: loginUser
      summary: Sign in with email and password
      security: []
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/LoginRequest'
      responses:
        '200':
          description: The user is signed in.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/AuthResponse'
        '400':
          $ref: '#/components/responses/BadRequest'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '429':
          $ref: '#/components/responses/TooManyRequests'
  /auth/refresh:
    post:
      tags: [Auth]
      operationId: refreshSession
      summary: Exchange a refresh token for a new token pair
      description: Rotates the refresh token. Presenting an already-used refresh token revokes the whole session family.
      security: []
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/RefreshRequest'
      responses:
        '200':
          description: A new token pair.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/AuthResponse'
        '400':
          $ref: '#/components/responses/BadRequest'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '429':
          $ref: '#/components/responses/TooManyRequests'
  /auth/logout:
    post:
      tags: [Auth]
      operationId: logoutUser
      summary: Revoke a refresh-token session
      description: Idempotent. An unknown or already-revoked token is not an error.
      security: []
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/RefreshRequest'
      responses:
        '204':
          description: The session is revoked.
        '400':
          $ref: '#/components/responses/BadRequest'
        '429':
          $ref: '#/components/responses/TooManyRequests'
  /me:
    get:
      tags: [Profile]
      operationId: getMe
      summary: Get the signed-in user's profile
      responses:
        '200':
          description: The profile.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/User'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '429':
          $ref: '#/components/responses/TooManyRequests'
    patch:
      tags: [Profile]
      operationId: updateMe
      summary: Update the signed-in user's profile
      description: Fields that are absent are left unchanged. A daily target set to null is cleared.
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/UpdateProfileRequest'
      responses:
        '200':
          description: The updated profile.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/User'
        '400':
          $ref: '#/components/responses/BadRequest'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '429':
          $ref: '#/components/responses/TooManyRequests'
    delete:
      tags: [Profile]
      operationId: deleteMe
      summary: Delete the signed-in user's account
      description: Permanently deletes the account and everything it owns.
      responses:
        '204':
          description: The account was deleted.
        '401':
          $ref: '#/components/responses/Unauthorized'
        '429':
          $ref: '#/components/responses/TooManyRequests'
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
      bearerFormat: JWT
  schemas:
    Health:
      type: object
      required: [status]
      properties:
        status:
          type: string
          enum: [ok]
    Problem:
      type: object
      description: RFC 9457 problem details with a stable machine-readable code.
      required: [type, title, status, code]
      properties:
        type:
          type: string
          format: uri-reference
        title:
          type: string
        status:
          type: integer
        detail:
          type: string
        code:
          type: string
          description: Stable identifier clients map to localized text, e.g. partner_not_linked.
        errors:
          type: array
          description: Per-field details, present on validation_failed problems.
          items:
            $ref: '#/components/schemas/FieldError'
    FieldError:
      type: object
      required: [field, code]
      properties:
        field:
          type: string
          description: Path of the offending JSON field, e.g. `email`.
        code:
          type: string
          description: Stable identifier, e.g. required, too_short, too_long, invalid_format, invalid_type, invalid_value.
    User:
      type: object
      required: [id, email, display_name, created_at, updated_at]
      properties:
        id:
          type: string
          format: uuid
        email:
          type: string
        display_name:
          type: string
        target_kcal:
          type: number
          format: double
          nullable: true
        target_protein_g:
          type: number
          format: double
          nullable: true
        target_carbs_g:
          type: number
          format: double
          nullable: true
        target_fat_g:
          type: number
          format: double
          nullable: true
        created_at:
          type: string
          format: date-time
        updated_at:
          type: string
          format: date-time
    RegisterRequest:
      type: object
      additionalProperties: false
      required: [email, password, display_name]
      properties:
        email:
          type: string
          format: email
          maxLength: 254
        password:
          type: string
          minLength: 10
          maxLength: 128
        display_name:
          type: string
          minLength: 1
          maxLength: 80
    LoginRequest:
      type: object
      additionalProperties: false
      required: [email, password]
      properties:
        email:
          type: string
          format: email
          maxLength: 254
        password:
          type: string
          minLength: 1
          maxLength: 128
    RefreshRequest:
      type: object
      additionalProperties: false
      required: [refresh_token]
      properties:
        refresh_token:
          type: string
          minLength: 1
          maxLength: 512
    AuthResponse:
      type: object
      required: [access_token, refresh_token, token_type, expires_in, user]
      properties:
        access_token:
          type: string
        refresh_token:
          type: string
        token_type:
          type: string
          enum: [Bearer]
        expires_in:
          type: integer
          description: Access token lifetime in seconds.
        user:
          $ref: '#/components/schemas/User'
    UpdateProfileRequest:
      type: object
      additionalProperties: false
      properties:
        display_name:
          type: string
          minLength: 1
          maxLength: 80
        target_kcal:
          type: number
          format: double
          nullable: true
          exclusiveMinimum: true
          minimum: 0
          maximum: 20000
        target_protein_g:
          type: number
          format: double
          nullable: true
          minimum: 0
          maximum: 2000
        target_carbs_g:
          type: number
          format: double
          nullable: true
          minimum: 0
          maximum: 5000
        target_fat_g:
          type: number
          format: double
          nullable: true
          minimum: 0
          maximum: 2000
  responses:
    Problem:
      description: An error occurred.
      content:
        application/problem+json:
          schema:
            $ref: '#/components/schemas/Problem'
    BadRequest:
      description: The request is malformed or fails validation. Field details are in `errors`.
      content:
        application/problem+json:
          schema:
            $ref: '#/components/schemas/Problem'
    Unauthorized:
      description: Missing, invalid or expired credentials.
      headers:
        WWW-Authenticate:
          schema:
            type: string
      content:
        application/problem+json:
          schema:
            $ref: '#/components/schemas/Problem'
    Conflict:
      description: The request conflicts with existing state.
      content:
        application/problem+json:
          schema:
            $ref: '#/components/schemas/Problem'
    TooManyRequests:
      description: Rate limit exceeded.
      headers:
        Retry-After:
          schema:
            type: integer
      content:
        application/problem+json:
          schema:
            $ref: '#/components/schemas/Problem'
```

- [ ] **Step 2: Lint the contract**

Run: `make lint-api`
Expected: `Woohoo! Your API description is valid.` and `2 problems are explicitly ignored.`, no warnings or errors. (Every new operation declares a 4XX response, so the exemption list stays `/healthz` and `/readyz`.)

- [ ] **Step 3: Replace the generator config**

`backend/internal/api/oapi.yaml`:

```yaml
# Paths here are relative to backend/. Always run via `make generate` from the repo root.
package: api
output: internal/api/api.gen.go
generate:
  chi-server: true
  models: true
  embedded-spec: true
compatibility:
  always-prefix-enum-values: true
output-options:
  nullable-type: true
```

`embedded-spec` makes the generated file carry the spec (`api.GetSpec()`), which the request validator uses at runtime, so the binary does not need `openapi.yaml` on disk. `nullable-type` makes optional nullable fields `nullable.Nullable[T]`, which is what lets PATCH tell "absent" from "null".

- [ ] **Step 4: Add the runtime dependencies and regenerate**

```bash
cd backend && go get github.com/oapi-codegen/nullable@v1.2.0 github.com/oapi-codegen/runtime@v1.7.0 && cd ..
make generate
```
Expected: `backend/internal/api/api.gen.go` is rewritten. It must now contain `func GetSpec()`, `type ServerInterface interface` with the seven new methods (`RegisterUser`, `LoginUser`, `RefreshSession`, `LogoutUser`, `GetMe`, `UpdateMe`, `DeleteMe`), and in `UpdateProfileRequest` fields of type `nullable.Nullable[float64]`.

- [ ] **Step 5: Watch the contract-first build failure**

Run: `cd backend && go build ./... 2>&1 | head -5`
Expected: FAIL with `*server does not implement api.ServerInterface (missing method DeleteMe)` (and the other new methods). This is the intended safety net: a spec change breaks the build until every operation has a handler.

- [ ] **Step 6: Keep the repo green until the handlers exist (temporary)**

In `backend/internal/httpapi/server.go`, replace

```go
type server struct {
	logger *slog.Logger
	ready  func(context.Context) error
}
```

with

```go
type server struct {
	// TEMPORARY: answers 501 for operations whose handlers arrive in later
	// tasks. Task 9 removes this embed, so the build breaks again for any
	// operation that has no handler.
	api.Unimplemented
	logger *slog.Logger
	ready  func(context.Context) error
}
```

- [ ] **Step 7: Tidy, then verify everything still builds and passes**

Run: `cd backend && go mod tidy && grep '^go ' go.mod && gofmt -l . && go vet ./... && go test ./... -count=1`
Expected: go line `go 1.26`/`go 1.26.0`; `gofmt -l` prints nothing; every package `ok` (the existing tests, including the database-backed ones, still pass: nothing behavioural changed).

- [ ] **Step 8: Commit, then prove the drift check**

```bash
git add openapi.yaml backend
git commit -m "feat(api): add auth and account operations to the contract and regenerate"
make check-generated
```
Expected: exits 0.

Prove it fails on drift: temporarily add a property under `components.schemas.Health.properties` in `openapi.yaml`:

```yaml
        version:
          type: string
```

Run: `make check-generated`
Expected: exits non-zero with a diff for `backend/internal/api/api.gen.go`. Restore with `git checkout openapi.yaml backend/internal/api/api.gen.go`, run `make check-generated` again (exit 0), and confirm `git status --short` prints nothing.

---

### Task 2: Migrated test databases (TDD, needs Docker)

**Files:**
- Create: `backend/internal/testutil/postgres_test.go`
- Modify: `backend/internal/testutil/postgres.go`

**Interfaces:**
- Produces: `testutil.NewMigratedDatabase(t *testing.T) string`, the URL of a fresh database with every migration applied. Migrations run once per test package into a template database; each call copies it (`CREATE DATABASE x TEMPLATE y`), so a test costs milliseconds. Skips or fails without Docker exactly like `NewDatabase`. `NewDatabase` is unchanged in behaviour. Tasks 3, 5, 9 and 10 use it.

- [ ] **Step 1: Write the failing test**

```go
package testutil_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/InzKazik/mealplanner/backend/internal/testutil"
)

func connect(t *testing.T, url string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

func TestNewMigratedDatabaseIsMigratedAndIsolated(t *testing.T) {
	ctx := context.Background()
	urlA := testutil.NewMigratedDatabase(t)
	urlB := testutil.NewMigratedDatabase(t)
	if urlA == urlB {
		t.Fatal("two calls returned the same database URL")
	}
	a, b := connect(t, urlA), connect(t, urlB)

	// Migrated: the first migration's trigger function exists.
	var migrated bool
	if err := a.QueryRow(ctx, `SELECT to_regprocedure('set_updated_at()') IS NOT NULL`).Scan(&migrated); err != nil || !migrated {
		t.Fatalf("set_updated_at() missing in a migrated database (err %v)", err)
	}

	// Isolated: a table created in one database is invisible in the other.
	if _, err := a.Exec(ctx, `CREATE TABLE only_in_a (x int)`); err != nil {
		t.Fatalf("create table in a: %v", err)
	}
	var visibleInB bool
	if err := b.QueryRow(ctx, `SELECT to_regclass('public.only_in_a') IS NOT NULL`).Scan(&visibleInB); err != nil {
		t.Fatalf("check b: %v", err)
	}
	if visibleInB {
		t.Error("a table created in database a is visible in database b")
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

Run: `cd backend && go test ./internal/testutil/ -count=1`
Expected: FAIL to build with `undefined: testutil.NewMigratedDatabase`.

- [ ] **Step 3: Replace the helper**

```go
// Package testutil holds helpers shared by integration tests.
package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/InzKazik/mealplanner/backend/internal/db"
)

var (
	startOnce sync.Once
	adminURL  string
	startErr  error
)

// requireDocker skips the test when Docker is unavailable, except on CI (the
// CI environment variable is non-empty) where a missing Docker fails the test
// so integration tests cannot pass while testing nothing.
func requireDocker(t *testing.T) {
	t.Helper()
	if os.Getenv("CI") == "" {
		testcontainers.SkipIfProviderIsNotHealthy(t)
		return
	}
	provider, err := testcontainers.ProviderDocker.GetProvider()
	if err != nil {
		t.Fatalf("Docker is required on CI: %v", err)
	}
	if err := provider.Health(context.Background()); err != nil {
		t.Fatalf("Docker is required on CI: %v", err)
	}
}

func startContainer(t *testing.T) {
	t.Helper()
	requireDocker(t)
	startOnce.Do(func() {
		ctx := context.Background()
		var ctr *tcpostgres.PostgresContainer
		ctr, startErr = tcpostgres.Run(ctx, "postgres:17-alpine",
			tcpostgres.WithDatabase("postgres"),
			tcpostgres.WithUsername("test"),
			tcpostgres.WithPassword("test"),
			tcpostgres.BasicWaitStrategies(),
		)
		if startErr != nil {
			return
		}
		// The container is removed by testcontainers' reaper when the test
		// process exits.
		adminURL, startErr = ctr.ConnectionString(ctx, "sslmode=disable")
	})
	if startErr != nil {
		t.Fatalf("start postgres container: %v", startErr)
	}
}

func databaseURL(t *testing.T, name string) string {
	t.Helper()
	u, err := url.Parse(adminURL)
	if err != nil {
		t.Fatalf("parse admin url: %v", err)
	}
	u.Path = "/" + name
	return u.String()
}

// createDatabase creates a database named name (from template when it is not
// empty) and returns its URL.
func createDatabase(t *testing.T, name, template string) string {
	t.Helper()
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("connect to admin database: %v", err)
	}
	defer func() { _ = admin.Close(ctx) }()

	stmt := "CREATE DATABASE " + name
	if template != "" {
		stmt += " TEMPLATE " + template
	}
	if _, err := admin.Exec(ctx, stmt); err != nil {
		t.Fatalf("create database: %v", err)
	}
	return databaseURL(t, name)
}

func dropDatabase(name string) {
	ctx := context.Background()
	c, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return
	}
	defer func() { _ = c.Close(ctx) }()
	_, _ = c.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
}

// NewDatabase returns the URL of a fresh, empty database inside a Postgres
// container shared by every test in the package. The database is dropped when
// the test ends. The test is skipped when Docker is not available, except when
// the CI environment variable is set, where it fails.
func NewDatabase(t *testing.T) string {
	t.Helper()
	startContainer(t)

	name := "t_" + randomHex(t)
	dbURL := createDatabase(t, name, "")
	t.Cleanup(func() { dropDatabase(name) })
	return dbURL
}

var (
	templateOnce sync.Once
	templateName string
)

// NewMigratedDatabase returns the URL of a fresh database with every
// migration applied. Migrations run once per test package into a template
// database, and each call copies it, which is much cheaper than migrating per
// test. The database is dropped when the test ends. It skips or fails without
// Docker exactly like NewDatabase.
func NewMigratedDatabase(t *testing.T) string {
	t.Helper()
	startContainer(t)

	templateOnce.Do(func() {
		templateName = "tmpl_" + randomHex(t)
		tmplURL := createDatabase(t, templateName, "")
		if _, err := db.Migrate(context.Background(), tmplURL); err != nil {
			t.Fatalf("migrate template database: %v", err)
		}
	})
	if templateName == "" {
		t.Fatal("template database was not created by an earlier test")
	}

	name := "t_" + randomHex(t)
	dbURL := createDatabase(t, name, templateName)
	t.Cleanup(func() { dropDatabase(name) })
	return dbURL
}

func randomHex(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random: %v", err)
	}
	return hex.EncodeToString(b)
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `cd backend && gofmt -l . && go vet ./... && go test ./internal/testutil/ ./internal/db/ -count=1 -v 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: `--- PASS: TestNewMigratedDatabaseIsMigratedAndIsolated` (about 3 seconds: it starts Postgres once), and the db package tests still `PASS` (not `SKIP`).

- [ ] **Step 5: Commit**

```bash
git add backend
git commit -m "test(backend): add migrated template databases for integration tests"
```

---

### Task 3: Users and refresh-token schema, sqlc queries and the store (TDD, needs Docker)

**Files:**
- Create: `backend/internal/db/schema_test.go`
- Create: `backend/migrations/00002_users.sql`, `backend/migrations/00003_refresh_tokens.sql`
- Create: `backend/sqlc.yaml`
- Create: `backend/internal/store/queries/users.sql`, `backend/internal/store/queries/refresh_tokens.sql`
- Create (generated): `backend/internal/store/sqlc/*.go`
- Create: `backend/internal/store/store.go`
- Modify: `Makefile`, `backend/go.mod`, `backend/go.sum`

**Interfaces:**
- Consumes: `testutil.NewMigratedDatabase` (Task 2).
- Produces:
  - Tables `users` (`id uuid`, `email citext unique`, `password_hash text null`, `apple_sub text unique null`, `display_name`, four nullable `double precision` targets, timestamps; a credential is required: `password_hash IS NOT NULL OR apple_sub IS NOT NULL`) and `refresh_tokens` (`id`, `user_id` cascading, `family_id`, `token_hash bytea unique`, `expires_at`, `revoked_at`, timestamps). Both have `updated_at` triggers using `set_updated_at()`.
  - Package `sqlc` (generated): `sqlc.Queries` with `CreateUser`, `GetUserByEmail`, `GetUserByID`, `UpdateUserProfile`, `DeleteUser` (returns rows affected), `CreateRefreshToken`, `GetRefreshTokenByHashForUpdate`, `RevokeRefreshToken`, `RevokeRefreshTokenFamily`; models `sqlc.User` and `sqlc.RefreshToken` (`uuid.UUID`, `time.Time`, pointers for nullable columns).
  - `store.Store` (embeds `*sqlc.Queries`), `store.New(pool *pgxpool.Pool) *Store`, `(*Store).InTx(ctx, fn func(q *sqlc.Queries) error) error`, `store.IsNotFound(err) bool`, `store.IsUniqueViolation(err, constraint string) bool`.
  - `make generate` also runs sqlc; `make check-generated` also covers `backend/internal/store/sqlc` and fails when a generated file is not committed.

- [ ] **Step 1: Write the failing schema test**

```go
package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/InzKazik/mealplanner/backend/internal/testutil"
)

func migratedConn(t *testing.T) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), testutil.NewMigratedDatabase(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	return conn
}

func TestUsersSchemaEnforcesItsConstraints(t *testing.T) {
	ctx := context.Background()
	conn := migratedConn(t)
	insert := func(sql string, args ...any) error {
		_, err := conn.Exec(ctx, "INSERT INTO users (email, password_hash, apple_sub, display_name, target_kcal) VALUES "+sql, args...)
		return err
	}

	if err := insert(`('a@example.com', 'hash', NULL, 'A', 2000)`); err != nil {
		t.Fatalf("valid insert: %v", err)
	}
	tests := []struct {
		name string
		sql  string
	}{
		{"email is unique case-insensitively", `('A@EXAMPLE.COM', 'hash', NULL, 'B', NULL)`},
		{"a credential is required", `('c@example.com', NULL, NULL, 'C', NULL)`},
		{"kcal target must be positive", `('d@example.com', 'hash', NULL, 'D', 0)`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := insert(tt.sql); err == nil {
				t.Error("insert succeeded, want a constraint violation")
			}
		})
	}
	if err := insert(`('e@example.com', NULL, 'apple-sub-1', 'E', NULL)`); err != nil {
		t.Errorf("an Apple-only account (no password hash) was rejected: %v", err)
	}
	if err := insert(`('f@example.com', NULL, 'apple-sub-1', 'F', NULL)`); err == nil {
		t.Error("two accounts with the same apple_sub were accepted")
	}
}

func TestUpdatedAtTriggerFires(t *testing.T) {
	ctx := context.Background()
	conn := migratedConn(t)
	var id string
	var created, updated time.Time
	err := conn.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, display_name) VALUES ('a@example.com', 'h', 'A') RETURNING id, created_at, updated_at`,
	).Scan(&id, &created, &updated)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if !updated.Equal(created) {
		t.Fatalf("updated_at %v differs from created_at %v on insert", updated, created)
	}

	// now() is frozen inside a transaction, so wait between statements.
	time.Sleep(20 * time.Millisecond)
	if err := conn.QueryRow(ctx, `UPDATE users SET display_name = 'B' WHERE id = $1 RETURNING updated_at`, id).Scan(&updated); err != nil {
		t.Fatalf("update: %v", err)
	}

	if !updated.After(created) {
		t.Errorf("updated_at %v was not advanced past created_at %v by the trigger", updated, created)
	}
}

func TestRefreshTokensCascadeWithTheirUser(t *testing.T) {
	ctx := context.Background()
	conn := migratedConn(t)
	var userID string
	if err := conn.QueryRow(ctx, `INSERT INTO users (email, password_hash, display_name) VALUES ('a@example.com', 'h', 'A') RETURNING id`).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO refresh_tokens (user_id, family_id, token_hash, expires_at) VALUES ($1, gen_random_uuid(), '\x01', now() + interval '1 day')`, userID); err != nil {
		t.Fatalf("insert refresh token: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO refresh_tokens (user_id, family_id, token_hash, expires_at) VALUES ($1, gen_random_uuid(), '\x01', now() + interval '1 day')`, userID); err == nil {
		t.Error("a duplicate token_hash was accepted, want a unique violation")
	}

	if _, err := conn.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	var n int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM refresh_tokens`).Scan(&n); err != nil || n != 0 {
		t.Errorf("refresh_tokens rows after deleting the user = %d (err %v), want 0", n, err)
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

Run: `cd backend && go test ./internal/db/ -run 'TestUsersSchema|TestUpdatedAt|TestRefreshTokens' -count=1`
Expected: FAIL. The `users` and `refresh_tokens` tables do not exist (`relation "users" does not exist`).

- [ ] **Step 3: Write the migrations**

`backend/migrations/00002_users.sql`:

```sql
-- +goose Up
CREATE TABLE users (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email            citext NOT NULL UNIQUE,
    password_hash    text,
    apple_sub        text UNIQUE,
    display_name     text NOT NULL,
    target_kcal      double precision CHECK (target_kcal > 0),
    target_protein_g double precision CHECK (target_protein_g >= 0),
    target_carbs_g   double precision CHECK (target_carbs_g >= 0),
    target_fat_g     double precision CHECK (target_fat_g >= 0),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_has_credential CHECK (password_hash IS NOT NULL OR apple_sub IS NOT NULL)
);

CREATE TRIGGER users_set_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE users;
```

`backend/migrations/00003_refresh_tokens.sql`:

```sql
-- +goose Up
CREATE TABLE refresh_tokens (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    family_id   uuid NOT NULL,
    token_hash  bytea NOT NULL UNIQUE,
    expires_at  timestamptz NOT NULL,
    revoked_at  timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX refresh_tokens_user_id_idx ON refresh_tokens (user_id);
CREATE INDEX refresh_tokens_family_id_idx ON refresh_tokens (family_id);

CREATE TRIGGER refresh_tokens_set_updated_at
    BEFORE UPDATE ON refresh_tokens
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE refresh_tokens;
```

- [ ] **Step 4: Run the schema tests and confirm they pass**

Run: `cd backend && go test ./internal/db/ -count=1 -v 2>&1 | grep -E '^(\s*--- |ok|FAIL)'`
Expected: every test `PASS`, including `TestMigrateAppliesAndIsIdempotent` (it now expects 3 migrations, counted from the embedded files, with no edit needed) and the new schema tests.

- [ ] **Step 5: Write the sqlc config and queries**

`backend/sqlc.yaml`:

```yaml
version: "2"
sql:
  - engine: postgresql
    schema: migrations
    queries: internal/store/queries
    gen:
      go:
        package: sqlc
        out: internal/store/sqlc
        sql_package: pgx/v5
        emit_pointers_for_null_types: true
        overrides:
          - db_type: uuid
            go_type: github.com/google/uuid.UUID
          - db_type: uuid
            nullable: true
            go_type:
              import: github.com/google/uuid
              type: UUID
              pointer: true
          - db_type: timestamptz
            go_type: time.Time
          - db_type: timestamptz
            nullable: true
            go_type:
              import: time
              type: Time
              pointer: true
```

`backend/internal/store/queries/users.sql`:

```sql
-- name: CreateUser :one
INSERT INTO users (email, password_hash, display_name)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: UpdateUserProfile :one
UPDATE users SET
    display_name     = COALESCE(sqlc.narg('display_name'), display_name),
    target_kcal      = CASE WHEN sqlc.arg('set_target_kcal')::boolean THEN sqlc.narg('target_kcal') ELSE target_kcal END,
    target_protein_g = CASE WHEN sqlc.arg('set_target_protein_g')::boolean THEN sqlc.narg('target_protein_g') ELSE target_protein_g END,
    target_carbs_g   = CASE WHEN sqlc.arg('set_target_carbs_g')::boolean THEN sqlc.narg('target_carbs_g') ELSE target_carbs_g END,
    target_fat_g     = CASE WHEN sqlc.arg('set_target_fat_g')::boolean THEN sqlc.narg('target_fat_g') ELSE target_fat_g END
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteUser :execrows
DELETE FROM users WHERE id = $1;
```

`backend/internal/store/queries/refresh_tokens.sql`:

```sql
-- name: CreateRefreshToken :one
INSERT INTO refresh_tokens (user_id, family_id, token_hash, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetRefreshTokenByHashForUpdate :one
SELECT * FROM refresh_tokens WHERE token_hash = $1 FOR UPDATE;

-- name: RevokeRefreshToken :exec
UPDATE refresh_tokens SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeRefreshTokenFamily :exec
UPDATE refresh_tokens SET revoked_at = now() WHERE family_id = $1 AND revoked_at IS NULL;
```

The `UpdateUserProfile` query takes a boolean per nullable target (`set_target_*`) so the caller can say "leave unchanged", "clear" or "set", which a single nullable parameter cannot express.

- [ ] **Step 6: Update the Makefile (adds sqlc and the stronger drift check)**

Recipe lines **must** start with a real tab character.

```makefile
.DEFAULT_GOAL := help
REDOCLY := npx --yes @redocly/cli@2.53.3
GOLANGCI := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2
OAPICODEGEN := go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0
SQLC := go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
GENERATED := backend/internal/api backend/internal/store/sqlc

# Local development database; matches the docker-compose.yml defaults.
migrate run-api: export DATABASE_URL ?= postgres://mealplanner:mealplanner@localhost:5432/mealplanner?sslmode=disable

.PHONY: help lint-api test-backend lint-backend generate check-generated migrate run-api db-up db-down check

help: ## List available targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-16s %s\n", $$1, $$2}'

lint-api: ## Lint openapi.yaml
	$(REDOCLY) lint openapi.yaml

test-backend: ## Vet and test the Go backend (needs Docker)
	cd backend && go vet ./... && go test ./...

lint-backend: ## Run golangci-lint on the Go backend
	cd backend && $(GOLANGCI) run ./...

generate: ## Regenerate backend code (oapi-codegen from openapi.yaml, sqlc from migrations and queries)
	cd backend && $(OAPICODEGEN) -config internal/api/oapi.yaml ../openapi.yaml
	cd backend && $(SQLC) generate

check-generated: generate ## Fail if the committed generated code is out of date
	git add -AN -- $(GENERATED)
	git diff --exit-code -- $(GENERATED)

migrate: ## Apply database migrations to DATABASE_URL
	cd backend && go run ./cmd/migrate

run-api: ## Run the API locally on :8080
	cd backend && go run ./cmd/api

db-up: ## Start local Postgres and wait until healthy
	docker compose up -d --wait postgres

db-down: ## Stop local Postgres (data is kept)
	docker compose down

check: lint-api test-backend lint-backend check-generated ## Everything CI runs, except the compose workflow
```

- [ ] **Step 7: Add the dependency, generate, and write the store**

```bash
cd backend && go get github.com/google/uuid@v1.6.0 && cd ..
make generate
```
Expected: `backend/internal/store/sqlc/` now contains `db.go`, `models.go`, `refresh_tokens.sql.go`, `users.sql.go`. `models.go` must define `User` with `ID uuid.UUID`, `Email string`, `PasswordHash *string`, `TargetKcal *float64`, `CreatedAt time.Time` and `RefreshToken` with `TokenHash []byte`, `RevokedAt *time.Time`.

`backend/internal/store/store.go`:

```go
// Package store is the only package that talks SQL. The generated queries live
// in the sqlc subpackage; Store adds transactions on top of them.
package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"
)

// Store wraps the generated queries and a connection pool.
type Store struct {
	*sqlc.Queries
	pool *pgxpool.Pool
}

// New returns a Store backed by pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{Queries: sqlc.New(pool), pool: pool}
}

// InTx runs fn in a transaction. It commits when fn returns nil and rolls back
// otherwise.
func (s *Store) InTx(ctx context.Context, fn func(q *sqlc.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after a successful commit

	if err := fn(s.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

// IsNotFound reports whether err means a query matched no row.
func IsNotFound(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

// IsUniqueViolation reports whether err is a unique-constraint violation on
// the named constraint.
func IsUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}
```

- [ ] **Step 8: Tidy and verify**

Run: `cd backend && go mod tidy && grep '^go ' go.mod && gofmt -l . && go vet ./... && go test ./... -count=1`
Expected: go line `go 1.26`/`go 1.26.0`; gofmt prints nothing; every package `ok`.

- [ ] **Step 9: Commit, then prove the drift check covers sqlc**

```bash
git add Makefile backend
git commit -m "feat(backend): add users and refresh-token schema, sqlc queries and the store"
make check-generated
```
Expected: exits 0.

Prove it fails on stale sqlc output: append to `backend/internal/store/queries/users.sql`

```sql

-- name: CountUsers :one
SELECT count(*) FROM users;
```

then run `make check-generated`. Expected: exits non-zero with a diff adding `CountUsers`. Restore with `git checkout backend/internal/store`, run `git clean -fd backend/internal/store`, and confirm `make check-generated` exits 0 again and `git status --short` prints nothing.

Prove it fails on a generated file that is not committed: `git rm --cached backend/internal/store/sqlc/models.go`, run `make check-generated` (expected: exits non-zero), then `git reset -q --hard HEAD` and confirm `make check-generated` exits 0 and `git status --short` prints nothing.

---

### Task 4: Passwords and tokens (TDD)

**Files:**
- Create: `backend/internal/auth/password_test.go`, `backend/internal/auth/token_test.go`
- Create: `backend/internal/auth/password.go`, `backend/internal/auth/token.go`
- Modify: `backend/go.mod`, `backend/go.sum`

**Interfaces:**
- Produces (package `auth`, no database or HTTP dependency):
  - `auth.HashParams{MemoryKiB uint32; Iterations uint32; Parallelism uint8}`, `auth.DefaultHashParams` (64 MiB, 3 passes, 2 lanes), `auth.NewHasher(HashParams) *Hasher`, `(*Hasher).Hash(password) (string, error)` (PHC argon2id string, random salt), `(*Hasher).Verify(password, hash) (bool, error)` (error only for a malformed hash), `(*Hasher).Burn(password)` (same cost as `Verify`, for unknown users).
  - `auth.MinSecretLength` (32), `auth.ErrInvalidAccessToken`, `auth.NewTokenIssuer(secret []byte, ttl time.Duration, now func() time.Time) *TokenIssuer` (panics on a short secret), `(*TokenIssuer).IssueAccess(userID uuid.UUID) (token string, ttl time.Duration, err error)`, `(*TokenIssuer).ParseAccess(token string) (uuid.UUID, error)` (HS256 only; issuer, expiry and a UUID subject required; every failure is `ErrInvalidAccessToken`).
  - `auth.NewRefreshToken() (raw string, hash []byte, err error)` (32 random bytes, base64url; SHA-256 hash) and `auth.HashRefreshToken(raw string) []byte`.

- [ ] **Step 1: Add the dependencies**

```bash
cd backend && go get github.com/golang-jwt/jwt/v5@v5.3.1 github.com/alexedwards/argon2id@v1.0.0
```

- [ ] **Step 2: Write the failing tests**

`backend/internal/auth/password_test.go`:

```go
package auth_test

import (
	"strings"
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/auth"
)

// lightParams keeps the tests fast; production uses auth.DefaultHashParams.
var lightParams = auth.HashParams{MemoryKiB: 8, Iterations: 1, Parallelism: 1}

func TestHasherRoundTrip(t *testing.T) {
	h := auth.NewHasher(lightParams)

	hash, err := h.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Errorf("hash = %q, want an argon2id PHC string", hash)
	}

	ok, err := h.Verify("correct horse battery staple", hash)
	if err != nil || !ok {
		t.Errorf("Verify(correct password) = %v, %v; want true, nil", ok, err)
	}
	ok, err = h.Verify("wrong password!", hash)
	if err != nil || ok {
		t.Errorf("Verify(wrong password) = %v, %v; want false, nil", ok, err)
	}
}

func TestHasherUsesRandomSalt(t *testing.T) {
	h := auth.NewHasher(lightParams)

	a, _ := h.Hash("same password 123")
	b, _ := h.Hash("same password 123")

	if a == b {
		t.Error("two hashes of the same password are identical, want distinct salts")
	}
}

func TestHasherVerifyRejectsMalformedHash(t *testing.T) {
	h := auth.NewHasher(lightParams)

	ok, err := h.Verify("whatever", "not-a-hash")

	if ok || err == nil {
		t.Errorf("Verify(malformed) = %v, %v; want false and an error", ok, err)
	}
}

func TestHasherBurnDoesNotPanicAndTakesTheHashingPath(t *testing.T) {
	h := auth.NewHasher(lightParams)

	// Burn is called for unknown users so that login timing does not reveal
	// whether an email is registered. It must simply complete.
	h.Burn("any password at all")
}
```

`backend/internal/auth/token_test.go`:

```go
package auth_test

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/auth"
)

var testSecret = []byte("0123456789abcdef0123456789abcdef") // 32 bytes

func newIssuer(now func() time.Time) *auth.TokenIssuer {
	return auth.NewTokenIssuer(testSecret, 15*time.Minute, now)
}

func TestAccessTokenRoundTrip(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	iss := newIssuer(func() time.Time { return now })
	id := uuid.New()

	token, ttl, err := iss.IssueAccess(id)
	if err != nil {
		t.Fatalf("IssueAccess: %v", err)
	}
	if ttl != 15*time.Minute {
		t.Errorf("ttl = %v, want 15m", ttl)
	}

	got, err := iss.ParseAccess(token)
	if err != nil {
		t.Fatalf("ParseAccess: %v", err)
	}
	if got != id {
		t.Errorf("ParseAccess subject = %v, want %v", got, id)
	}
}

func TestParseAccessRejectsBadTokens(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	iss := newIssuer(func() time.Time { return now })
	id := uuid.New()
	good, _, _ := iss.IssueAccess(id)

	sign := func(method jwt.SigningMethod, key any, claims jwt.RegisteredClaims) string {
		s, err := jwt.NewWithClaims(method, claims).SignedString(key)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		return s
	}
	valid := jwt.RegisteredClaims{
		Issuer: "mealplanner", Subject: id.String(),
		IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)),
	}
	expired := valid
	expired.ExpiresAt = jwt.NewNumericDate(now.Add(-time.Second))
	noExpiry := valid
	noExpiry.ExpiresAt = nil
	wrongIssuer := valid
	wrongIssuer.Issuer = "someone-else"
	badSubject := valid
	badSubject.Subject = "not-a-uuid"

	tests := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"garbage", "not.a.jwt"},
		{"tampered signature", good[:len(good)-2] + "xx"},
		{"expired", sign(jwt.SigningMethodHS256, testSecret, expired)},
		{"no expiry", sign(jwt.SigningMethodHS256, testSecret, noExpiry)},
		{"wrong issuer", sign(jwt.SigningMethodHS256, testSecret, wrongIssuer)},
		{"subject not a uuid", sign(jwt.SigningMethodHS256, testSecret, badSubject)},
		{"signed with another key", sign(jwt.SigningMethodHS256, []byte("another-secret-another-secret-00"), valid)},
		{"alg none", sign(jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, valid)},
		{"wrong algorithm HS512", sign(jwt.SigningMethodHS512, testSecret, valid)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := iss.ParseAccess(tt.token); err == nil {
				t.Errorf("ParseAccess(%q) succeeded, want an error", tt.name)
			}
		})
	}
}

func TestParseAccessRejectsExpiredTokenAfterTimePasses(t *testing.T) {
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	clock := now
	iss := newIssuer(func() time.Time { return clock })
	token, _, _ := iss.IssueAccess(uuid.New())

	clock = now.Add(15*time.Minute + time.Second)

	if _, err := iss.ParseAccess(token); err == nil {
		t.Error("token accepted after its lifetime, want an error")
	}
}

func TestNewRefreshTokenIsRandomAndHashable(t *testing.T) {
	raw1, hash1, err := auth.NewRefreshToken()
	if err != nil {
		t.Fatalf("NewRefreshToken: %v", err)
	}
	raw2, _, _ := auth.NewRefreshToken()

	if raw1 == raw2 {
		t.Error("two refresh tokens are identical")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw1)
	if err != nil || len(decoded) != 32 {
		t.Errorf("raw token = %q: want 32 random bytes in base64url (decode err %v, len %d)", raw1, err, len(decoded))
	}
	if string(auth.HashRefreshToken(raw1)) != string(hash1) {
		t.Error("HashRefreshToken(raw) differs from the hash returned with the token")
	}
	if len(hash1) != 32 {
		t.Errorf("hash length = %d, want 32 (SHA-256)", len(hash1))
	}
}

func TestNewTokenIssuerPanicsOnShortSecret(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("NewTokenIssuer accepted a secret shorter than 32 bytes, want a panic")
		}
	}()
	auth.NewTokenIssuer([]byte("too short"), time.Minute, time.Now)
}
```

- [ ] **Step 3: Run them and confirm they fail**

Run: `cd backend && go test ./internal/auth/ -count=1`
Expected: FAIL to build with `no non-test Go files` (or `undefined: auth.NewHasher`).

- [ ] **Step 4: Write the implementation**

`backend/internal/auth/password.go`:

```go
// Package auth holds the pure credential logic: password hashing and token
// issuing/parsing. It has no database or HTTP dependency.
package auth

import (
	"fmt"

	"github.com/alexedwards/argon2id"
)

// HashParams are the argon2id cost parameters.
type HashParams struct {
	MemoryKiB   uint32
	Iterations  uint32
	Parallelism uint8
}

// DefaultHashParams are the production parameters: 64 MiB, 3 passes, 2 lanes.
var DefaultHashParams = HashParams{MemoryKiB: 64 * 1024, Iterations: 3, Parallelism: 2}

// Hasher hashes and verifies passwords with argon2id.
type Hasher struct {
	params    *argon2id.Params
	dummyHash string
}

// NewHasher returns a Hasher using p.
func NewHasher(p HashParams) *Hasher {
	params := &argon2id.Params{
		Memory:      p.MemoryKiB,
		Iterations:  p.Iterations,
		Parallelism: p.Parallelism,
		SaltLength:  16,
		KeyLength:   32,
	}
	// A hash computed with the same parameters, used by Burn so that checking a
	// password for an unknown user costs the same as for a known one.
	dummy, err := argon2id.CreateHash("dummy-password-for-timing", params)
	if err != nil {
		panic(fmt.Sprintf("auth: create dummy hash: %v", err)) // only fails if the system RNG is broken
	}
	return &Hasher{params: params, dummyHash: dummy}
}

// Hash returns the PHC-encoded argon2id hash of password with a random salt.
func (h *Hasher) Hash(password string) (string, error) {
	hash, err := argon2id.CreateHash(password, h.params)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return hash, nil
}

// Verify reports whether password matches hash. It returns an error only when
// hash is malformed.
func (h *Hasher) Verify(password, hash string) (bool, error) {
	ok, err := argon2id.ComparePasswordAndHash(password, hash)
	if err != nil {
		return false, fmt.Errorf("verify password: %w", err)
	}
	return ok, nil
}

// Burn spends the same time as Verify without a real hash. Call it when the
// account does not exist, so response time does not reveal which emails exist.
func (h *Hasher) Burn(password string) {
	_, _ = argon2id.ComparePasswordAndHash(password, h.dummyHash)
}
```

`backend/internal/auth/token.go`:

```go
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	issuer = "mealplanner"
	// MinSecretLength is the shortest accepted HS256 signing secret, in bytes.
	MinSecretLength = 32
)

// ErrInvalidAccessToken is returned by ParseAccess for any token that is not a
// currently valid access token. Callers must not distinguish the reason.
var ErrInvalidAccessToken = errors.New("invalid access token")

// TokenIssuer issues and parses short-lived HS256 access tokens.
type TokenIssuer struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

// NewTokenIssuer returns an issuer. It panics when secret is shorter than
// MinSecretLength: configuration is validated before this is reached.
func NewTokenIssuer(secret []byte, ttl time.Duration, now func() time.Time) *TokenIssuer {
	if len(secret) < MinSecretLength {
		panic(fmt.Sprintf("auth: token secret must be at least %d bytes", MinSecretLength))
	}
	return &TokenIssuer{secret: secret, ttl: ttl, now: now}
}

// IssueAccess returns a signed access token for userID and its lifetime.
func (t *TokenIssuer) IssueAccess(userID uuid.UUID) (string, time.Duration, error) {
	now := t.now()
	claims := jwt.RegisteredClaims{
		Issuer:    issuer,
		Subject:   userID.String(),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(t.ttl)),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
	if err != nil {
		return "", 0, fmt.Errorf("sign access token: %w", err)
	}
	return signed, t.ttl, nil
}

// ParseAccess validates token (signature, algorithm, issuer, expiry) and
// returns the user ID it was issued for.
func (t *TokenIssuer) ParseAccess(token string) (uuid.UUID, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(issuer),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(t.now),
	)
	claims := &jwt.RegisteredClaims{}
	_, err := parser.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) { return t.secret, nil })
	if err != nil {
		return uuid.Nil, ErrInvalidAccessToken
	}
	id, err := uuid.Parse(claims.Subject)
	if err != nil {
		return uuid.Nil, ErrInvalidAccessToken
	}
	return id, nil
}

// NewRefreshToken returns a random opaque refresh token (32 bytes,
// base64url) and the SHA-256 hash that is stored in the database.
func NewRefreshToken() (raw string, hash []byte, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, fmt.Errorf("generate refresh token: %w", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, HashRefreshToken(raw), nil
}

// HashRefreshToken returns the SHA-256 hash under which a refresh token is stored.
func HashRefreshToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}
```

- [ ] **Step 5: Run the tests and confirm they pass**

Run: `cd backend && go mod tidy && grep '^go ' go.mod && gofmt -l . && go vet ./internal/auth/ && go test ./internal/auth/ -count=1 -v 2>&1 | grep -E '^(\s*--- |ok|FAIL)'`
Expected: go line unchanged; every test `PASS`, including all ten subtests of `TestParseAccessRejectsBadTokens` (tampered, expired, no expiry, wrong issuer, bad subject, foreign key, `alg: none`, HS512).

- [ ] **Step 6: Commit**

```bash
git add backend
git commit -m "feat(backend): add argon2id password hashing and JWT access and refresh tokens"
```

---

### Task 5: Auth service (TDD, needs Docker)

**Files:**
- Create: `backend/internal/service/auth_test.go`
- Create: `backend/internal/service/auth.go`

**Interfaces:**
- Consumes: `store.Store`, `sqlc.Queries` (Task 3), `auth.Hasher`, `auth.TokenIssuer`, `auth.NewRefreshToken`, `auth.HashRefreshToken` (Task 4), `testutil.NewMigratedDatabase` (Task 2).
- Produces (package `service`): errors `ErrEmailTaken`, `ErrInvalidCredentials`, `ErrInvalidRefreshToken`, `ErrNotFound`; types `User{ID uuid.UUID; Email, DisplayName string; TargetKcal, TargetProteinG, TargetCarbsG, TargetFatG *float64; CreatedAt, UpdatedAt time.Time}`, `Session{AccessToken, RefreshToken string; ExpiresIn time.Duration; User User}`, `RegisterInput{Email, Password, DisplayName string}`, `Optional[T]{Specified bool; Value *T}` with `Set[T any](v *T) Optional[T]` (a nil `Value` clears the column), `UpdateInput{DisplayName *string; TargetKcal, TargetProteinG, TargetCarbsG, TargetFatG Optional[float64]}`; `service.NewAuth(st *store.Store, hasher *auth.Hasher, tokens *auth.TokenIssuer, refreshTTL time.Duration, now func() time.Time) *Auth` with methods `Register(ctx, RegisterInput) (Session, error)`, `Login(ctx, email, password) (Session, error)`, `Refresh(ctx, rawToken) (Session, error)`, `Logout(ctx, rawToken) error`, `GetUser(ctx, id) (User, error)`, `UpdateUser(ctx, id, UpdateInput) (User, error)`, `DeleteUser(ctx, id) error`.
- Behaviour that later tasks rely on: registration creates the user and the first refresh token in one transaction; `Login` answers an unknown email, an Apple-only account and a wrong password with the same `ErrInvalidCredentials` and spends the same hashing time; `Refresh` rotates the token and, when handed a token that was already revoked, revokes its whole family and still returns `ErrInvalidRefreshToken`; `Logout` revokes the whole family and is idempotent; a missing user is `ErrNotFound`.

- [ ] **Step 1: Write the failing tests**

```go
package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/auth"
	"github.com/InzKazik/mealplanner/backend/internal/db"
	"github.com/InzKazik/mealplanner/backend/internal/service"
	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/testutil"
)

type fixture struct {
	svc   *service.Auth
	store *store.Store
	clock *time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool, err := db.Connect(context.Background(), testutil.NewMigratedDatabase(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	clock := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	f := &fixture{store: store.New(pool), clock: &clock}
	now := func() time.Time { return *f.clock }
	f.svc = service.NewAuth(f.store,
		auth.NewHasher(auth.HashParams{MemoryKiB: 8, Iterations: 1, Parallelism: 1}),
		auth.NewTokenIssuer([]byte("0123456789abcdef0123456789abcdef"), 15*time.Minute, now),
		30*24*time.Hour, now)
	return f
}

func register(t *testing.T, f *fixture, email string) service.Session {
	t.Helper()
	s, err := f.svc.Register(context.Background(), service.RegisterInput{
		Email: email, Password: "a-long-enough-password", DisplayName: "Test User",
	})
	if err != nil {
		t.Fatalf("Register(%s): %v", email, err)
	}
	return s
}

func TestRegisterReturnsSessionAndStoresUser(t *testing.T) {
	f := newFixture(t)

	s := register(t, f, "Alice@Example.com")

	if s.AccessToken == "" || s.RefreshToken == "" || s.ExpiresIn != 15*time.Minute {
		t.Errorf("session = %+v, want tokens and a 15m lifetime", s)
	}
	if s.User.Email != "Alice@Example.com" || s.User.DisplayName != "Test User" {
		t.Errorf("user = %+v", s.User)
	}
	if s.User.TargetKcal != nil {
		t.Errorf("new user has a kcal target %v, want none", *s.User.TargetKcal)
	}
	stored, err := f.store.GetUserByID(context.Background(), s.User.ID)
	if err != nil {
		t.Fatalf("stored user: %v", err)
	}
	if stored.PasswordHash == nil || *stored.PasswordHash == "a-long-enough-password" {
		t.Error("password was not hashed before storing")
	}
}

func TestRegisterRejectsDuplicateEmailCaseInsensitively(t *testing.T) {
	f := newFixture(t)
	register(t, f, "alice@example.com")

	_, err := f.svc.Register(context.Background(), service.RegisterInput{
		Email: "ALICE@example.com", Password: "another-long-password", DisplayName: "Other",
	})

	if !errors.Is(err, service.ErrEmailTaken) {
		t.Errorf("err = %v, want ErrEmailTaken", err)
	}
}

func TestLogin(t *testing.T) {
	f := newFixture(t)
	register(t, f, "alice@example.com")

	tests := []struct {
		name, email, password string
		wantErr               error
	}{
		{"correct credentials", "alice@example.com", "a-long-enough-password", nil},
		{"email is case-insensitive", "ALICE@example.com", "a-long-enough-password", nil},
		{"wrong password", "alice@example.com", "not-the-password", service.ErrInvalidCredentials},
		{"unknown email", "nobody@example.com", "a-long-enough-password", service.ErrInvalidCredentials},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := f.svc.Login(context.Background(), tt.email, tt.password)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && (s.AccessToken == "" || s.RefreshToken == "") {
				t.Errorf("session missing tokens: %+v", s)
			}
		})
	}
}

func TestRefreshRotatesAndDetectsReuse(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	first := register(t, f, "alice@example.com")

	second, err := f.svc.Refresh(ctx, first.RefreshToken)
	if err != nil {
		t.Fatalf("first Refresh: %v", err)
	}
	if second.RefreshToken == first.RefreshToken {
		t.Error("refresh token was not rotated")
	}

	// Replaying the old token is theft: it must fail and revoke the whole family.
	if _, err := f.svc.Refresh(ctx, first.RefreshToken); !errors.Is(err, service.ErrInvalidRefreshToken) {
		t.Errorf("replayed token: err = %v, want ErrInvalidRefreshToken", err)
	}
	if _, err := f.svc.Refresh(ctx, second.RefreshToken); !errors.Is(err, service.ErrInvalidRefreshToken) {
		t.Errorf("newest token after reuse: err = %v, want ErrInvalidRefreshToken (family revoked)", err)
	}
}

func TestRefreshRejectsUnknownAndExpiredTokens(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := register(t, f, "alice@example.com")

	if _, err := f.svc.Refresh(ctx, "definitely-not-a-real-token"); !errors.Is(err, service.ErrInvalidRefreshToken) {
		t.Errorf("unknown token: err = %v, want ErrInvalidRefreshToken", err)
	}

	*f.clock = f.clock.Add(31 * 24 * time.Hour)
	if _, err := f.svc.Refresh(ctx, s.RefreshToken); !errors.Is(err, service.ErrInvalidRefreshToken) {
		t.Errorf("expired token: err = %v, want ErrInvalidRefreshToken", err)
	}
}

func TestLogoutRevokesTheSessionAndIsIdempotent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := register(t, f, "alice@example.com")

	if err := f.svc.Logout(ctx, s.RefreshToken); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := f.svc.Refresh(ctx, s.RefreshToken); !errors.Is(err, service.ErrInvalidRefreshToken) {
		t.Errorf("Refresh after Logout: err = %v, want ErrInvalidRefreshToken", err)
	}
	if err := f.svc.Logout(ctx, s.RefreshToken); err != nil {
		t.Errorf("second Logout: %v, want nil (idempotent)", err)
	}
	if err := f.svc.Logout(ctx, "unknown-token"); err != nil {
		t.Errorf("Logout(unknown token): %v, want nil", err)
	}
}

func TestUpdateUserAppliesOnlySpecifiedFields(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := register(t, f, "alice@example.com")
	kcal, protein := 2200.0, 150.0

	u, err := f.svc.UpdateUser(ctx, s.User.ID, service.UpdateInput{
		DisplayName:    ptr("Alice"),
		TargetKcal:     service.Set(&kcal),
		TargetProteinG: service.Set(&protein),
	})
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if u.DisplayName != "Alice" || u.TargetKcal == nil || *u.TargetKcal != 2200 || u.TargetProteinG == nil || *u.TargetProteinG != 150 {
		t.Errorf("after first update: %+v", u)
	}

	// Only the kcal target is specified (cleared): the others must stay.
	u, err = f.svc.UpdateUser(ctx, s.User.ID, service.UpdateInput{TargetKcal: service.Set[float64](nil)})
	if err != nil {
		t.Fatalf("second UpdateUser: %v", err)
	}
	if u.TargetKcal != nil {
		t.Errorf("kcal target = %v, want cleared", *u.TargetKcal)
	}
	if u.TargetProteinG == nil || *u.TargetProteinG != 150 || u.DisplayName != "Alice" {
		t.Errorf("unspecified fields changed: %+v", u)
	}
}

func TestGetUpdateDeleteUserNotFound(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	missing := uuid.New()

	if _, err := f.svc.GetUser(ctx, missing); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("GetUser: err = %v, want ErrNotFound", err)
	}
	if _, err := f.svc.UpdateUser(ctx, missing, service.UpdateInput{DisplayName: ptr("x")}); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("UpdateUser: err = %v, want ErrNotFound", err)
	}
	if err := f.svc.DeleteUser(ctx, missing); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("DeleteUser: err = %v, want ErrNotFound", err)
	}
}

func TestDeleteUserRemovesAccountAndSessions(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := register(t, f, "alice@example.com")

	if err := f.svc.DeleteUser(ctx, s.User.ID); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}

	if _, err := f.svc.GetUser(ctx, s.User.ID); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("GetUser after delete: err = %v, want ErrNotFound", err)
	}
	if _, err := f.svc.Refresh(ctx, s.RefreshToken); !errors.Is(err, service.ErrInvalidRefreshToken) {
		t.Errorf("Refresh after delete: err = %v, want ErrInvalidRefreshToken", err)
	}
	if _, err := f.svc.Login(ctx, "alice@example.com", "a-long-enough-password"); !errors.Is(err, service.ErrInvalidCredentials) {
		t.Errorf("Login after delete: err = %v, want ErrInvalidCredentials", err)
	}
}

func ptr[T any](v T) *T { return &v }
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `cd backend && go test ./internal/service/ -count=1`
Expected: FAIL to build with `no non-test Go files`.

- [ ] **Step 3: Write the implementation**

```go
// Package service holds business rules. It calls the store and never speaks HTTP.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/auth"
	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"
)

// Errors returned by Auth. Handlers map them to problem responses.
var (
	ErrEmailTaken          = errors.New("email is already registered")
	ErrInvalidCredentials  = errors.New("invalid email or password")
	ErrInvalidRefreshToken = errors.New("invalid refresh token")
	ErrNotFound            = errors.New("not found")
)

// User is an account as the rest of the application sees it.
type User struct {
	ID             uuid.UUID
	Email          string
	DisplayName    string
	TargetKcal     *float64
	TargetProteinG *float64
	TargetCarbsG   *float64
	TargetFatG     *float64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// Session is a signed-in user with a fresh token pair.
type Session struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    time.Duration
	User         User
}

// RegisterInput is the data needed to create an account.
type RegisterInput struct {
	Email       string
	Password    string
	DisplayName string
}

// Optional distinguishes "leave unchanged" (zero value) from "set to Value",
// where a nil Value clears a nullable column.
type Optional[T any] struct {
	Specified bool
	Value     *T
}

// Set returns an Optional that assigns v (nil clears the column).
func Set[T any](v *T) Optional[T] { return Optional[T]{Specified: true, Value: v} }

// UpdateInput is a partial profile update: unspecified fields are unchanged.
type UpdateInput struct {
	DisplayName    *string
	TargetKcal     Optional[float64]
	TargetProteinG Optional[float64]
	TargetCarbsG   Optional[float64]
	TargetFatG     Optional[float64]
}

// Auth implements registration, login, refresh-token sessions and the
// signed-in user's own account.
type Auth struct {
	st         *store.Store
	hasher     *auth.Hasher
	tokens     *auth.TokenIssuer
	refreshTTL time.Duration
	now        func() time.Time
}

// NewAuth returns an Auth service. now is injected so tests control time.
func NewAuth(st *store.Store, hasher *auth.Hasher, tokens *auth.TokenIssuer, refreshTTL time.Duration, now func() time.Time) *Auth {
	return &Auth{st: st, hasher: hasher, tokens: tokens, refreshTTL: refreshTTL, now: now}
}

// Register creates an account and signs it in.
func (a *Auth) Register(ctx context.Context, in RegisterInput) (Session, error) {
	hash, err := a.hasher.Hash(in.Password)
	if err != nil {
		return Session{}, err
	}

	var sess Session
	err = a.st.InTx(ctx, func(q *sqlc.Queries) error {
		user, err := q.CreateUser(ctx, sqlc.CreateUserParams{
			Email: in.Email, PasswordHash: &hash, DisplayName: in.DisplayName,
		})
		if store.IsUniqueViolation(err, "users_email_key") {
			return ErrEmailTaken
		}
		if err != nil {
			return fmt.Errorf("create user: %w", err)
		}
		sess, err = a.newSession(ctx, q, user, uuid.New())
		return err
	})
	if err != nil {
		return Session{}, err
	}
	return sess, nil
}

// Login checks an email and password and starts a new session.
func (a *Auth) Login(ctx context.Context, email, password string) (Session, error) {
	user, err := a.st.GetUserByEmail(ctx, email)
	if store.IsNotFound(err) {
		a.hasher.Burn(password)
		return Session{}, ErrInvalidCredentials
	}
	if err != nil {
		return Session{}, fmt.Errorf("get user: %w", err)
	}
	if user.PasswordHash == nil { // an account that only signs in with Apple
		a.hasher.Burn(password)
		return Session{}, ErrInvalidCredentials
	}
	ok, err := a.hasher.Verify(password, *user.PasswordHash)
	if err != nil {
		return Session{}, err
	}
	if !ok {
		return Session{}, ErrInvalidCredentials
	}
	return a.newSession(ctx, a.st.Queries, user, uuid.New())
}

// Refresh exchanges a refresh token for a new token pair and revokes the old
// token. Presenting a token that was already used or revoked is treated as
// theft: the whole session family is revoked.
func (a *Auth) Refresh(ctx context.Context, rawToken string) (Session, error) {
	var (
		sess   Session
		reused bool
	)
	err := a.st.InTx(ctx, func(q *sqlc.Queries) error {
		rt, err := q.GetRefreshTokenByHashForUpdate(ctx, auth.HashRefreshToken(rawToken))
		if store.IsNotFound(err) {
			return ErrInvalidRefreshToken
		}
		if err != nil {
			return fmt.Errorf("get refresh token: %w", err)
		}
		if rt.RevokedAt != nil {
			// Commit the family revocation, then report the failure.
			reused = true
			return q.RevokeRefreshTokenFamily(ctx, rt.FamilyID)
		}
		if !rt.ExpiresAt.After(a.now()) {
			return ErrInvalidRefreshToken
		}
		if err := q.RevokeRefreshToken(ctx, rt.ID); err != nil {
			return fmt.Errorf("revoke refresh token: %w", err)
		}
		user, err := q.GetUserByID(ctx, rt.UserID)
		if err != nil {
			return fmt.Errorf("get user: %w", err)
		}
		sess, err = a.newSession(ctx, q, user, rt.FamilyID)
		return err
	})
	if err != nil {
		return Session{}, err
	}
	if reused {
		return Session{}, ErrInvalidRefreshToken
	}
	return sess, nil
}

// Logout revokes the whole session that rawToken belongs to. It is idempotent:
// an unknown or already-revoked token is not an error.
func (a *Auth) Logout(ctx context.Context, rawToken string) error {
	rt, err := a.st.GetRefreshTokenByHashForUpdate(ctx, auth.HashRefreshToken(rawToken))
	if store.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get refresh token: %w", err)
	}
	if err := a.st.RevokeRefreshTokenFamily(ctx, rt.FamilyID); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

// GetUser returns the account with the given ID.
func (a *Auth) GetUser(ctx context.Context, id uuid.UUID) (User, error) {
	u, err := a.st.GetUserByID(ctx, id)
	if store.IsNotFound(err) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("get user: %w", err)
	}
	return toUser(u), nil
}

// UpdateUser applies a partial profile update.
func (a *Auth) UpdateUser(ctx context.Context, id uuid.UUID, in UpdateInput) (User, error) {
	u, err := a.st.UpdateUserProfile(ctx, sqlc.UpdateUserProfileParams{
		ID:                id,
		DisplayName:       in.DisplayName,
		SetTargetKcal:     in.TargetKcal.Specified,
		TargetKcal:        in.TargetKcal.Value,
		SetTargetProteinG: in.TargetProteinG.Specified,
		TargetProteinG:    in.TargetProteinG.Value,
		SetTargetCarbsG:   in.TargetCarbsG.Specified,
		TargetCarbsG:      in.TargetCarbsG.Value,
		SetTargetFatG:     in.TargetFatG.Specified,
		TargetFatG:        in.TargetFatG.Value,
	})
	if store.IsNotFound(err) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("update user: %w", err)
	}
	return toUser(u), nil
}

// DeleteUser permanently deletes the account and, through cascading foreign
// keys, everything that belongs to it.
func (a *Auth) DeleteUser(ctx context.Context, id uuid.UUID) error {
	n, err := a.st.DeleteUser(ctx, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (a *Auth) newSession(ctx context.Context, q *sqlc.Queries, user sqlc.User, family uuid.UUID) (Session, error) {
	access, ttl, err := a.tokens.IssueAccess(user.ID)
	if err != nil {
		return Session{}, err
	}
	raw, hash, err := auth.NewRefreshToken()
	if err != nil {
		return Session{}, err
	}
	_, err = q.CreateRefreshToken(ctx, sqlc.CreateRefreshTokenParams{
		UserID: user.ID, FamilyID: family, TokenHash: hash, ExpiresAt: a.now().Add(a.refreshTTL),
	})
	if err != nil {
		return Session{}, fmt.Errorf("store refresh token: %w", err)
	}
	return Session{AccessToken: access, RefreshToken: raw, ExpiresIn: ttl, User: toUser(user)}, nil
}

func toUser(u sqlc.User) User {
	return User{
		ID: u.ID, Email: u.Email, DisplayName: u.DisplayName,
		TargetKcal: u.TargetKcal, TargetProteinG: u.TargetProteinG,
		TargetCarbsG: u.TargetCarbsG, TargetFatG: u.TargetFatG,
		CreatedAt: u.CreatedAt, UpdatedAt: u.UpdatedAt,
	}
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `cd backend && gofmt -l . && go vet ./internal/service/ && go test ./internal/service/ -count=1 -v 2>&1 | grep -E '^(\s*--- |ok|FAIL)'`
Expected: every test `PASS` (not `SKIP`): registration, case-insensitive duplicate email, login (four subtests), refresh rotation and reuse detection, unknown and expired tokens, logout idempotence, partial profile updates, not-found handling, and account deletion with its sessions.

- [ ] **Step 5: Commit**

```bash
git add backend
git commit -m "feat(backend): add the auth service with rotating refresh tokens"
```

---

### Task 6: Configuration for tokens and proxies (TDD)

**Files:**
- Modify: `backend/internal/config/config_test.go` (full replacement)
- Modify: `backend/internal/config/config.go`

**Interfaces:**
- Consumes: `auth.MinSecretLength` (Task 4).
- Produces: `config.Config` gains `JWTSecret string` (env `JWT_SECRET`, required, at least 32 bytes; the value never appears in an error) and `TrustedProxies int` (env `TRUSTED_PROXY_COUNT`, default 0, integer 0 to 10). Task 10 consumes them.

- [ ] **Step 1: Replace the tests**

```go
package config_test

import (
	"strings"
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/config"
)

const secret = "0123456789abcdef0123456789abcdef" // 32 bytes

// base is a valid environment; each test case overrides or removes ("") keys.
func envWith(overrides map[string]string) func(string) string {
	m := map[string]string{"DATABASE_URL": "postgres://x", "JWT_SECRET": secret}
	for k, v := range overrides {
		m[k] = v
	}
	return func(k string) string { return m[k] }
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name      string
		overrides map[string]string
		want      config.Config
		wantErr   string
		also      string // second substring the error must contain
	}{
		{
			name: "defaults applied",
			want: config.Config{Addr: ":8080", DatabaseURL: "postgres://x", WebOrigin: "http://localhost:3000", JWTSecret: secret},
		},
		{
			name: "all set",
			overrides: map[string]string{
				"API_ADDR": "127.0.0.1:9000", "DATABASE_URL": "postgres://y", "WEB_ORIGIN": "https://app.example",
				"TRUSTED_PROXY_COUNT": "2",
			},
			want: config.Config{
				Addr: "127.0.0.1:9000", DatabaseURL: "postgres://y", WebOrigin: "https://app.example",
				JWTSecret: secret, TrustedProxies: 2,
			},
		},
		{name: "missing database url", overrides: map[string]string{"DATABASE_URL": ""}, wantErr: "DATABASE_URL is required"},
		{name: "missing jwt secret", overrides: map[string]string{"JWT_SECRET": ""}, wantErr: "JWT_SECRET is required"},
		{name: "short jwt secret", overrides: map[string]string{"JWT_SECRET": "too-short"}, wantErr: "JWT_SECRET must be at least 32 bytes"},
		{
			name:      "origin with port",
			overrides: map[string]string{"WEB_ORIGIN": "https://app.example:8443"},
			want:      config.Config{Addr: ":8080", DatabaseURL: "postgres://x", WebOrigin: "https://app.example:8443", JWTSecret: secret},
		},
		{name: "origin wildcard", overrides: map[string]string{"WEB_ORIGIN": "*"}, wantErr: "WEB_ORIGIN"},
		{name: "origin trailing slash", overrides: map[string]string{"WEB_ORIGIN": "http://localhost:3000/"}, wantErr: "WEB_ORIGIN"},
		{name: "origin no scheme", overrides: map[string]string{"WEB_ORIGIN": "localhost:3000"}, wantErr: "WEB_ORIGIN"},
		{name: "origin bad scheme", overrides: map[string]string{"WEB_ORIGIN": "ftp://app.example"}, wantErr: "WEB_ORIGIN"},
		{name: "origin no host", overrides: map[string]string{"WEB_ORIGIN": "http://"}, wantErr: "WEB_ORIGIN"},
		{name: "origin with path", overrides: map[string]string{"WEB_ORIGIN": "https://app.example/path"}, wantErr: "WEB_ORIGIN"},
		{name: "origin with userinfo", overrides: map[string]string{"WEB_ORIGIN": "https://user@app.example"}, wantErr: "WEB_ORIGIN"},
		{name: "origin with query", overrides: map[string]string{"WEB_ORIGIN": "https://app.example?x=1"}, wantErr: "WEB_ORIGIN"},
		{name: "origin with fragment", overrides: map[string]string{"WEB_ORIGIN": "https://app.example#frag"}, wantErr: "WEB_ORIGIN"},
		{name: "proxy count zero", overrides: map[string]string{"TRUSTED_PROXY_COUNT": "0"},
			want: config.Config{Addr: ":8080", DatabaseURL: "postgres://x", WebOrigin: "http://localhost:3000", JWTSecret: secret}},
		{name: "proxy count not a number", overrides: map[string]string{"TRUSTED_PROXY_COUNT": "two"}, wantErr: "TRUSTED_PROXY_COUNT"},
		{name: "proxy count negative", overrides: map[string]string{"TRUSTED_PROXY_COUNT": "-1"}, wantErr: "TRUSTED_PROXY_COUNT"},
		{name: "proxy count absurd", overrides: map[string]string{"TRUSTED_PROXY_COUNT": "50"}, wantErr: "TRUSTED_PROXY_COUNT"},
		{
			name:      "several problems are all reported",
			overrides: map[string]string{"DATABASE_URL": "", "JWT_SECRET": "", "WEB_ORIGIN": "*"},
			wantErr:   "DATABASE_URL is required",
			also:      "JWT_SECRET is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := config.Load(envWith(tt.overrides))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				if tt.also != "" && !strings.Contains(err.Error(), tt.also) {
					t.Fatalf("err = %v, want also containing %q", err, tt.also)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLoadDoesNotPutTheSecretInErrors(t *testing.T) {
	_, err := config.Load(envWith(map[string]string{"JWT_SECRET": "short-secret-value", "DATABASE_URL": ""}))

	if err == nil || strings.Contains(err.Error(), "short-secret-value") {
		t.Errorf("err = %v; the error must exist and must not contain the secret", err)
	}
}
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `cd backend && go test ./internal/config/ -count=1`
Expected: FAIL to build with `unknown field JWTSecret in struct literal of type config.Config`.

- [ ] **Step 3: Replace the implementation**

```go
// Package config loads process configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"

	"github.com/InzKazik/mealplanner/backend/internal/auth"
)

// maxTrustedProxies bounds TRUSTED_PROXY_COUNT: more hops than this is a typo.
const maxTrustedProxies = 10

// Config is the API's runtime configuration.
type Config struct {
	// Addr is the listen address, for example ":8080".
	Addr string
	// DatabaseURL is the Postgres connection string.
	DatabaseURL string
	// WebOrigin is the single browser origin allowed by CORS.
	WebOrigin string
	// JWTSecret signs access tokens (HS256). At least 32 bytes.
	JWTSecret string
	// TrustedProxies is how many reverse proxies in front of the API append to
	// X-Forwarded-For. 0 means clients connect directly.
	TrustedProxies int
}

// Load reads configuration through getenv (normally os.Getenv). It returns an
// error naming every missing required variable and every invalid value: it
// never includes a secret's value.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		Addr:        withDefault(getenv("API_ADDR"), ":8080"),
		DatabaseURL: getenv("DATABASE_URL"),
		WebOrigin:   withDefault(getenv("WEB_ORIGIN"), "http://localhost:3000"),
		JWTSecret:   getenv("JWT_SECRET"),
	}
	var errs []error
	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	switch {
	case cfg.JWTSecret == "":
		errs = append(errs, errors.New("JWT_SECRET is required"))
	case len(cfg.JWTSecret) < auth.MinSecretLength:
		errs = append(errs, fmt.Errorf("JWT_SECRET must be at least %d bytes", auth.MinSecretLength))
	}
	if err := validateOrigin(cfg.WebOrigin); err != nil {
		errs = append(errs, err)
	}
	if raw := getenv("TRUSTED_PROXY_COUNT"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > maxTrustedProxies {
			errs = append(errs, fmt.Errorf("TRUSTED_PROXY_COUNT must be an integer from 0 to %d, got %q", maxTrustedProxies, raw))
		} else {
			cfg.TrustedProxies = n
		}
	}
	if err := errors.Join(errs...); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return cfg, nil
}

func withDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// validateOrigin reports whether v is a bare origin (scheme and host, with an
// optional port) as browsers send in the Origin header, so it can be matched
// exactly by CORS. "*", trailing slashes, paths, queries, fragments and
// credentials are rejected.
func validateOrigin(v string) error {
	bad := fmt.Errorf("WEB_ORIGIN must be an origin such as https://app.example (scheme and host only, no path or trailing slash), got %q", v)
	u, err := url.Parse(v)
	if err != nil || v == "*" {
		return bad
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.User != nil {
		return bad
	}
	return nil
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `cd backend && gofmt -l . && go vet ./internal/config/ && go test ./internal/config/ -count=1 -v 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: `--- PASS: TestLoad` (with all its subtests) and `--- PASS: TestLoadDoesNotPutTheSecretInErrors`.

- [ ] **Step 5: Commit**

```bash
git add backend
git commit -m "feat(backend): configure the JWT secret and trusted proxy count"
```

---

### Task 7: Client IP (TDD)

**Files:**
- Create: `backend/internal/httpapi/clientip_test.go`
- Create: `backend/internal/httpapi/clientip.go`

**Interfaces:**
- Produces (package `httpapi`): `clientIP(trustedProxies int) func(http.Handler) http.Handler` (unexported middleware that stores the client address in the request context) and `httpapi.ClientIP(ctx context.Context) string` (`""` when the middleware did not run). With 0 trusted proxies `X-Forwarded-For` is ignored; with N the client is the Nth entry from the right, entries further left are never used, and an unparsable or missing entry falls back to the peer address. Tasks 8 and 9 use it.

- [ ] **Step 1: Write the failing test**

```go
package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		trusted    int
		remoteAddr string
		xff        []string
		want       string
	}{
		{"no proxy: remote address", 0, "203.0.113.7:5555", nil, "203.0.113.7"},
		{"no proxy: X-Forwarded-For is ignored (spoofable)", 0, "203.0.113.7:5555", []string{"1.2.3.4"}, "203.0.113.7"},
		{"one proxy: last entry is the client", 1, "10.0.0.1:80", []string{"198.51.100.9"}, "198.51.100.9"},
		{"one proxy: client-supplied prefix is ignored", 1, "10.0.0.1:80", []string{"6.6.6.6, 198.51.100.9"}, "198.51.100.9"},
		{"two proxies: second from the right", 2, "10.0.0.2:80", []string{"198.51.100.9, 10.0.0.1"}, "198.51.100.9"},
		{"header split across several lines", 2, "10.0.0.2:80", []string{"6.6.6.6", "198.51.100.9, 10.0.0.1"}, "198.51.100.9"},
		{"fewer entries than trusted proxies: fall back to the peer", 2, "10.0.0.2:80", []string{"198.51.100.9"}, "10.0.0.2"},
		{"missing header: fall back to the peer", 1, "10.0.0.1:80", nil, "10.0.0.1"},
		{"garbage entry: fall back to the peer", 1, "10.0.0.1:80", []string{"not-an-ip"}, "10.0.0.1"},
		{"ipv6 peer", 0, "[2001:db8::1]:4000", nil, "2001:db8::1"},
		{"ipv6 client behind a proxy", 1, "10.0.0.1:80", []string{"2001:db8::9"}, "2001:db8::9"},
		{"peer without a port", 0, "203.0.113.7", nil, "203.0.113.7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			h := clientIP(tt.trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got = ClientIP(r.Context())
			}))
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			for _, v := range tt.xff {
				req.Header.Add("X-Forwarded-For", v)
			}

			h.ServeHTTP(httptest.NewRecorder(), req)

			if got != tt.want {
				t.Errorf("ClientIP = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClientIPIsEmptyWithoutTheMiddleware(t *testing.T) {
	if got := ClientIP(httptest.NewRequest(http.MethodGet, "/", nil).Context()); got != "" {
		t.Errorf("ClientIP = %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run it and confirm it fails**

Run: `cd backend && go test ./internal/httpapi/ -run TestClientIP -count=1`
Expected: FAIL to build with `undefined: clientIP` and `undefined: ClientIP`.

- [ ] **Step 3: Write the implementation**

```go
package httpapi

import (
	"context"
	"net"
	"net/http"
	"strings"
)

const clientIPKey ctxKey = iota + 100

// ClientIP returns the client address determined by the clientIP middleware,
// or "" when the middleware did not run.
func ClientIP(ctx context.Context) string {
	ip, _ := ctx.Value(clientIPKey).(string)
	return ip
}

// clientIP stores the client's IP address in the request context.
//
// trustedProxies is the number of reverse proxies in front of the API that
// each append the address of their peer to X-Forwarded-For. With 0 the header
// is ignored, because any client can forge it. With N the client is the Nth
// entry counted from the right: entries further left were supplied by the
// client or an untrusted hop, so they are never used.
func clientIP(trustedProxies int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := peerIP(r.RemoteAddr)
			if trustedProxies > 0 {
				if forwarded, ok := forwardedClient(r.Header.Values("X-Forwarded-For"), trustedProxies); ok {
					ip = forwarded
				}
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientIPKey, ip)))
		})
	}
}

func peerIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

func forwardedClient(headers []string, trusted int) (string, bool) {
	var entries []string
	for _, h := range headers {
		for _, e := range strings.Split(h, ",") {
			entries = append(entries, strings.TrimSpace(e))
		}
	}
	if len(entries) < trusted {
		return "", false
	}
	candidate := entries[len(entries)-trusted]
	if net.ParseIP(candidate) == nil {
		return "", false
	}
	return candidate, true
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `cd backend && gofmt -l . && go vet ./internal/httpapi/ && go test ./internal/httpapi/ -count=1 -v 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: `--- PASS: TestClientIP` (twelve subtests), `--- PASS: TestClientIPIsEmptyWithoutTheMiddleware`, and every earlier httpapi test still `PASS`.

- [ ] **Step 5: Commit**

```bash
git add backend
git commit -m "feat(backend): determine the client IP with a trusted-proxy setting"
```

---

### Task 8: Field-level problems and validation errors (TDD)

**Files:**
- Modify: `backend/internal/httpapi/problem_test.go` (full replacement)
- Modify: `backend/internal/httpapi/problem.go` (full replacement)
- Create: `backend/internal/httpapi/validation_test.go`
- Create: `backend/internal/httpapi/validation.go`

**Interfaces:**
- Consumes: `api.GetSpec()` (Task 1).
- Produces (package `httpapi`): new problem codes `CodeUnauthorized`, `CodeRateLimited`, `CodeEmailTaken`, `CodeInvalidCredentials`, `CodeInvalidRefreshToken`; `FieldError{Field, Code string}`; field codes `FieldRequired`, `FieldTooShort`, `FieldTooLong`, `FieldInvalidType`, `FieldInvalidForm`, `FieldInvalidValue`, `FieldOutOfRange`, `FieldUnknown`; `WriteValidationProblem(w, detail string, fields []FieldError)` (400, `validation_failed`, `errors` omitted when empty); unexported `describeValidation(err error) (validationResult, bool)` mapping kin-openapi request-validation errors to sorted field errors (`ok == false` for any other error); an `init()` that registers an `email` string-format validator with kin-openapi (a bare address only, no display name). Task 9 uses all of these.

- [ ] **Step 1: Replace the problem tests and write the validation tests**

`backend/internal/httpapi/problem_test.go` (adds the two `WriteValidationProblem` tests to the existing ones):

```go
package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/httpapi"
)

func TestWriteProblem(t *testing.T) {
	rec := httptest.NewRecorder()

	httpapi.WriteProblem(rec, http.StatusConflict, "ingredient_in_use", "used by 2 meals")

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusConflict)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", got)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := map[string]any{
		"type":   "urn:mealplanner:problem:ingredient_in_use",
		"title":  "Conflict",
		"status": float64(409),
		"detail": "used by 2 meals",
		"code":   "ingredient_in_use",
	}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("body[%q] = %v, want %v", k, body[k], v)
		}
	}
}

func TestWriteProblemOmitsEmptyDetail(t *testing.T) {
	rec := httptest.NewRecorder()

	httpapi.WriteProblem(rec, http.StatusNotFound, httpapi.CodeNotFound, "")

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["detail"]; ok {
		t.Errorf("detail present, want omitted: %v", body)
	}
}

func TestWriteValidationProblemListsFieldErrors(t *testing.T) {
	rec := httptest.NewRecorder()

	httpapi.WriteValidationProblem(rec, "", []httpapi.FieldError{
		{Field: "email", Code: httpapi.FieldInvalidForm},
		{Field: "password", Code: httpapi.FieldTooShort},
	})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", got)
	}
	var body struct {
		Code   string `json:"code"`
		Errors []struct {
			Field string `json:"field"`
			Code  string `json:"code"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Code != httpapi.CodeValidationFailed || len(body.Errors) != 2 ||
		body.Errors[0].Field != "email" || body.Errors[0].Code != "invalid_format" ||
		body.Errors[1].Field != "password" || body.Errors[1].Code != "too_short" {
		t.Errorf("body = %+v", body)
	}
}

func TestWriteValidationProblemOmitsEmptyErrors(t *testing.T) {
	rec := httptest.NewRecorder()

	httpapi.WriteValidationProblem(rec, "request body is required", nil)

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["errors"]; ok {
		t.Errorf("errors present, want omitted: %v", body)
	}
	if body["detail"] != "request body is required" {
		t.Errorf("detail = %v", body["detail"])
	}
}
```

`backend/internal/httpapi/validation_test.go` (package `httpapi`, because it tests the unexported mapping; it drives the real kin-openapi validation against the embedded spec):

```go
package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/gorillamux"

	"github.com/InzKazik/mealplanner/backend/internal/api"
)

// validationError runs the real OpenAPI request validation for a request and
// returns the error kin-openapi produces.
func validationError(t *testing.T, method, path, body string) error {
	t.Helper()
	spec, err := api.GetSpec()
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	router, err := gorillamux.NewRouter(spec)
	if err != nil {
		t.Fatalf("build router: %v", err)
	}
	req := httptest.NewRequest(method, "http://localhost:8080/v1"+path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	route, params, err := router.FindRoute(req)
	if err != nil {
		t.Fatalf("find route: %v", err)
	}
	return openapi3filter.ValidateRequest(context.Background(), &openapi3filter.RequestValidationInput{
		Request: req, PathParams: params, Route: route,
		Options: &openapi3filter.Options{
			MultiError:         true,
			AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
		},
	})
}

func TestDescribeValidation(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		body       string
		wantDetail string
		wantFields []string // "field:code", sorted by field
	}{
		{"missing required fields", "/auth/register", `{"email":"a@example.com"}`, "", []string{"display_name:required", "password:required"}},
		{"password too short", "/auth/register", `{"email":"a@example.com","password":"short","display_name":"A"}`, "", []string{"password:too_short"}},
		{"password too long", "/auth/register", `{"email":"a@example.com","password":"` + strings.Repeat("x", 129) + `","display_name":"A"}`, "", []string{"password:too_long"}},
		{"email is not an address", "/auth/login", `{"email":"nope","password":"x"}`, "", []string{"email:invalid_format"}},
		{"email with a display name", "/auth/login", `{"email":"Alice <a@example.com>","password":"x"}`, "", []string{"email:invalid_format"}},
		{"wrong type", "/auth/register", `{"email":"a@example.com","password":12345,"display_name":"A"}`, "", []string{"password:invalid_type"}},
		{"unknown field", "/auth/login", `{"email":"a@example.com","password":"x","admin":true}`, "", []string{"admin:unknown_field"}},
		{"several problems, sorted", "/auth/register", `{"email":"nope","password":"short","display_name":""}`, "", []string{"display_name:too_short", "email:invalid_format", "password:too_short"}},
		{"invalid JSON", "/auth/login", `{"email":`, "request body is not valid JSON", nil},
		{"empty body", "/auth/login", ``, "request body is required", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validationError(t, http.MethodPost, tt.path, tt.body)
			if err == nil {
				t.Fatal("validation passed, want an error")
			}

			res, ok := describeValidation(err)

			if !ok {
				t.Fatalf("describeValidation reported an unexpected error: %v", err)
			}
			if res.detail != tt.wantDetail {
				t.Errorf("detail = %q, want %q", res.detail, tt.wantDetail)
			}
			var got []string
			for _, f := range res.fields {
				got = append(got, f.Field+":"+f.Code)
			}
			if strings.Join(got, ",") != strings.Join(tt.wantFields, ",") {
				t.Errorf("fields = %v, want %v", got, tt.wantFields)
			}
		})
	}
}

func TestDescribeValidationAcceptsAValidRequest(t *testing.T) {
	err := validationError(t, http.MethodPost, "/auth/register", `{"email":"a@example.com","password":"a-long-enough-pw","display_name":"A"}`)

	if err != nil {
		t.Errorf("a valid request failed validation: %v", err)
	}
}

func TestDescribeValidationRejectsErrorsItDoesNotUnderstand(t *testing.T) {
	if _, ok := describeValidation(errors.New("something else went wrong")); ok {
		t.Error("describeValidation claimed to understand an arbitrary error")
	}
}
```

- [ ] **Step 2: Run them and confirm they fail**

Run: `cd backend && go test ./internal/httpapi/ -count=1`
Expected: FAIL to build with `undefined: httpapi.WriteValidationProblem`, `undefined: httpapi.FieldError` and `undefined: describeValidation`.

- [ ] **Step 3: Replace `problem.go` and write `validation.go`**

`backend/internal/httpapi/problem.go`:

```go
package httpapi

import (
	"encoding/json"
	"net/http"
)

// Stable, machine-readable problem codes. Clients map these to localized text.
const (
	CodeValidationFailed    = "validation_failed"
	CodeNotFound            = "not_found"
	CodeMethodNotAllowed    = "method_not_allowed"
	CodeNotReady            = "not_ready"
	CodeInternal            = "internal_error"
	CodeUnauthorized        = "unauthorized"
	CodeRateLimited         = "rate_limited"
	CodeEmailTaken          = "email_taken"
	CodeInvalidCredentials  = "invalid_credentials" //nolint:gosec // an error code, not a credential
	CodeInvalidRefreshToken = "invalid_refresh_token"
)

// Stable codes for FieldError.Code.
const (
	FieldRequired     = "required"
	FieldTooShort     = "too_short"
	FieldTooLong      = "too_long"
	FieldInvalidType  = "invalid_type"
	FieldInvalidForm  = "invalid_format"
	FieldInvalidValue = "invalid_value"
	FieldOutOfRange   = "out_of_range"
	FieldUnknown      = "unknown_field"
)

// FieldError describes one invalid field of a request body.
type FieldError struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}

// problem is an RFC 9457 problem details document plus a stable code.
type problem struct {
	Type   string       `json:"type"`
	Title  string       `json:"title"`
	Status int          `json:"status"`
	Detail string       `json:"detail,omitempty"`
	Code   string       `json:"code"`
	Errors []FieldError `json:"errors,omitempty"`
}

// WriteProblem writes an application/problem+json response. detail may be empty.
func WriteProblem(w http.ResponseWriter, status int, code, detail string) {
	writeProblem(w, problem{
		Type:   "urn:mealplanner:problem:" + code,
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
		Code:   code,
	})
}

// WriteValidationProblem writes a 400 validation_failed problem listing the
// invalid fields. detail and fields may each be empty.
func WriteValidationProblem(w http.ResponseWriter, detail string, fields []FieldError) {
	writeProblem(w, problem{
		Type:   "urn:mealplanner:problem:" + CodeValidationFailed,
		Title:  http.StatusText(http.StatusBadRequest),
		Status: http.StatusBadRequest,
		Detail: detail,
		Code:   CodeValidationFailed,
		Errors: fields,
	})
}

func writeProblem(w http.ResponseWriter, p problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}
```

`backend/internal/httpapi/validation.go`:

```go
package httpapi

import (
	"errors"
	"net/mail"
	"regexp"
	"sort"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
)

func init() {
	// kin-openapi does not check `format: email` unless a validator is
	// registered. Accept a bare address only ("Name <a@b.c>" is rejected).
	openapi3.DefineStringFormatValidator("email", openapi3.NewCallbackValidator(func(s string) error {
		addr, err := mail.ParseAddress(s)
		if err != nil || addr.Address != s {
			return errors.New("not an email address")
		}
		return nil
	}))
}

var unsupportedProperty = regexp.MustCompile(`property "([^"]+)" is unsupported`)

// validationResult is what could be learned from a request-validation error.
type validationResult struct {
	detail string
	fields []FieldError
}

// describeValidation translates kin-openapi request-validation errors into
// client-facing field errors. It returns false when err contains anything
// other than request-validation errors (a server-side problem).
func describeValidation(err error) (validationResult, bool) {
	var res validationResult
	if !collect(err, &res) {
		return validationResult{}, false
	}
	sort.Slice(res.fields, func(i, j int) bool {
		if res.fields[i].Field != res.fields[j].Field {
			return res.fields[i].Field < res.fields[j].Field
		}
		return res.fields[i].Code < res.fields[j].Code
	})
	return res, true
}

func collect(err error, res *validationResult) bool {
	// kin-openapi returns these types directly, and MultiError must be tried
	// first: it wraps the others.
	var multi openapi3.MultiError
	if errors.As(err, &multi) {
		for _, inner := range multi {
			if !collect(inner, res) {
				return false
			}
		}
		return true
	}
	var reqErr *openapi3filter.RequestError
	if errors.As(err, &reqErr) {
		return collectRequestError(reqErr, res)
	}
	var schemaErr *openapi3.SchemaError
	if errors.As(err, &schemaErr) {
		res.fields = append(res.fields, schemaFieldError(schemaErr))
		return true
	}
	return false
}

func collectRequestError(e *openapi3filter.RequestError, res *validationResult) bool {
	if e.Parameter != nil {
		res.fields = append(res.fields, FieldError{Field: e.Parameter.Name, Code: FieldInvalidValue})
		return true
	}
	if e.RequestBody == nil || e.Err == nil {
		return false
	}
	var (
		multi     openapi3.MultiError
		schemaErr *openapi3.SchemaError
		parseErr  *openapi3filter.ParseError
	)
	switch {
	case errors.As(e.Err, &multi), errors.As(e.Err, &schemaErr):
		return collect(e.Err, res)
	case errors.As(e.Err, &parseErr):
		res.detail = "request body is not valid JSON"
		return true
	default:
		// kin-openapi reports a missing required body as a plain error.
		res.detail = "request body is required"
		return true
	}
}

func schemaFieldError(e *openapi3.SchemaError) FieldError {
	field := strings.Join(e.JSONPointer(), ".")
	switch e.SchemaField {
	case "required":
		return FieldError{Field: field, Code: FieldRequired}
	case "minLength", "minItems":
		return FieldError{Field: field, Code: FieldTooShort}
	case "maxLength", "maxItems":
		return FieldError{Field: field, Code: FieldTooLong}
	case "format", "pattern":
		return FieldError{Field: field, Code: FieldInvalidForm}
	case "type":
		return FieldError{Field: field, Code: FieldInvalidType}
	case "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf":
		return FieldError{Field: field, Code: FieldOutOfRange}
	case "properties":
		if m := unsupportedProperty.FindStringSubmatch(e.Reason); m != nil {
			name := m[1]
			if field != "" {
				name = field + "." + name
			}
			return FieldError{Field: name, Code: FieldUnknown}
		}
	}
	return FieldError{Field: field, Code: FieldInvalidValue}
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `cd backend && gofmt -l . && go vet ./internal/httpapi/ && go test ./internal/httpapi/ -count=1 -v 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: `--- PASS: TestDescribeValidation` (ten subtests), `TestDescribeValidationAcceptsAValidRequest`, `TestDescribeValidationRejectsErrorsItDoesNotUnderstand`, `TestWriteValidationProblemListsFieldErrors`, `TestWriteValidationProblemOmitsEmptyErrors`, and every earlier httpapi test.

- [ ] **Step 5: Commit**

```bash
git add backend
git commit -m "feat(backend): report validation failures as per-field problems"
```

---

### Task 9: Enforcement, rate limits and the account handlers (TDD, needs Docker)

**Files:**
- Modify: `backend/go.mod`, `backend/go.sum`
- Modify: `backend/internal/httpapi/contract_test.go`, `router_test.go` (full replacements)
- Create: `backend/internal/httpapi/auth_test.go`, `account_flow_test.go`
- Create: `backend/internal/httpapi/auth.go`, `ratelimit.go`, `account.go`
- Modify: `backend/internal/httpapi/middleware.go`, `server.go`, `router.go` (full replacements)

**Interfaces:**
- Consumes: `service.*` (Task 5), `auth.ErrInvalidAccessToken` (Task 4), `clientIP` / `ClientIP` (Task 7), `WriteProblem`, `WriteValidationProblem`, `describeValidation` and the new codes (Task 8), `api.*` (Task 1), `testutil.NewMigratedDatabase` (Task 2).
- Produces:
  - `httpapi.AuthService` (interface: `Register`, `Login`, `Refresh`, `Logout`, `GetUser`, `UpdateUser`, `DeleteUser`; `*service.Auth` satisfies it), `httpapi.TokenParser` (interface: `ParseAccess(token string) (uuid.UUID, error)`; `*auth.TokenIssuer` satisfies it), `httpapi.RateLimits{AuthPerMinute, UserPerMinute int}` (zero means 10 and 300), `httpapi.UserID(ctx) (uuid.UUID, bool)`.
  - `httpapi.Deps` gains `Auth AuthService`, `Tokens TokenParser`, `Limits RateLimits`, `TrustedProxies int`. `NewRouter` now **panics at construction** when `Logger`, `Ready`, `Auth` or `Tokens` is nil.
  - Behaviour: the spec-driven validator authenticates secured operations (bearer token) and validates bodies for every operation; authentication failure (`401`, `WWW-Authenticate: Bearer`, code `unauthorized`) wins over body errors; schema failures are `400 validation_failed` with field `errors`; `/v1/auth/*` is limited per client IP before routing (malformed requests count) and other authenticated requests per user after authentication (`429`, code `rate_limited`, `Retry-After`); request bodies over 64 KiB are rejected; the request log gains `remote_ip`, `user_id`, `duration_ms` and logs 5xx at error level; `/readyz` gives up after 2 seconds; the generated wrapper's parameter errors no longer echo parser text to clients.
  - Every operation has a handler, so the temporary `api.Unimplemented` embed from Task 1 is removed.

- [ ] **Step 1: Add the dependencies**

```bash
cd backend && go get github.com/oapi-codegen/nethttp-middleware@v1.2.0 github.com/go-chi/httprate@v0.16.0
```

- [ ] **Step 2: Replace the shared test helpers**

`backend/internal/httpapi/contract_test.go` grows request options (`withBody`, `withBearer`, `withRemoteAddr`, `withInvalidRequest`), loads the spec once, and provides the stubs the stub-based tests use:

```go
package httpapi_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/auth"
	"github.com/InzKazik/mealplanner/backend/internal/httpapi"
	"github.com/InzKazik/mealplanner/backend/internal/service"
)

const specPath = "../../../openapi.yaml"

// specRouter loads and validates openapi.yaml once for the whole test binary.
var specRouter = sync.OnceValues(func() (routers.Router, error) {
	doc, err := openapi3.NewLoader().LoadFromFile(specPath)
	if err != nil {
		return nil, err
	}
	if err := doc.Validate(context.Background()); err != nil {
		return nil, err
	}
	return gorillamux.NewRouter(doc)
})

// validToken is the only access token stubTokens accepts.
const validToken = "valid-token"

var stubUserID = uuid.MustParse("11111111-1111-1111-1111-111111111111")

type stubTokens struct{}

func (stubTokens) ParseAccess(token string) (uuid.UUID, error) {
	if token == validToken {
		return stubUserID, nil
	}
	return uuid.Nil, auth.ErrInvalidAccessToken
}

// stubAuth answers Login with invalid credentials and GetUser with a fixed
// user, and panics on anything else, so tests that must not reach the service
// fail loudly if they do.
type stubAuth struct{ httpapi.AuthService }

func (stubAuth) Login(context.Context, string, string) (service.Session, error) {
	return service.Session{}, service.ErrInvalidCredentials
}

func (stubAuth) GetUser(_ context.Context, id uuid.UUID) (service.User, error) {
	at := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	return service.User{ID: id, Email: "stub@example.com", DisplayName: "Stub", CreatedAt: at, UpdatedAt: at}, nil
}

func newTestRouter(t *testing.T, mods ...func(*httpapi.Deps)) http.Handler {
	t.Helper()
	d := httpapi.Deps{
		Logger:    slog.New(slog.DiscardHandler),
		Ready:     func(context.Context) error { return nil },
		WebOrigin: "http://localhost:3000",
		Auth:      stubAuth{},
		Tokens:    stubTokens{},
	}
	for _, m := range mods {
		m(&d)
	}
	return httpapi.NewRouter(d)
}

func newRouter(t *testing.T, ready func(context.Context) error) http.Handler {
	t.Helper()
	return newTestRouter(t, func(d *httpapi.Deps) { d.Ready = ready })
}

func alwaysReady(context.Context) error { return nil }

type request struct {
	body       string
	bearer     string
	remoteAddr string
	skipReqVal bool
}

type requestOption func(*request)

func withBody(json string) requestOption    { return func(r *request) { r.body = json } }
func withBearer(token string) requestOption { return func(r *request) { r.bearer = token } }
func withRemoteAddr(addr string) requestOption {
	return func(r *request) { r.remoteAddr = addr }
}

// withInvalidRequest skips validating the request itself against the
// contract, for tests that deliberately send a bad request. The response is
// still validated.
func withInvalidRequest() requestOption { return func(r *request) { r.skipReqVal = true } }

// contract sends the request through handler and fails the test unless the
// request (unless withInvalidRequest) and the response conform to
// openapi.yaml. It returns the response.
func contract(t *testing.T, handler http.Handler, method, path string, opts ...requestOption) *httptest.ResponseRecorder {
	t.Helper()
	ctx := context.Background()
	var cfg request
	for _, o := range opts {
		o(&cfg)
	}

	oaRouter, err := specRouter()
	if err != nil {
		t.Fatalf("load %s: %v", specPath, err)
	}

	var body io.Reader
	if cfg.body != "" {
		body = strings.NewReader(cfg.body)
	}
	req := httptest.NewRequest(method, "http://localhost:8080/v1"+path, body)
	if cfg.body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cfg.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.bearer)
	}
	if cfg.remoteAddr != "" {
		req.RemoteAddr = cfg.remoteAddr
	}

	route, pathParams, err := oaRouter.FindRoute(req)
	if err != nil {
		if errors.Is(err, routers.ErrPathNotFound) {
			t.Fatalf("%s %s is not declared in openapi.yaml", method, path)
		}
		t.Fatalf("find route: %v", err)
	}
	opts2 := &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc}
	in := &openapi3filter.RequestValidationInput{Request: req, PathParams: pathParams, Route: route, Options: opts2}
	if !cfg.skipReqVal {
		if err := openapi3filter.ValidateRequest(ctx, in); err != nil {
			t.Fatalf("request does not match contract: %v", err)
		}
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	out := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: in,
		Status:                 rec.Code,
		Header:                 rec.Header(),
		Body:                   io.NopCloser(bytes.NewReader(rec.Body.Bytes())),
		Options:                &openapi3filter.Options{IncludeResponseStatus: true},
	}
	if err := openapi3filter.ValidateResponse(ctx, out); err != nil {
		t.Fatalf("response %d %q does not match contract: %v", rec.Code, rec.Body.String(), err)
	}
	return rec
}

func TestHealthzMatchesContract(t *testing.T) {
	rec := contract(t, newRouter(t, alwaysReady), http.MethodGet, "/healthz")

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestReadyzMatchesContract(t *testing.T) {
	tests := []struct {
		name       string
		ready      func(context.Context) error
		wantStatus int
		wantCT     string
	}{
		{"ready", alwaysReady, http.StatusOK, "application/json"},
		{
			"database down",
			func(context.Context) error { return errors.New("connection refused") },
			http.StatusServiceUnavailable,
			"application/problem+json",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := contract(t, newRouter(t, tt.ready), http.MethodGet, "/readyz")

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Content-Type"); got != tt.wantCT {
				t.Errorf("Content-Type = %q, want %q", got, tt.wantCT)
			}
		})
	}
}

func TestReadyzGivesUpOnAHungDatabase(t *testing.T) {
	hung := func(ctx context.Context) error {
		<-ctx.Done() // a wedged database never answers; only the deadline ends the wait
		return ctx.Err()
	}

	rec := contract(t, newRouter(t, hung), http.MethodGet, "/readyz")

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestNewRouterPanicsWithoutRequiredDependencies(t *testing.T) {
	full := httpapi.Deps{
		Logger: slog.New(slog.DiscardHandler), Ready: alwaysReady,
		WebOrigin: "http://localhost:3000", Auth: stubAuth{}, Tokens: stubTokens{},
	}
	tests := map[string]func(*httpapi.Deps){
		"no logger": func(d *httpapi.Deps) { d.Logger = nil },
		"no ready":  func(d *httpapi.Deps) { d.Ready = nil },
		"no auth":   func(d *httpapi.Deps) { d.Auth = nil },
		"no tokens": func(d *httpapi.Deps) { d.Tokens = nil },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			d := full
			mutate(&d)
			defer func() {
				if recover() == nil {
					t.Error("NewRouter did not panic")
				}
			}()
			httpapi.NewRouter(d)
		})
	}
}
```

`backend/internal/httpapi/router_test.go` (the existing router, request-ID and CORS tests, adapted to the new `Deps`, plus the request-log tests):

```go
package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/httpapi"
)

func do(t *testing.T, h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json", got)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	return body
}

func TestUnknownPathReturnsProblem(t *testing.T) {
	rec := do(t, newRouter(t, alwaysReady), httptest.NewRequest(http.MethodGet, "/v1/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if code := decodeProblem(t, rec)["code"]; code != httpapi.CodeNotFound {
		t.Errorf("code = %v, want %s", code, httpapi.CodeNotFound)
	}
}

func TestWrongMethodReturnsProblem(t *testing.T) {
	rec := do(t, newRouter(t, alwaysReady), httptest.NewRequest(http.MethodPost, "/v1/healthz", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if code := decodeProblem(t, rec)["code"]; code != httpapi.CodeMethodNotAllowed {
		t.Errorf("code = %v, want %s", code, httpapi.CodeMethodNotAllowed)
	}
	if got := rec.Header().Get("Allow"); got != "GET" {
		t.Errorf("Allow = %q, want %q (RFC 9110 requires it on 405)", got, "GET")
	}
}

func TestRequestIDIsGeneratedAndReplacedWhenInvalid(t *testing.T) {
	h := newRouter(t, alwaysReady)
	hex32 := regexp.MustCompile(`^[0-9a-f]{32}$`)

	rec := do(t, h, httptest.NewRequest(http.MethodGet, "/v1/healthz", nil))
	if id := rec.Header().Get("X-Request-Id"); !hex32.MatchString(id) {
		t.Errorf("generated X-Request-Id = %q, want 32 hex chars", id)
	}

	bad := httptest.NewRequest(http.MethodGet, "/v1/healthz", nil)
	bad.Header.Set("X-Request-Id", "has spaces and \"quotes\"")
	if id := do(t, h, bad).Header().Get("X-Request-Id"); !hex32.MatchString(id) {
		t.Errorf("invalid inbound ID was kept: %q", id)
	}
}

func TestRequestIDIsEchoedWhenValid(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/healthz", nil)
	req.Header.Set("X-Request-Id", "trace-abc.123")

	rec := do(t, newRouter(t, alwaysReady), req)

	if id := rec.Header().Get("X-Request-Id"); id != "trace-abc.123" {
		t.Errorf("X-Request-Id = %q, want the inbound value", id)
	}
}

func TestRequestIsLoggedWithRequestID(t *testing.T) {
	var buf bytes.Buffer
	h := newTestRouter(t, func(d *httpapi.Deps) { d.Logger = slog.New(slog.NewJSONHandler(&buf, nil)) })
	req := httptest.NewRequest(http.MethodGet, "/v1/healthz", nil)
	req.Header.Set("X-Request-Id", "log-me")

	do(t, h, req)

	var line map[string]any
	if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
		t.Fatalf("log line is not JSON: %q (%v)", buf.String(), err)
	}
	want := map[string]any{"msg": "request", "request_id": "log-me", "method": "GET", "path": "/v1/healthz", "status": float64(200)}
	for k, v := range want {
		if line[k] != v {
			t.Errorf("log[%q] = %v, want %v", k, line[k], v)
		}
	}
}

func TestCORS(t *testing.T) {
	h := newRouter(t, alwaysReady)
	preflight := func(origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodOptions, "/v1/healthz", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", "GET")
		req.Header.Set("Access-Control-Request-Headers", "authorization")
		return do(t, h, req)
	}

	allowed := preflight("http://localhost:3000")
	if got := allowed.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Errorf("allowed origin: Allow-Origin = %q, want the origin echoed", got)
	}
	if got := allowed.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(strings.ToLower(got), "authorization") {
		t.Errorf("Allow-Headers = %q, want it to include Authorization", got)
	}

	denied := preflight("https://evil.example")
	if got := denied.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("disallowed origin got Allow-Origin = %q, want none", got)
	}
}

// requestLogLine returns the "request" log entry from JSON-lines output.
func requestLogLine(t *testing.T, out string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("log line is not JSON: %q (%v)", line, err)
		}
		if entry["msg"] == "request" {
			return entry
		}
	}
	t.Fatalf("no request log line in %q", out)
	return nil
}

func TestRequestLogIncludesClientIPUserAndDuration(t *testing.T) {
	var buf bytes.Buffer
	h := newTestRouter(t, func(d *httpapi.Deps) { d.Logger = slog.New(slog.NewJSONHandler(&buf, nil)) })
	req := httptest.NewRequest(http.MethodGet, "/v1/me?secret=do-not-log", nil)
	req.RemoteAddr = "203.0.113.5:4321"
	req.Header.Set("Authorization", "Bearer "+validToken)

	do(t, h, req)

	line := requestLogLine(t, buf.String())
	if line["remote_ip"] != "203.0.113.5" {
		t.Errorf("remote_ip = %v, want 203.0.113.5", line["remote_ip"])
	}
	if line["user_id"] != stubUserID.String() {
		t.Errorf("user_id = %v, want %s", line["user_id"], stubUserID)
	}
	if _, ok := line["duration_ms"].(float64); !ok {
		t.Errorf("duration_ms = %v (%T), want a number", line["duration_ms"], line["duration_ms"])
	}
	if strings.Contains(buf.String(), "do-not-log") || strings.Contains(buf.String(), validToken) {
		t.Errorf("the log leaked a query string or a token: %s", buf.String())
	}
}

func TestUnauthenticatedRequestLogHasNoUser(t *testing.T) {
	var buf bytes.Buffer
	h := newTestRouter(t, func(d *httpapi.Deps) { d.Logger = slog.New(slog.NewJSONHandler(&buf, nil)) })

	do(t, h, httptest.NewRequest(http.MethodGet, "/v1/healthz", nil))

	if _, ok := requestLogLine(t, buf.String())["user_id"]; ok {
		t.Error("user_id present on an unauthenticated request")
	}
}

func TestRequestLogLevelFollowsStatus(t *testing.T) {
	tests := []struct {
		name      string
		ready     func(context.Context) error
		path      string
		wantLevel string
	}{
		{"success is INFO", alwaysReady, "/v1/healthz", "INFO"},
		{"client error is INFO", alwaysReady, "/v1/nope", "INFO"},
		{"server error is ERROR", func(context.Context) error { return errors.New("db down") }, "/v1/readyz", "ERROR"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			h := newTestRouter(t, func(d *httpapi.Deps) {
				d.Logger = slog.New(slog.NewJSONHandler(&buf, nil))
				d.Ready = tt.ready
			})

			do(t, h, httptest.NewRequest(http.MethodGet, tt.path, nil))

			if got := requestLogLine(t, buf.String())["level"]; got != tt.wantLevel {
				t.Errorf("level = %v, want %s", got, tt.wantLevel)
			}
		})
	}
}
```

- [ ] **Step 3: Write the new tests**

`backend/internal/httpapi/auth_test.go` (validation problems through the router, authentication precedence, opt-out routes, both rate limits, the trusted-proxy header):

```go
package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/httpapi"
)

type problemBody struct {
	Code   string `json:"code"`
	Detail string `json:"detail"`
	Errors []struct {
		Field string `json:"field"`
		Code  string `json:"code"`
	} `json:"errors"`
}

func decodeProblemBody(t *testing.T, rec *httptest.ResponseRecorder) problemBody {
	t.Helper()
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Fatalf("Content-Type = %q, want application/problem+json (body %s)", got, rec.Body.String())
	}
	var p problemBody
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	return p
}

func TestRequestValidationProblems(t *testing.T) {
	longPassword := strings.Repeat("x", 129)
	tests := []struct {
		name       string
		path       string
		body       string
		wantDetail string
		wantFields []string // "field:code", sorted by field
	}{
		{
			name:       "missing required fields",
			path:       "/auth/register",
			body:       `{"email":"a@example.com"}`,
			wantFields: []string{"display_name:required", "password:required"},
		},
		{
			name:       "password too short",
			path:       "/auth/register",
			body:       `{"email":"a@example.com","password":"short","display_name":"A"}`,
			wantFields: []string{"password:too_short"},
		},
		{
			name:       "password too long",
			path:       "/auth/register",
			body:       `{"email":"a@example.com","password":"` + longPassword + `","display_name":"A"}`,
			wantFields: []string{"password:too_long"},
		},
		{
			name:       "email is not an address",
			path:       "/auth/register",
			body:       `{"email":"not-an-email","password":"long-enough-password","display_name":"A"}`,
			wantFields: []string{"email:invalid_format"},
		},
		{
			name:       "email with a display name is rejected",
			path:       "/auth/login",
			body:       `{"email":"Alice <a@example.com>","password":"x"}`,
			wantFields: []string{"email:invalid_format"},
		},
		{
			name:       "wrong type",
			path:       "/auth/register",
			body:       `{"email":"a@example.com","password":12345,"display_name":"A"}`,
			wantFields: []string{"password:invalid_type"},
		},
		{
			name:       "unknown field",
			path:       "/auth/login",
			body:       `{"email":"a@example.com","password":"x","admin":true}`,
			wantFields: []string{"admin:unknown_field"},
		},
		{
			name:       "several problems are all reported",
			path:       "/auth/register",
			body:       `{"email":"nope","password":"short","display_name":""}`,
			wantFields: []string{"display_name:too_short", "email:invalid_format", "password:too_short"},
		},
		{name: "invalid JSON", path: "/auth/login", body: `{"email":`, wantDetail: "request body is not valid JSON"},
		{name: "empty body", path: "/auth/login", body: ``, wantDetail: "request body is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := contract(t, newTestRouter(t), http.MethodPost, tt.path, withBody(tt.body), withInvalidRequest())

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
			p := decodeProblemBody(t, rec)
			if p.Code != httpapi.CodeValidationFailed {
				t.Errorf("code = %q, want %q", p.Code, httpapi.CodeValidationFailed)
			}
			if tt.wantDetail != "" && p.Detail != tt.wantDetail {
				t.Errorf("detail = %q, want %q", p.Detail, tt.wantDetail)
			}
			var got []string
			for _, e := range p.Errors {
				got = append(got, e.Field+":"+e.Code)
			}
			if strings.Join(got, ",") != strings.Join(tt.wantFields, ",") {
				t.Errorf("errors = %v, want %v", got, tt.wantFields)
			}
		})
	}
}

func TestOversizedBodyIsRejected(t *testing.T) {
	big := `{"email":"a@example.com","password":"` + strings.Repeat("x", 70<<10) + `"}`

	rec := contract(t, newTestRouter(t), http.MethodPost, "/auth/login", withBody(big), withInvalidRequest())

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestSecuredOperationsRequireAValidAccessToken(t *testing.T) {
	router := newTestRouter(t)
	tests := []struct {
		name   string
		method string
		path   string
		opts   []requestOption
		want   int
	}{
		{"GET /me without a token", http.MethodGet, "/me", nil, http.StatusUnauthorized},
		{"GET /me with an invalid token", http.MethodGet, "/me", []requestOption{withBearer("garbage")}, http.StatusUnauthorized},
		{"DELETE /me without a token", http.MethodDelete, "/me", nil, http.StatusUnauthorized},
		{
			// Authentication is decided before the body is looked at, so an
			// unauthenticated caller learns nothing about the schema.
			"PATCH /me without a token and with an invalid body",
			http.MethodPatch, "/me",
			[]requestOption{withBody(`{"target_kcal":-5}`), withInvalidRequest()},
			http.StatusUnauthorized,
		},
		{"GET /me with a valid token", http.MethodGet, "/me", []requestOption{withBearer(validToken)}, http.StatusOK},
		{
			"PATCH /me with a valid token and an invalid body",
			http.MethodPatch, "/me",
			[]requestOption{withBearer(validToken), withBody(`{"target_kcal":-5}`), withInvalidRequest()},
			http.StatusBadRequest,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := contract(t, router, tt.method, tt.path, tt.opts...)

			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.want, rec.Body.String())
			}
			if tt.want == http.StatusUnauthorized {
				if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
					t.Errorf("WWW-Authenticate = %q, want Bearer", got)
				}
				if p := decodeProblemBody(t, rec); p.Code != httpapi.CodeUnauthorized || len(p.Errors) != 0 {
					t.Errorf("problem = %+v, want code unauthorized and no field errors", p)
				}
			}
		})
	}
}

func TestAuthorizationSchemeIsCaseInsensitiveButMustBeBearer(t *testing.T) {
	router := newTestRouter(t)
	do := func(header string) int {
		req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
		req.Header.Set("Authorization", header)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	if got := do("bearer " + validToken); got != http.StatusOK {
		t.Errorf("lowercase scheme: status = %d, want 200", got)
	}
	for _, h := range []string{"Basic " + validToken, "Bearer", "Bearer  ", validToken} {
		if got := do(h); got != http.StatusUnauthorized {
			t.Errorf("Authorization %q: status = %d, want 401", h, got)
		}
	}
}

func TestOperationsThatOptOutOfSecurityNeedNoToken(t *testing.T) {
	// The stub rejects every login, but the request must get as far as the
	// handler: a 401 with code invalid_credentials, not the validator's unauthorized.
	rec := contract(t, newTestRouter(t), http.MethodPost, "/auth/login",
		withBody(`{"email":"a@example.com","password":"x"}`))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if p := decodeProblemBody(t, rec); p.Code != httpapi.CodeInvalidCredentials {
		t.Errorf("code = %q, want %q", p.Code, httpapi.CodeInvalidCredentials)
	}
}

func TestAuthEndpointsAreRateLimitedPerClientIP(t *testing.T) {
	router := newTestRouter(t, func(d *httpapi.Deps) { d.Limits = httpapi.RateLimits{AuthPerMinute: 3} })
	login := func(addr string) *httptest.ResponseRecorder {
		return contract(t, router, http.MethodPost, "/auth/login",
			withBody(`{"email":"a@example.com","password":"x"}`), withRemoteAddr(addr))
	}

	for i := 1; i <= 3; i++ {
		if rec := login("198.51.100.1:1000"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401 (not yet limited)", i, rec.Code)
		}
	}
	rec := login("198.51.100.1:1000")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("4th attempt: status = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("429 response has no Retry-After header")
	}
	if p := decodeProblemBody(t, rec); p.Code != httpapi.CodeRateLimited {
		t.Errorf("code = %q, want %q", p.Code, httpapi.CodeRateLimited)
	}

	if rec := login("198.51.100.2:1000"); rec.Code != http.StatusUnauthorized {
		t.Errorf("a different client IP was limited too: status = %d, want 401", rec.Code)
	}
	if rec := contract(t, router, http.MethodGet, "/healthz", withRemoteAddr("198.51.100.1:1000")); rec.Code != http.StatusOK {
		t.Errorf("non-auth endpoint was limited by the auth limiter: status = %d", rec.Code)
	}
}

func TestBadRequestsCountTowardTheAuthRateLimit(t *testing.T) {
	router := newTestRouter(t, func(d *httpapi.Deps) { d.Limits = httpapi.RateLimits{AuthPerMinute: 2} })
	send := func() int {
		return contract(t, router, http.MethodPost, "/auth/login",
			withBody(`{}`), withInvalidRequest(), withRemoteAddr("198.51.100.1:1000")).Code
	}

	if a, b := send(), send(); a != http.StatusBadRequest || b != http.StatusBadRequest {
		t.Fatalf("first two = %d, %d; want 400, 400", a, b)
	}
	if got := send(); got != http.StatusTooManyRequests {
		t.Errorf("third malformed request: status = %d, want 429", got)
	}
}

func TestAuthenticatedRequestsAreRateLimitedPerUser(t *testing.T) {
	router := newTestRouter(t, func(d *httpapi.Deps) { d.Limits = httpapi.RateLimits{UserPerMinute: 2} })
	me := func(opts ...requestOption) int {
		return contract(t, router, http.MethodGet, "/me", opts...).Code
	}

	if a, b := me(withBearer(validToken)), me(withBearer(validToken)); a != http.StatusOK || b != http.StatusOK {
		t.Fatalf("first two = %d, %d; want 200, 200", a, b)
	}
	if got := me(withBearer(validToken)); got != http.StatusTooManyRequests {
		t.Errorf("third request: status = %d, want 429", got)
	}
	// Unauthenticated requests are rejected by the validator before the
	// per-user limiter, and never count against anyone.
	if got := me(); got != http.StatusUnauthorized {
		t.Errorf("unauthenticated request: status = %d, want 401", got)
	}
}

func TestTrustedProxyHeaderDecidesTheRateLimitedClient(t *testing.T) {
	router := newTestRouter(t, func(d *httpapi.Deps) {
		d.Limits = httpapi.RateLimits{AuthPerMinute: 1}
		d.TrustedProxies = 1
	})
	login := func(forwardedFor string) int {
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(`{"email":"a@example.com","password":"x"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "10.0.0.1:80" // the proxy: every request comes from here
		req.Header.Set("X-Forwarded-For", forwardedFor)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	if got := login("198.51.100.1"); got != http.StatusUnauthorized {
		t.Fatalf("client A first request: status = %d, want 401", got)
	}
	if got := login("198.51.100.1"); got != http.StatusTooManyRequests {
		t.Errorf("client A second request: status = %d, want 429", got)
	}
	if got := login("198.51.100.2"); got != http.StatusUnauthorized {
		t.Errorf("client B was limited with client A: status = %d, want 401", got)
	}
	// Forging entries on the left cannot dodge the limit.
	if got := login("6.6.6.6, 198.51.100.1"); got != http.StatusTooManyRequests {
		t.Errorf("client A with a forged prefix: status = %d, want 429", got)
	}
}
```

`backend/internal/httpapi/account_flow_test.go` (the whole account lifecycle through the real router, service and Postgres, with every request and response validated against `openapi.yaml`):

```go
package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/InzKazik/mealplanner/backend/internal/api"
	"github.com/InzKazik/mealplanner/backend/internal/auth"
	"github.com/InzKazik/mealplanner/backend/internal/db"
	"github.com/InzKazik/mealplanner/backend/internal/httpapi"
	"github.com/InzKazik/mealplanner/backend/internal/service"
	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/testutil"
)

func newAccountRouter(t *testing.T) http.Handler {
	t.Helper()
	pool, err := db.Connect(context.Background(), testutil.NewMigratedDatabase(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	issuer := auth.NewTokenIssuer([]byte("0123456789abcdef0123456789abcdef"), 15*time.Minute, time.Now)
	svc := service.NewAuth(store.New(pool),
		auth.NewHasher(auth.HashParams{MemoryKiB: 8, Iterations: 1, Parallelism: 1}),
		issuer, 30*24*time.Hour, time.Now)
	return newTestRouter(t, func(d *httpapi.Deps) {
		d.Auth = svc
		d.Tokens = issuer
		d.Limits = httpapi.RateLimits{AuthPerMinute: 1000, UserPerMinute: 1000}
	})
}

func decodeAs[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return v
}

func problemCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	return decodeProblemBody(t, rec).Code
}

// TestAccountLifecycle drives every account endpoint through the real router,
// service and Postgres. contract() validates each request and response
// against openapi.yaml.
func TestAccountLifecycle(t *testing.T) {
	router := newAccountRouter(t)
	creds := func(email, password string) requestOption {
		return withBody(`{"email":"` + email + `","password":"` + password + `"}`)
	}
	const password = "a-long-enough-password"

	// Register.
	rec := contract(t, router, http.MethodPost, "/auth/register",
		withBody(`{"email":"Alice@Example.com","password":"`+password+`","display_name":"Alice"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: status = %d, body %s", rec.Code, rec.Body.String())
	}
	registered := decodeAs[api.AuthResponse](t, rec)
	if registered.TokenType != api.AuthResponseTokenTypeBearer || registered.ExpiresIn != 900 {
		t.Errorf("register: token_type=%q expires_in=%d, want Bearer and 900", registered.TokenType, registered.ExpiresIn)
	}
	if registered.User.Email != "Alice@Example.com" || registered.User.DisplayName != "Alice" {
		t.Errorf("register: user = %+v", registered.User)
	}
	if !strings.Contains(rec.Body.String(), `"target_kcal":null`) {
		t.Errorf("a new user's unset targets must be explicit nulls: %s", rec.Body.String())
	}

	// A second account with the same email (different case) is refused.
	rec = contract(t, router, http.MethodPost, "/auth/register",
		withBody(`{"email":"ALICE@example.com","password":"`+password+`","display_name":"Other"}`))
	if rec.Code != http.StatusConflict || problemCode(t, rec) != httpapi.CodeEmailTaken {
		t.Errorf("duplicate register: status = %d, body %s", rec.Code, rec.Body.String())
	}

	// Login: wrong password and unknown email are indistinguishable.
	for _, c := range []struct{ email, pw string }{{"alice@example.com", "wrong-password!"}, {"nobody@example.com", password}} {
		rec = contract(t, router, http.MethodPost, "/auth/login", creds(c.email, c.pw))
		if rec.Code != http.StatusUnauthorized || problemCode(t, rec) != httpapi.CodeInvalidCredentials {
			t.Errorf("login(%s): status = %d, body %s", c.email, rec.Code, rec.Body.String())
		}
	}
	rec = contract(t, router, http.MethodPost, "/auth/login", creds("alice@example.com", password))
	if rec.Code != http.StatusOK {
		t.Fatalf("login: status = %d, body %s", rec.Code, rec.Body.String())
	}
	session := decodeAs[api.AuthResponse](t, rec)

	// Read and update the profile.
	rec = contract(t, router, http.MethodGet, "/me", withBearer(session.AccessToken))
	if rec.Code != http.StatusOK || decodeAs[api.User](t, rec).Email != "Alice@Example.com" {
		t.Fatalf("GET /me: status = %d, body %s", rec.Code, rec.Body.String())
	}
	rec = contract(t, router, http.MethodPatch, "/me", withBearer(session.AccessToken),
		withBody(`{"display_name":"Ally","target_kcal":2200,"target_protein_g":150}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH /me: status = %d, body %s", rec.Code, rec.Body.String())
	}
	updated := decodeAs[api.User](t, rec)
	if updated.DisplayName != "Ally" || updated.TargetKcal.MustGet() != 2200 || updated.TargetProteinG.MustGet() != 150 {
		t.Errorf("after PATCH: %+v", updated)
	}
	// Clearing one target with null leaves the others (and the name) alone.
	rec = contract(t, router, http.MethodPatch, "/me", withBearer(session.AccessToken), withBody(`{"target_kcal":null}`))
	cleared := decodeAs[api.User](t, rec)
	if !cleared.TargetKcal.IsNull() || cleared.TargetProteinG.MustGet() != 150 || cleared.DisplayName != "Ally" {
		t.Errorf("after clearing kcal: %s", rec.Body.String())
	}
	rec = contract(t, router, http.MethodPatch, "/me", withBearer(session.AccessToken),
		withBody(`{"target_fat_g":-1}`), withInvalidRequest())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PATCH /me with an out-of-range value: status = %d", rec.Code)
	}
	if p := decodeProblemBody(t, rec); len(p.Errors) != 1 || p.Errors[0].Field != "target_fat_g" || p.Errors[0].Code != httpapi.FieldOutOfRange {
		t.Errorf("problem = %+v, want one out_of_range error on target_fat_g", p)
	}

	// Refresh rotates the token; replaying the old one revokes the family.
	rec = contract(t, router, http.MethodPost, "/auth/refresh", withBody(`{"refresh_token":"`+session.RefreshToken+`"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: status = %d, body %s", rec.Code, rec.Body.String())
	}
	rotated := decodeAs[api.AuthResponse](t, rec)
	if rotated.RefreshToken == session.RefreshToken {
		t.Error("refresh token was not rotated")
	}
	rec = contract(t, router, http.MethodPost, "/auth/refresh", withBody(`{"refresh_token":"`+session.RefreshToken+`"}`))
	if rec.Code != http.StatusUnauthorized || problemCode(t, rec) != httpapi.CodeInvalidRefreshToken {
		t.Errorf("replayed refresh token: status = %d, body %s", rec.Code, rec.Body.String())
	}
	rec = contract(t, router, http.MethodPost, "/auth/refresh", withBody(`{"refresh_token":"`+rotated.RefreshToken+`"}`))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("newest token after reuse: status = %d, want 401 (family revoked)", rec.Code)
	}

	// Logout revokes the session and is idempotent.
	rec = contract(t, router, http.MethodPost, "/auth/login", creds("alice@example.com", password))
	session = decodeAs[api.AuthResponse](t, rec)
	for i := 0; i < 2; i++ {
		rec = contract(t, router, http.MethodPost, "/auth/logout", withBody(`{"refresh_token":"`+session.RefreshToken+`"}`))
		if rec.Code != http.StatusNoContent {
			t.Errorf("logout #%d: status = %d, want 204", i+1, rec.Code)
		}
	}
	rec = contract(t, router, http.MethodPost, "/auth/refresh", withBody(`{"refresh_token":"`+session.RefreshToken+`"}`))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("refresh after logout: status = %d, want 401", rec.Code)
	}

	// Delete the account: its access token stops working immediately.
	rec = contract(t, router, http.MethodPost, "/auth/login", creds("alice@example.com", password))
	session = decodeAs[api.AuthResponse](t, rec)
	rec = contract(t, router, http.MethodDelete, "/me", withBearer(session.AccessToken))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /me: status = %d, body %s", rec.Code, rec.Body.String())
	}
	rec = contract(t, router, http.MethodGet, "/me", withBearer(session.AccessToken))
	if rec.Code != http.StatusUnauthorized || problemCode(t, rec) != httpapi.CodeUnauthorized {
		t.Errorf("GET /me after deletion: status = %d, body %s", rec.Code, rec.Body.String())
	}
	rec = contract(t, router, http.MethodPost, "/auth/login", creds("alice@example.com", password))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("login after deletion: status = %d, want 401", rec.Code)
	}
}
```

- [ ] **Step 4: Run them and confirm they fail**

Run: `cd backend && go test ./internal/httpapi/ -count=1 2>&1 | head -8`
Expected: FAIL to build: `unknown field Auth in struct literal of type httpapi.Deps` (and `Tokens`, `Limits`, `TrustedProxies`), `undefined: httpapi.AuthService`, `undefined: httpapi.RateLimits`.

- [ ] **Step 5: Write the implementation**

`backend/internal/httpapi/middleware.go` (the request logger gains `remote_ip`, `user_id`, `duration_ms` and level-by-status; adds `bodyLimit`):

```go
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"regexp"
	"runtime/debug"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

const requestIDHeader = "X-Request-Id"

type ctxKey int

const requestIDKey ctxKey = iota

// A caller-supplied request ID is only trusted when it is short and printable.
var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// RequestID returns the request ID stored in ctx, or "" when there is none.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

// requestID assigns every request an ID, exposes it as X-Request-Id, and
// stores it in the request context.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if !validRequestID.MatchString(id) {
			id = newRequestID()
		}
		w.Header().Set(requestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

func newRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error on supported platforms
	return hex.EncodeToString(b)
}

// requestLogger logs one structured line per request, after it completes.
// Server errors (5xx) log at error level so they can be alerted on by level.
func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
			}
			level := slog.LevelInfo
			if status >= http.StatusInternalServerError {
				level = slog.LevelError
			}
			attrs := []slog.Attr{
				slog.String("request_id", RequestID(r.Context())),
				slog.String("remote_ip", ClientIP(r.Context())),
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", status),
				slog.Int("bytes", ww.BytesWritten()),
				slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
			}
			if id, ok := UserID(r.Context()); ok {
				attrs = append(attrs, slog.String("user_id", id.String()))
			}
			logger.LogAttrs(r.Context(), level, "request", attrs...)
		})
	}
}

// maxBodyBytes caps request bodies; every request body in this API is small JSON.
const maxBodyBytes = 64 << 10

// bodyLimit rejects request bodies larger than maxBodyBytes.
func bodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

// recoverer turns a handler panic into a 500 problem response and logs the stack.
func recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if rec == http.ErrAbortHandler { //nolint:errorlint // http.ErrAbortHandler is compared by identity per net/http docs
					panic(rec)
				}
				logger.ErrorContext(r.Context(), "panic in handler",
					slog.String("request_id", RequestID(r.Context())),
					slog.Any("panic", rec),
					slog.String("stack", string(debug.Stack())),
				)
				WriteProblem(w, http.StatusInternalServerError, CodeInternal, "")
			}()
			next.ServeHTTP(w, r)
		})
	}
}
```

`backend/internal/httpapi/auth.go` (auth state, the spec-driven validator and its error mapping):

```go
package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/google/uuid"
	nethttpmw "github.com/oapi-codegen/nethttp-middleware"
)

const authStateKey ctxKey = iota + 200

// authState is attached to every request context by withAuthState. The
// validator's authentication step fills it in; handlers and the request logger
// read it. A pointer is shared because the validator cannot replace the
// request context.
type authState struct {
	userID uuid.UUID
}

// TokenParser validates an access token and returns the user it was issued to.
type TokenParser interface {
	ParseAccess(token string) (uuid.UUID, error)
}

// UserID returns the authenticated user's ID. It reports false on routes that
// do not require authentication and when authentication failed.
func UserID(ctx context.Context) (uuid.UUID, bool) {
	st, _ := ctx.Value(authStateKey).(*authState)
	if st == nil || st.userID == uuid.Nil {
		return uuid.Nil, false
	}
	return st.userID, true
}

func withAuthState(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authStateKey, &authState{})))
	})
}

var errUnauthenticated = errors.New("unauthenticated")

// openAPIValidator validates every routed request against the OpenAPI
// document: the body and parameters must match the schema, and operations that
// declare `bearerAuth` need a valid access token. Because it is driven by the
// spec's own `security` blocks, a route added to openapi.yaml is protected by
// default; only `security: []` opts out.
func openAPIValidator(spec *openapi3.T, tokens TokenParser, logger *slog.Logger) func(http.Handler) http.Handler {
	return nethttpmw.OapiRequestValidatorWithOptions(spec, &nethttpmw.Options{
		Options: openapi3filter.Options{
			MultiError:         true,
			AuthenticationFunc: authenticate(tokens),
		},
		DoNotValidateServers: true,
		Prefix:               "/v1",
		ErrorHandlerWithOpts: func(ctx context.Context, err error, w http.ResponseWriter, r *http.Request, opts nethttpmw.ErrorHandlerOpts) {
			handleValidationError(logger, err, w, r, opts)
		},
	})
}

func authenticate(tokens TokenParser) openapi3filter.AuthenticationFunc {
	return func(ctx context.Context, in *openapi3filter.AuthenticationInput) error {
		if in.SecuritySchemeName != "bearerAuth" {
			return errUnauthenticated
		}
		scheme, token, ok := strings.Cut(in.RequestValidationInput.Request.Header.Get("Authorization"), " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
			return errUnauthenticated
		}
		id, err := tokens.ParseAccess(token)
		if err != nil {
			return errUnauthenticated
		}
		if st, _ := ctx.Value(authStateKey).(*authState); st != nil {
			st.userID = id
		}
		return nil
	}
}

func handleValidationError(logger *slog.Logger, err error, w http.ResponseWriter, r *http.Request, opts nethttpmw.ErrorHandlerOpts) {
	switch {
	case hasSecurityError(err):
		// Authentication failure wins over body errors, so an unauthenticated
		// caller learns nothing about the request schema.
		w.Header().Set("WWW-Authenticate", "Bearer")
		WriteProblem(w, http.StatusUnauthorized, CodeUnauthorized, "")
	case opts.MatchedRoute == nil:
		WriteProblem(w, http.StatusNotFound, CodeNotFound, "")
	default:
		res, ok := describeValidation(err)
		if !ok {
			logger.ErrorContext(r.Context(), "request validation failed unexpectedly",
				slog.String("request_id", RequestID(r.Context())), slog.Any("err", err))
			WriteProblem(w, http.StatusInternalServerError, CodeInternal, "")
			return
		}
		if res.detail == "" && len(res.fields) == 0 {
			res.detail = "request is invalid"
		}
		WriteValidationProblem(w, res.detail, res.fields)
	}
}

func hasSecurityError(err error) bool {
	var multi openapi3.MultiError
	if errors.As(err, &multi) {
		for _, inner := range multi {
			if hasSecurityError(inner) {
				return true
			}
		}
		return false
	}
	var sec *openapi3filter.SecurityRequirementsError
	return errors.As(err, &sec)
}
```

`backend/internal/httpapi/ratelimit.go`:

```go
package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/httprate"
)

const rateWindow = time.Minute

// RateLimits are the per-minute request limits. Zero values use the defaults.
type RateLimits struct {
	// AuthPerMinute limits /v1/auth/* per client IP (credential-guessing defence).
	AuthPerMinute int
	// UserPerMinute limits every other authenticated request per user.
	UserPerMinute int
}

const (
	defaultAuthPerMinute = 10
	defaultUserPerMinute = 300
)

func (l RateLimits) withDefaults() RateLimits {
	if l.AuthPerMinute <= 0 {
		l.AuthPerMinute = defaultAuthPerMinute
	}
	if l.UserPerMinute <= 0 {
		l.UserPerMinute = defaultUserPerMinute
	}
	return l
}

func rateLimited(w http.ResponseWriter, _ *http.Request) {
	if w.Header().Get("Retry-After") == "" {
		w.Header().Set("Retry-After", strconv.Itoa(int(rateWindow.Seconds())))
	}
	WriteProblem(w, http.StatusTooManyRequests, CodeRateLimited, "")
}

// authIPLimiter limits /v1/auth/* requests per client IP. It runs before
// routing and validation, so malformed and failed attempts count too.
// Counters are in memory: with several API replicas the effective limit is
// per replica.
func authIPLimiter(perMinute int) func(http.Handler) http.Handler {
	limit := httprate.LimitBy(perMinute, rateWindow,
		func(r *http.Request) (string, error) { return ClientIP(r.Context()), nil },
		httprate.WithLimitHandler(rateLimited))
	return func(next http.Handler) http.Handler {
		limited := limit(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/v1/auth/") {
				limited.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// userLimiter limits authenticated requests per user. It must run after the
// validator, which is what identifies the user; unauthenticated requests pass
// through untouched.
func userLimiter(perMinute int) func(http.Handler) http.Handler {
	limit := httprate.LimitBy(perMinute, rateWindow,
		func(r *http.Request) (string, error) {
			id, _ := UserID(r.Context())
			return id.String(), nil
		},
		httprate.WithLimitHandler(rateLimited))
	return func(next http.Handler) http.Handler {
		limited := limit(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := UserID(r.Context()); ok {
				limited.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
```

`backend/internal/httpapi/account.go` (`AuthService` and the seven handlers):

```go
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/InzKazik/mealplanner/backend/internal/api"
	"github.com/InzKazik/mealplanner/backend/internal/service"
)

// AuthService is what the account handlers need from the auth service.
type AuthService interface {
	Register(ctx context.Context, in service.RegisterInput) (service.Session, error)
	Login(ctx context.Context, email, password string) (service.Session, error)
	Refresh(ctx context.Context, rawToken string) (service.Session, error)
	Logout(ctx context.Context, rawToken string) error
	GetUser(ctx context.Context, id uuid.UUID) (service.User, error)
	UpdateUser(ctx context.Context, id uuid.UUID, in service.UpdateInput) (service.User, error)
	DeleteUser(ctx context.Context, id uuid.UUID) error
}

func (s *server) RegisterUser(w http.ResponseWriter, r *http.Request) {
	var req api.RegisterRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	sess, err := s.auth.Register(r.Context(), service.RegisterInput{
		Email: string(req.Email), Password: req.Password, DisplayName: req.DisplayName,
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAuthResponse(sess))
}

func (s *server) LoginUser(w http.ResponseWriter, r *http.Request) {
	var req api.LoginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	sess, err := s.auth.Login(r.Context(), string(req.Email), req.Password)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAuthResponse(sess))
}

func (s *server) RefreshSession(w http.ResponseWriter, r *http.Request) {
	var req api.RefreshRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	sess, err := s.auth.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAuthResponse(sess))
}

func (s *server) LogoutUser(w http.ResponseWriter, r *http.Request) {
	var req api.RefreshRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.auth.Logout(r.Context(), req.RefreshToken); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) GetMe(w http.ResponseWriter, r *http.Request) {
	id, ok := requireUser(w, r)
	if !ok {
		return
	}
	u, err := s.auth.GetUser(r.Context(), id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIUser(u))
}

func (s *server) UpdateMe(w http.ResponseWriter, r *http.Request) {
	id, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.UpdateProfileRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	u, err := s.auth.UpdateUser(r.Context(), id, service.UpdateInput{
		DisplayName:    req.DisplayName,
		TargetKcal:     toOptional(req.TargetKcal),
		TargetProteinG: toOptional(req.TargetProteinG),
		TargetCarbsG:   toOptional(req.TargetCarbsG),
		TargetFatG:     toOptional(req.TargetFatG),
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIUser(u))
}

func (s *server) DeleteMe(w http.ResponseWriter, r *http.Request) {
	id, ok := requireUser(w, r)
	if !ok {
		return
	}
	if err := s.auth.DeleteUser(r.Context(), id); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// requireUser returns the authenticated user's ID. The validator guarantees it
// on secured routes; the check is a second line of defence.
func requireUser(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, ok := UserID(r.Context())
	if !ok {
		w.Header().Set("WWW-Authenticate", "Bearer")
		WriteProblem(w, http.StatusUnauthorized, CodeUnauthorized, "")
	}
	return id, ok
}

// decodeJSON decodes the request body. The validator has already checked it
// against the schema, so a failure here is unexpected but still answered as a
// client error.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		WriteValidationProblem(w, "request body is not valid JSON", nil)
		return false
	}
	return true
}

func (s *server) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrEmailTaken):
		WriteProblem(w, http.StatusConflict, CodeEmailTaken, "")
	case errors.Is(err, service.ErrInvalidCredentials):
		WriteProblem(w, http.StatusUnauthorized, CodeInvalidCredentials, "")
	case errors.Is(err, service.ErrInvalidRefreshToken):
		WriteProblem(w, http.StatusUnauthorized, CodeInvalidRefreshToken, "")
	case errors.Is(err, service.ErrNotFound):
		// Only the signed-in user's own account is looked up: a valid token
		// for an account that no longer exists is simply no longer authorized.
		w.Header().Set("WWW-Authenticate", "Bearer")
		WriteProblem(w, http.StatusUnauthorized, CodeUnauthorized, "")
	default:
		s.logger.ErrorContext(r.Context(), "unhandled service error",
			slog.String("request_id", RequestID(r.Context())), slog.Any("err", err))
		WriteProblem(w, http.StatusInternalServerError, CodeInternal, "")
	}
}

func toAuthResponse(s service.Session) api.AuthResponse {
	return api.AuthResponse{
		AccessToken:  s.AccessToken,
		RefreshToken: s.RefreshToken,
		TokenType:    api.AuthResponseTokenTypeBearer,
		ExpiresIn:    int(s.ExpiresIn.Seconds()),
		User:         toAPIUser(s.User),
	}
}

func toAPIUser(u service.User) api.User {
	return api.User{
		Id:             u.ID,
		Email:          u.Email,
		DisplayName:    u.DisplayName,
		TargetKcal:     toNullable(u.TargetKcal),
		TargetProteinG: toNullable(u.TargetProteinG),
		TargetCarbsG:   toNullable(u.TargetCarbsG),
		TargetFatG:     toNullable(u.TargetFatG),
		CreatedAt:      u.CreatedAt,
		UpdatedAt:      u.UpdatedAt,
	}
}

// toNullable renders a nullable column as an explicit JSON null when unset.
func toNullable(v *float64) nullable.Nullable[float64] {
	if v == nil {
		return nullable.NewNullNullable[float64]()
	}
	return nullable.NewNullableWithValue(*v)
}

// toOptional maps a PATCH field to "unchanged", "clear" or "set".
func toOptional(n nullable.Nullable[float64]) service.Optional[float64] {
	switch {
	case !n.IsSpecified():
		return service.Optional[float64]{}
	case n.IsNull():
		return service.Set[float64](nil)
	default:
		v := n.MustGet()
		return service.Set(&v)
	}
}
```

`backend/internal/httpapi/server.go` (the `api.Unimplemented` embed is gone; `/readyz` gets its timeout):

```go
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/InzKazik/mealplanner/backend/internal/api"
)

const readyTimeout = 2 * time.Second

// server implements api.ServerInterface. The account endpoints live in account.go.
type server struct {
	logger *slog.Logger
	ready  func(context.Context) error
	auth   AuthService
}

var _ api.ServerInterface = (*server)(nil)

func (s *server) GetHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, api.Health{Status: api.HealthStatusOk})
}

func (s *server) GetReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
	defer cancel()
	if err := s.ready(ctx); err != nil {
		s.logger.WarnContext(r.Context(), "readiness check failed",
			slog.String("request_id", RequestID(r.Context())),
			slog.Any("err", err),
		)
		WriteProblem(w, http.StatusServiceUnavailable, CodeNotReady, "database is not reachable")
		return
	}
	writeJSON(w, http.StatusOK, api.Health{Status: api.HealthStatusOk})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
```

`backend/internal/httpapi/router.go`:

```go
// Package httpapi holds the HTTP layer: routing, request decoding and response
// encoding. It calls services and never touches SQL.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"

	"github.com/InzKazik/mealplanner/backend/internal/api"
)

// Deps are the collaborators the router needs. Logger, Ready, Auth and Tokens
// are required: NewRouter panics without them rather than failing on the first
// request.
type Deps struct {
	Logger *slog.Logger
	// Ready reports whether the service can take traffic (for example the
	// database answers). It backs GET /v1/readyz.
	Ready func(ctx context.Context) error
	// WebOrigin is the single browser origin allowed by CORS.
	WebOrigin string
	// Auth implements the account endpoints.
	Auth AuthService
	// Tokens validates access tokens for secured operations.
	Tokens TokenParser
	// Limits are the rate limits; zero values use the defaults.
	Limits RateLimits
	// TrustedProxies is how many reverse proxies sit in front of the API and
	// append to X-Forwarded-For. 0 means the peer address is the client.
	TrustedProxies int
}

// NewRouter returns the root handler with every /v1 route and all middleware.
func NewRouter(d Deps) http.Handler {
	if d.Logger == nil || d.Ready == nil || d.Auth == nil || d.Tokens == nil {
		panic("httpapi: Deps.Logger, Ready, Auth and Tokens are required")
	}
	limits := d.Limits.withDefaults()
	spec, err := api.GetSpec()
	if err != nil {
		panic("httpapi: load embedded OpenAPI document: " + err.Error())
	}

	r := chi.NewRouter()

	r.Use(requestID)
	r.Use(withAuthState)
	r.Use(clientIP(d.TrustedProxies))
	r.Use(requestLogger(d.Logger))
	r.Use(recoverer(d.Logger))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins: []string{d.WebOrigin},
		AllowedMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowedHeaders: []string{"Authorization", "Content-Type"},
		ExposedHeaders: []string{requestIDHeader, "Retry-After"},
		MaxAge:         300,
	}))
	r.Use(bodyLimit)
	r.Use(authIPLimiter(limits.AuthPerMinute))

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		WriteProblem(w, http.StatusNotFound, CodeNotFound, "")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		if allowed := allowedMethods(r, req.URL.Path); len(allowed) > 0 {
			w.Header().Set("Allow", strings.Join(allowed, ", "))
		}
		WriteProblem(w, http.StatusMethodNotAllowed, CodeMethodNotAllowed, "")
	})

	srv := &server{logger: d.Logger, ready: d.Ready, auth: d.Auth}
	api.HandlerWithOptions(srv, api.ChiServerOptions{
		BaseURL:    "/v1",
		BaseRouter: r,
		// The generated wrapper applies these in order, so the last one is the
		// outermost: the validator (which authenticates) runs first, then the
		// per-user rate limit, then the handler.
		Middlewares: []api.MiddlewareFunc{
			userLimiter(limits.UserPerMinute),
			openAPIValidator(spec, d.Tokens, d.Logger),
		},
		ErrorHandlerFunc: func(w http.ResponseWriter, req *http.Request, err error) {
			d.Logger.WarnContext(req.Context(), "generated wrapper rejected a request",
				slog.String("request_id", RequestID(req.Context())), slog.Any("err", err))
			WriteProblem(w, http.StatusBadRequest, CodeValidationFailed, "invalid request parameter")
		},
	})
	return r
}

// allowedMethods lists the methods mux has a handler for at path, so a 405
// response can carry the Allow header RFC 9110 requires.
func allowedMethods(mux *chi.Mux, path string) []string {
	var allowed []string
	for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if mux.Match(chi.NewRouteContext(), m, path) {
			allowed = append(allowed, m)
		}
	}
	return allowed
}
```

The `Middlewares` slice order matters: the generated wrapper applies them in order, so the last one is outermost. The validator (which authenticates) runs first, then the per-user limiter, then the handler. Do not reorder them.

- [ ] **Step 6: Tidy, format and run the tests**

Run: `cd backend && go mod tidy && grep '^go ' go.mod && gofmt -l . && go vet ./... && go test ./internal/httpapi/ -count=1 -v 2>&1 | grep -E '^(--- |ok|FAIL|panic)'`
Expected: go line `go 1.26`/`go 1.26.0`; gofmt prints nothing; every test `PASS` (not `SKIP`), in particular `TestAccountLifecycle`, `TestRequestValidationProblems`, `TestSecuredOperationsRequireAValidAccessToken`, `TestAuthEndpointsAreRateLimitedPerClientIP`, `TestBadRequestsCountTowardTheAuthRateLimit`, `TestAuthenticatedRequestsAreRateLimitedPerUser`, `TestTrustedProxyHeaderDecidesTheRateLimitedClient`, `TestNewRouterPanicsWithoutRequiredDependencies`, `TestReadyzGivesUpOnAHungDatabase`, and the request-log tests.

- [ ] **Step 7: Prove two of the tests can fail**

Mutation 1: in `router.go` swap the two entries of the `Middlewares` slice (put `openAPIValidator(...)` before `userLimiter(...)`). Run `cd backend && go test ./internal/httpapi/ -count=1`.
Expected: FAIL in `TestAuthenticatedRequestsAreRateLimitedPerUser` (the limiter then runs before authentication, so it never sees the user). Restore the original order and confirm the package passes again.

Mutation 2: in `validation.go` change the registered format name `"email"` to `"emailx"`. Run the same command.
Expected: FAIL in `TestDescribeValidation` and `TestRequestValidationProblems` (an invalid email is accepted). Restore it and confirm green.

- [ ] **Step 8: Lint and commit**

Run: `make lint-backend`
Expected: exits 0 (`0 issues.`).

```bash
git add backend
git commit -m "feat(backend): authenticate and validate through the spec, rate limit, and add the account handlers"
```

---

### Task 10: Wire the server, tooling and a smoke test (TDD, needs Docker)

**Files:**
- Modify: `backend/cmd/api/main_test.go` (full replacement)
- Modify: `backend/cmd/api/main.go` (full replacement)
- Modify: `Makefile`, `.env.example`

**Interfaces:**
- Consumes: `config.Config.JWTSecret` / `TrustedProxies` (Task 6), `store.New`, `service.NewAuth`, `auth.NewTokenIssuer`, `auth.NewHasher(auth.DefaultHashParams)`, `httpapi.Deps` (Tasks 3 to 9).
- Produces: a running API with the account endpoints (access tokens live 15 minutes, refresh tokens 30 days); after the first SIGINT/SIGTERM default signal handling is restored, so a second one force-quits during the drain; `make run-api` defaults `JWT_SECRET` to a development-only value; `.env.example` documents `JWT_SECRET` and `TRUSTED_PROXY_COUNT`.

- [ ] **Step 1: Replace the server tests**

```go
package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/InzKazik/mealplanner/backend/internal/config"
	"github.com/InzKazik/mealplanner/backend/internal/testutil"
)

const testSecret = "0123456789abcdef0123456789abcdef"

// runningServer is a serve() instance listening on a loopback port.
type runningServer struct {
	baseURL string
	addr    string
	stop    context.CancelFunc
	done    chan error
	client  *http.Client
}

// startServer runs serve against dbURL and returns once /v1/readyz answers 200.
// It fails fast if serve exits early instead of waiting for a timeout.
func startServer(t *testing.T, dbURL string) *runningServer {
	t.Helper()
	cfg := config.Config{DatabaseURL: dbURL, WebOrigin: "http://localhost:3000", JWTSecret: testSecret}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	s := &runningServer{
		baseURL: "http://" + ln.Addr().String() + "/v1",
		addr:    ln.Addr().String(),
		stop:    cancel,
		done:    make(chan error, 1),
		client:  &http.Client{Timeout: 2 * time.Second},
	}
	go func() { s.done <- serve(ctx, cfg, slog.New(slog.DiscardHandler), ln) }()

	deadline := time.Now().Add(10 * time.Second)
	for {
		select {
		case err := <-s.done:
			t.Fatalf("serve exited before it became ready: %v", err)
		default:
		}
		resp, err := s.client.Get(s.baseURL + "/readyz")
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return s
			}
			t.Fatalf("GET /readyz = %d, want 200", resp.StatusCode)
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never became reachable: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (s *runningServer) post(t *testing.T, path, body string) (int, []byte) {
	t.Helper()
	resp, err := s.client.Post(s.baseURL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func TestServeReportsReadyThenShutsDownCleanly(t *testing.T) {
	s := startServer(t, testutil.NewDatabase(t))

	s.stop()

	select {
	case err := <-s.done:
		if err != nil {
			t.Fatalf("serve returned %v after cancel, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return within 5s of cancellation")
	}
	if conn, err := net.DialTimeout("tcp", s.addr, time.Second); err == nil {
		_ = conn.Close()
		t.Error("port still accepts connections after shutdown")
	}
}

// TestServeWiresTheAccountEndpoints proves the real server (config, database,
// service, tokens and router together) can register a user and authenticate
// that user's next request.
func TestServeWiresTheAccountEndpoints(t *testing.T) {
	s := startServer(t, testutil.NewMigratedDatabase(t))

	status, body := s.post(t, "/auth/register", `{"email":"a@example.com","password":"a-long-enough-password","display_name":"A"}`)
	if status != http.StatusCreated {
		t.Fatalf("register: status = %d, body %s", status, body)
	}
	var session struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &session); err != nil || session.AccessToken == "" {
		t.Fatalf("register: no access token in %s (%v)", body, err)
	}

	req, _ := http.NewRequest(http.MethodGet, s.baseURL+"/me", nil)
	req.Header.Set("Authorization", "Bearer "+session.AccessToken)
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatalf("GET /me: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /me with the issued token: status = %d, want 200", resp.StatusCode)
	}

	resp, err = s.client.Get(s.baseURL + "/me")
	if err != nil {
		t.Fatalf("GET /me: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("GET /me without a token: status = %d, want 401", resp.StatusCode)
	}
}

func TestServeFailsWhenDatabaseIsUnreachable(t *testing.T) {
	cfg := config.Config{
		DatabaseURL: "postgres://127.0.0.1:1/none?sslmode=disable&connect_timeout=1",
		WebOrigin:   "http://localhost:3000",
		JWTSecret:   testSecret,
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	if err := serve(context.Background(), cfg, slog.New(slog.DiscardHandler), ln); err == nil {
		t.Fatal("serve succeeded against an unreachable database, want error")
	}
}
```

The tests now use a timed HTTP client and stop waiting the moment `serve` exits, so a failure cannot hang until the go test timeout.

- [ ] **Step 2: Run them and confirm the new one fails**

Run: `cd backend && go test ./cmd/api/ -run TestServeWiresTheAccountEndpoints -count=1 2>&1 | grep -E 'panic|FAIL|required'`
Expected: FAIL: `panic: httpapi: Deps.Logger, Ready, Auth and Tokens are required` (the old `main.go` never supplied an auth service).

- [ ] **Step 3: Replace `main.go`**

```go
// Command api runs the Meal Planner HTTP API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/InzKazik/mealplanner/backend/internal/auth"
	"github.com/InzKazik/mealplanner/backend/internal/config"
	"github.com/InzKazik/mealplanner/backend/internal/db"
	"github.com/InzKazik/mealplanner/backend/internal/httpapi"
	"github.com/InzKazik/mealplanner/backend/internal/service"
	"github.com/InzKazik/mealplanner/backend/internal/store"
)

const (
	shutdownTimeout = 10 * time.Second
	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 30 * 24 * time.Hour
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "api:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// After the first signal, restore default handling so a second Ctrl-C
	// force-quits instead of being swallowed while the server drains.
	go func() {
		<-ctx.Done()
		stop()
	}()

	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Addr, err)
	}
	logger.Info("api listening", "addr", ln.Addr().String())

	return serve(ctx, cfg, logger, ln)
}

// serve runs the API on ln until ctx is cancelled, then drains in-flight
// requests for up to shutdownTimeout. It returns nil after a clean shutdown.
func serve(ctx context.Context, cfg config.Config, logger *slog.Logger, ln net.Listener) error {
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	tokens := auth.NewTokenIssuer([]byte(cfg.JWTSecret), accessTokenTTL, time.Now)
	accounts := service.NewAuth(store.New(pool), auth.NewHasher(auth.DefaultHashParams), tokens, refreshTokenTTL, time.Now)

	srv := &http.Server{
		Handler: httpapi.NewRouter(httpapi.Deps{
			Logger:         logger,
			Ready:          pool.Ping,
			WebOrigin:      cfg.WebOrigin,
			Auth:           accounts,
			Tokens:         tokens,
			TrustedProxies: cfg.TrustedProxies,
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	select {
	case err := <-serveErr:
		return fmt.Errorf("server stopped unexpectedly: %w", err)
	case <-ctx.Done():
	}

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		_ = srv.Close()
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("server: %w", err)
	}
	return nil
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run: `cd backend && gofmt -l . && go vet ./... && go test ./cmd/api/ -count=1 -v 2>&1 | grep -E '^(--- |ok|FAIL)'`
Expected: `TestServeReportsReadyThenShutsDownCleanly`, `TestServeWiresTheAccountEndpoints` and `TestServeFailsWhenDatabaseIsUnreachable` all `PASS` (not `SKIP`).

- [ ] **Step 5: Update the Makefile and `.env.example`**

Recipe lines **must** start with a real tab character.

```makefile
.DEFAULT_GOAL := help
REDOCLY := npx --yes @redocly/cli@2.53.3
GOLANGCI := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2
OAPICODEGEN := go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.0
SQLC := go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1
GENERATED := backend/internal/api backend/internal/store/sqlc

# Local development database; matches the docker-compose.yml defaults.
migrate run-api: export DATABASE_URL ?= postgres://mealplanner:mealplanner@localhost:5432/mealplanner?sslmode=disable

# Development-only signing secret so `make run-api` works out of the box. Never use it anywhere else.
run-api: export JWT_SECRET ?= dev-only-secret-change-me-0123456789

.PHONY: help lint-api test-backend lint-backend generate check-generated migrate run-api db-up db-down check

help: ## List available targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-16s %s\n", $$1, $$2}'

lint-api: ## Lint openapi.yaml
	$(REDOCLY) lint openapi.yaml

test-backend: ## Vet and test the Go backend (needs Docker)
	cd backend && go vet ./... && go test ./...

lint-backend: ## Run golangci-lint on the Go backend
	cd backend && $(GOLANGCI) run ./...

generate: ## Regenerate backend code (oapi-codegen from openapi.yaml, sqlc from migrations and queries)
	cd backend && $(OAPICODEGEN) -config internal/api/oapi.yaml ../openapi.yaml
	cd backend && $(SQLC) generate

check-generated: generate ## Fail if the committed generated code is out of date
	git add -AN -- $(GENERATED)
	git diff --exit-code -- $(GENERATED)

migrate: ## Apply database migrations to DATABASE_URL
	cd backend && go run ./cmd/migrate

run-api: ## Run the API locally on :8080
	cd backend && go run ./cmd/api

db-up: ## Start local Postgres and wait until healthy
	docker compose up -d --wait postgres

db-down: ## Stop local Postgres (data is kept)
	docker compose down

check: lint-api test-backend lint-backend check-generated ## Everything CI runs, except the compose workflow
```

`.env.example`:

```dotenv
# Docker Compose reads .env: copy this file to .env and adjust. Other processes (including the API) read the shell environment only.

# Local Postgres (docker-compose.yml)
POSTGRES_USER=mealplanner
POSTGRES_PASSWORD=mealplanner
POSTGRES_DB=mealplanner
POSTGRES_PORT=5432

# API (backend/cmd/api, backend/cmd/migrate): read from the shell environment, not from .env
# `make run-api` and `make migrate` default DATABASE_URL to the compose database below,
# and `make run-api` defaults JWT_SECRET to a development-only value.
API_ADDR=:8080
# Assumes POSTGRES_PORT=5432: if you change POSTGRES_PORT above, change the port in this URL too.
DATABASE_URL=postgres://mealplanner:mealplanner@localhost:5432/mealplanner?sslmode=disable
WEB_ORIGIN=http://localhost:3000
# Signs access tokens (HS256). At least 32 bytes and secret: generate one with `openssl rand -base64 48`.
# Rotating it signs every user out. There is deliberately no default outside `make run-api`.
JWT_SECRET=
# How many reverse proxies sit in front of the API and append to X-Forwarded-For (0 = clients connect
# directly). Getting this wrong lets clients spoof their IP and dodge per-IP rate limits.
TRUSTED_PROXY_COUNT=0
```

- [ ] **Step 6: Smoke-test the real binary against compose Postgres**

Shell state does not persist between commands, so run this whole script in ONE command from the repo root. It needs Docker and a free port 8080 (`ss -ltn | grep -c ':8080 '` must print 0 first).

```bash
set -u
DB='postgres://mealplanner:mealplanner@localhost:5432/mealplanner?sslmode=disable'
export JWT_SECRET='smoke-test-secret-smoke-test-secret-00'
J='Content-Type: application/json'
field() { python3 -c "import sys,json; print(json.load(sys.stdin)['$1'])"; }

make db-up
make migrate                       # expect "count":3
make migrate                       # expect "count":0
(cd backend && go build -o /tmp/mealplanner-api ./cmd/api)
DATABASE_URL="$DB" /tmp/mealplanner-api > /tmp/mealplanner-api.log 2>&1 &
API_PID=$!
sleep 2

echo "--- register";              R=$(curl -s -H "$J" -d '{"email":"smoke@example.com","password":"a-long-enough-password","display_name":"Smoke"}' localhost:8080/v1/auth/register); echo "$R" | cut -c1-120
ACCESS=$(echo "$R" | field access_token); REFRESH=$(echo "$R" | field refresh_token)
echo "--- register again (expect 409 email_taken)"; curl -s -w ' [%{http_code}]\n' -H "$J" -d '{"email":"SMOKE@example.com","password":"a-long-enough-password","display_name":"X"}' localhost:8080/v1/auth/register
echo "--- bad body (expect 400 with errors)"; curl -s -w ' [%{http_code}]\n' -H "$J" -d '{"email":"nope","password":"short"}' localhost:8080/v1/auth/register
echo "--- GET /me without a token (expect 401)"; curl -s -i localhost:8080/v1/me | grep -i -E '^(HTTP|www-authenticate)'
echo "--- GET /me";               curl -s -w ' [%{http_code}]\n' -H "Authorization: Bearer $ACCESS" localhost:8080/v1/me
echo "--- PATCH /me";             curl -s -w ' [%{http_code}]\n' -X PATCH -H "$J" -H "Authorization: Bearer $ACCESS" -d '{"display_name":"Smoky","target_kcal":2200}' localhost:8080/v1/me
echo "--- refresh";               R2=$(curl -s -H "$J" -d "{\"refresh_token\":\"$REFRESH\"}" localhost:8080/v1/auth/refresh); echo "$R2" | cut -c1-80
NEWREFRESH=$(echo "$R2" | field refresh_token)
echo "--- replay the old refresh token (expect 401 invalid_refresh_token)"; curl -s -w ' [%{http_code}]\n' -H "$J" -d "{\"refresh_token\":\"$REFRESH\"}" localhost:8080/v1/auth/refresh
echo "--- the newest token is now revoked too (expect 401)"; curl -s -o /dev/null -w '[%{http_code}]\n' -H "$J" -d "{\"refresh_token\":\"$NEWREFRESH\"}" localhost:8080/v1/auth/refresh
echo "--- DELETE /me";            curl -s -o /dev/null -w '[%{http_code}]\n' -X DELETE -H "Authorization: Bearer $ACCESS" localhost:8080/v1/me
echo "--- GET /me after deletion (expect 401)"; curl -s -o /dev/null -w '[%{http_code}]\n' -H "Authorization: Bearer $ACCESS" localhost:8080/v1/me
echo "--- rate limit: 12 more login attempts, statuses:"; for i in $(seq 1 12); do curl -s -o /dev/null -w '%{http_code} ' -H "$J" -d '{"email":"smoke@example.com","password":"wrong-password"}' localhost:8080/v1/auth/login; done; echo
echo "--- 429 body";              curl -s -i -H "$J" -d '{"email":"smoke@example.com","password":"wrong-password"}' localhost:8080/v1/auth/login | grep -i -E '^(HTTP|retry-after|content-type)|"code"'
echo "--- request log line for a request with a user"; grep '"user_id"' /tmp/mealplanner-api.log | head -1 | cut -c1-260
kill -TERM "$API_PID"; wait "$API_PID"; echo "api exit=$?"
docker compose down -v
rm -f /tmp/mealplanner-api /tmp/mealplanner-api.log
```

Expected:
- `make migrate` logs `"count":3`, then `"count":0`.
- register prints a JSON body starting with `{"access_token":"eyJ...`.
- register again: `email_taken` and `[409]`.
- bad body: `validation_failed` with `errors` listing `display_name` `required`, `email` `invalid_format`, `password` `too_short`, and `[400]`.
- `GET /me` without a token: `HTTP/1.1 401` and `WWW-Authenticate: Bearer`.
- `GET /me` with the token: the profile with `null` targets and `[200]`; `PATCH /me` returns `"display_name":"Smoky"` and `"target_kcal":2200`, `[200]`.
- refresh returns a new pair; replaying the old refresh token is `invalid_refresh_token` `[401]`; the newest refresh token is then `[401]` too (family revoked).
- `DELETE /me` is `[204]`; `GET /me` afterwards is `[401]`.
- the rate-limit loop prints some `401` statuses followed by `429`s; the 429 body has `rate_limited` and a `Retry-After` header.
- the logged request line contains `remote_ip` and `user_id`.
- `api exit=0` after SIGTERM; port 8080 is free at the end and no containers remain.

- [ ] **Step 7: Lint and commit**

Run: `make lint-backend`
Expected: exits 0.

```bash
git add Makefile .env.example backend
git commit -m "feat(backend): wire the auth service into the server"
```

---

### Task 11: Linters, documentation and the final check

**Files:**
- Modify: `backend/.golangci.yml`
- Modify: `CLAUDE.md`, `backend/CLAUDE.md`, `AGENTS.md`

**Interfaces:**
- Consumes: everything above. Produces the documented conventions and stricter static checks (`bodyclose`, `gosec`, `sqlclosecheck`) that the rest of the backend is written against.

- [ ] **Step 1: Enable the extra linters**

`backend/.golangci.yml`:

```yaml
version: "2"
linters:
  default: standard
  enable:
    - errorlint
    - bodyclose
    - gosec
    - sqlclosecheck
formatters:
  enable:
    - gofmt
```

Run: `make lint-backend`
Expected: exits 0 (`0 issues.`). The code from Tasks 3 to 10 was written to pass these, including: `errors.As` instead of type switches on errors, `api.GetSpec` and `openapi3.DefineStringFormatValidator` instead of their deprecated forms, and one targeted `//nolint:gosec` on the `CodeInvalidCredentials` error-code constant (its name trips the hard-coded-credential rule).

- [ ] **Step 2: Replace `backend/CLAUDE.md`**

````markdown
# Backend (Go)

Module: `github.com/InzKazik/mealplanner/backend`. Go 1.26, `chi` router, `pgx` + `goose` + `sqlc` for Postgres.

## Layout

- `cmd/api/`: process entry point. Loads config, connects to Postgres, wires the services, serves HTTP, shuts down gracefully on SIGINT/SIGTERM. No business logic.
- `cmd/migrate/`: applies pending migrations and exits. The API never applies migrations.
- `internal/config/`: environment configuration and its validation.
- `internal/api/`: **generated** from `openapi.yaml` (`api.gen.go`, never hand-edit) plus its `oapi-codegen` config (`oapi.yaml`). The generated file also embeds the spec (`api.GetSpec()`), which the request validator uses.
- `internal/httpapi/`: HTTP layer. Router, middleware (request ID, client IP, logging, panic recovery, CORS, body limit, rate limits), the OpenAPI request validator that also authenticates, the problem+json writer, and the handlers that implement `api.ServerInterface` (`account.go`, `server.go`). Calls services only.
- `internal/auth/`: password hashing (argon2id) and access/refresh tokens. Pure: no database or HTTP.
- `internal/service/`: business rules. `Auth` covers registration, login, refresh-token sessions and the signed-in user's account. Never speaks HTTP.
- `internal/store/`: **all SQL**. `queries/*.sql` is the source; `sqlc/` is generated from it and the migrations (never hand-edit). `Store` adds transactions.
- `internal/db/`: pgx pool and the goose migration runner.
- `internal/testutil/`: integration-test helpers (Postgres via testcontainers; a migrated template database copied per test).
- `migrations/`: embedded goose SQL migrations.

Dependencies point one way: `httpapi` → `service` → `store`; `auth` is a leaf used by `service` and `config`. A package never imports one to its left.

## Commands (from repo root)

- `make test-backend`: `go vet ./...` and `go test ./...`. **Needs Docker** (integration tests start Postgres with testcontainers). Without Docker they skip locally but fail when `CI` is set, so CI cannot pass silently without them.
- `make lint-backend`: runs golangci-lint v2.13.2 via `go run` (the same command CI uses), including `gofmt`, `gosec`, `bodyclose` and `sqlclosecheck`.
- `make generate`: regenerate `internal/api/api.gen.go` (oapi-codegen) and `internal/store/sqlc/` (sqlc)
- `make check-generated`: fail if the committed generated code differs from the spec, migrations and queries, or if a generated file is not committed
- `make migrate`: apply migrations to `DATABASE_URL` (defaults to the compose database)
- `make run-api`: run on `API_ADDR` from the process environment (default `:8080`; `.env` is not loaded). Needs `make db-up` and `make migrate` first. It defaults `JWT_SECRET` to a development-only value; every other environment must set its own.

## Conventions

- **Routes:** edit `openapi.yaml` first, run `make generate`, then implement the new `api.ServerInterface` method. The build fails until you do. Handlers are non-strict (`w, r`); the contract tests and the request validator catch drift instead of typed responses.
- **Auth is decided by the spec.** The global `security: bearerAuth` protects every operation; `security: []` opts one out (only health checks and `auth/*`). The validator in `internal/httpapi/auth.go` enforces it and puts the user in the request context (`httpapi.UserID(ctx)`), so a route added to the spec is protected by default. Never add path-based auth checks.
- **Requests are validated by the schema** (shape, lengths, formats, unknown fields), reported as `400 validation_failed` with per-field `errors`. Service code enforces business rules only.
- **Contract tests:** every new endpoint gets a test that goes through `contract(...)` in `internal/httpapi/contract_test.go`, which validates the request and the response against `openapi.yaml` (use `withInvalidRequest()` for deliberately bad requests). Error responses use the shared `components/responses` (`BadRequest`, `Unauthorized`, `Conflict`, `TooManyRequests`).
- **Errors** use `httpapi.WriteProblem` / `WriteValidationProblem` (RFC 9457 `application/problem+json`) with a stable `code` constant. Add new codes to `problem.go` and document them in the spec.
- **SQL** lives only in `internal/store/queries/*.sql`; run `make generate` and commit the output. Services take a `*store.Store`, use `InTx` for multi-statement changes, and translate database errors into service errors (`store.IsUniqueViolation`, `store.IsNotFound`).
- **Secrets never reach logs or errors**: no passwords, tokens, refresh tokens, JWT secret or query strings. Compare secrets in constant time (argon2id and JWT libraries do).
- Every response carries `X-Request-Id`. Log with `slog` (JSON to stdout) and include `httpapi.RequestID(ctx)`. No `fmt.Println` in non-test code.
- **Migrations** are goose SQL files named `NNNNN_description.sql` in `migrations/`, forward-only in production. Every table has `created_at` and `updated_at`, and an `updated_at` trigger that uses `set_updated_at()`.
- **Tests** are table-driven where there are several cases. Handler tests use `httptest`; integration tests use a real Postgres via `testutil.NewMigratedDatabase` (or `NewDatabase` for an empty one), never mocks. Use light argon2 parameters in tests (`auth.HashParams{MemoryKiB: 8, Iterations: 1, Parallelism: 1}`).
- Configuration comes from environment variables and is documented in `.env.example`.

## Behaviour worth knowing

- Refresh tokens rotate on every use. Presenting a token that was already used or revoked revokes the whole session family, so a client that retries a refresh after a network failure gets signed out: clients must serialize refreshes and keep only the newest token.
- Login answers unknown email and wrong password identically and spends the same hashing time. Registering an existing email returns `409 email_taken`, which does reveal that the email exists.
- Access tokens are stateless and live 15 minutes, so a deleted account's token is rejected only because `/me` looks the user up (401 `unauthorized`).
- Rate limits (10 auth requests per minute per client IP, 300 requests per minute per user) are in memory: with several API replicas the effective limit is per replica. `TRUSTED_PROXY_COUNT` must match the real number of proxies, or clients can forge their IP.

## Not built yet

- **Sign in with Apple** (`POST /v1/auth/apple`): its own plan, needs Apple Developer credentials (Service ID, keys) to test against.
- Email verification and password reset; deleting expired or revoked refresh tokens (a scheduled cleanup).
- The domain: ingredients, meals, diets, plan, shopping lists, partners (later plans).
- Shared `components/responses` for `404` and `405` (the router returns problem+json for both, but they are outside the contract, so contract tests cannot check them).

## Carried forward (hardening to schedule)

- `goose` session locker in `db.Migrate` before any multi-replica deploy.
- `recoverer` after a partial write appends a problem body to the partial response.
- `cmd/migrate` has no signal handling and reports errors as plain text.
- Later (shopping-list SSE plan): the 30s server `WriteTimeout` cuts event streams: override it per handler with `http.NewResponseController(w).SetWriteDeadline(time.Time{})` (chi's response wrapper supports `Unwrap`). `Server.Shutdown` does not cancel request contexts and a live stream never goes idle, so set `BaseContext` or `RegisterOnShutdown` so streams end on shutdown, otherwise every shutdown with a connected client burns the full 10s and exits 1.
````

- [ ] **Step 3: Update `CLAUDE.md`**

Make these four replacements in `CLAUDE.md` (each old string appears exactly once):

1. Replace
`- **Auth:** email/password + Sign in with Apple. JWT access tokens (15 min) + rotating refresh tokens.`
with
`- **Auth:** email/password (built) and Sign in with Apple (own plan). HS256 JWT access tokens (15 min) + opaque rotating refresh tokens (30 days, stored hashed; replaying a used one revokes the session family). Enforcement comes from the `security` blocks in `openapi.yaml`: global `bearerAuth`, `security: []` opts a route out, so a new route is protected by default.`

2. Replace
`| `make generate` | Regenerate backend code from `openapi.yaml` |`
with
`| `make generate` | Regenerate backend code: oapi-codegen from `openapi.yaml`, sqlc from migrations and queries |`

3. Replace
`The API reads its variables (for example `API_ADDR`, `DATABASE_URL`) from the shell environment and does not load `.env`; the Makefile defaults `DATABASE_URL` to the compose database.`
with
`The API reads its variables (`API_ADDR`, `DATABASE_URL`, `JWT_SECRET`, ...) from the shell environment and does not load `.env`; the Makefile defaults `DATABASE_URL` to the compose database and, for `make run-api` only, `JWT_SECRET` to a development-only value.`

4. Replace
`- Never hand-edit generated code. Regenerate it from `openapi.yaml`.`
with
`- Never hand-edit generated code (`backend/internal/api`, `backend/internal/store/sqlc`). Regenerate it with `make generate`.`

- [ ] **Step 4: Update `AGENTS.md`**

In the first "Boundaries" bullet, replace
`(`backend/internal/api/api.gen.go`, API clients, `sqlc` output)`
with
`(`backend/internal/api/api.gen.go`, `backend/internal/store/sqlc/`, API clients)`.

- [ ] **Step 5: Run the whole check**

Run: `make check`
Expected: exits 0: `lint-api` valid with `2 problems are explicitly ignored`; `go vet` clean and every package `ok`; `lint-backend` `0 issues.`; `check-generated` exits 0.
Run: `cd backend && go test ./... -count=1 -v 2>&1 | grep -E '^(--- SKIP|--- FAIL)'`
Expected: prints nothing (no test skipped or failed: Docker was used).
Run: `git status --short`
Expected: prints nothing after the commit below.
Confirm every `make` target named in `CLAUDE.md` and `backend/CLAUDE.md` exists in `make help`.

- [ ] **Step 6: Commit**

```bash
git add backend/.golangci.yml CLAUDE.md backend/CLAUDE.md AGENTS.md
git commit -m "docs: document auth, the spec-driven enforcement and the stricter linters"
```

---

## Self-review against the spec

| Spec requirement | Covered by |
|---|---|
| 4.1 `POST auth/register`, `auth/login`, `auth/refresh`, `auth/logout` | Tasks 1, 5, 9 |
| 4.1 `GET/PATCH me` (profile and targets), `DELETE me` | Tasks 1, 5, 9 |
| 4.1 `POST auth/apple` | **Deferred** to its own plan (decision 8) |
| 3.1 `users` (email citext unique, password hash nullable, `apple_sub` unique nullable, display name, four nullable targets) | Task 3 |
| 3.1 `refresh_tokens` (user, token hash, family id, expires, revoked) | Task 3 |
| 2.2 JWT access tokens (15 min), rotating refresh tokens stored hashed, reuse revokes the family, argon2id | Tasks 4, 5 |
| 2.2 per-IP rate limits on `auth/*`, per-user elsewhere | Tasks 7, 9 |
| 4.2 stable `code`, validation by schema plus service rules, `404` for unseen resources | Tasks 8, 9 (field errors; the only per-user resource is the caller's own account) |
| 6 real Postgres, no DB mocks; table-driven unit tests | Tasks 2, 3, 5, 9 |
| 7 12-factor config, `.env.example`, no committed secrets, CI drift check | Tasks 6, 10, 3 (`check-generated` covers sqlc) |
| Foundation review inputs (auth seam, chi constraint, problem shape, client IP, migrated test DB, contract helper, hardening list, extra linters) | Tasks 1, 3, 7, 8, 9, 10, 11 |

Type and name consistency: `service.Optional`/`Set`/`UpdateInput` (Task 5) are used by `account.go` (Task 9); `httpapi.AuthService` (Task 9) is satisfied by `*service.Auth` (Task 5) and wired in `main.go` (Task 10); `auth.TokenIssuer` (Task 4) satisfies `httpapi.TokenParser` (Task 9); `config.Config.JWTSecret`/`TrustedProxies` (Task 6) are read by `main.go` (Task 10); `testutil.NewMigratedDatabase` (Task 2) is used by Tasks 3, 5, 9, 10; `api.*` names (Task 1) match their uses in Tasks 8 and 9.
