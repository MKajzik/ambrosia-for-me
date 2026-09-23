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
- `internal/usda/`: the FoodData Central client and its category/nutrient mapping tables, used only by `cmd/import-usda`. Talks to `store` directly, not through `internal/service`.
- `internal/store/`: **all SQL**. `queries/*.sql` is the source; `sqlc/` is generated from it and the migrations (never hand-edit). `Store` adds transactions.
- `internal/db/`: pgx pool and the goose migration runner.
- `internal/testutil/`: integration-test helpers (Postgres via testcontainers; a migrated template database copied per test).
- `migrations/`: embedded goose SQL migrations.

Dependencies point one way: `httpapi` → `service` → `store`; `auth` is a leaf used by `service` and `config`. A package never imports one to its left. `internal/usda` is a separate leaf-like package beside `httpapi`, not below `service`: it imports `service` directly (for the nutrient-key constants only) and `store` directly, so `service` must never import `usda`.

## Commands (from repo root)

- `make test-backend`: `go vet ./...` and `go test ./...`. **Needs Docker** (integration tests start Postgres with testcontainers). Without Docker they skip locally but fail when `CI` is set, so CI cannot pass silently without them.
- `make lint-backend`: runs golangci-lint v2.13.2 via `go run` (the same command CI uses), including `gofmt`, `gosec`, `bodyclose` and `sqlclosecheck`.
- `make generate`: regenerate `internal/api/api.gen.go` (oapi-codegen) and `internal/store/sqlc/` (sqlc)
- `make check-generated`: fail if the committed generated code differs from the spec, migrations and queries, or if a generated file is not committed
- `make migrate`: apply migrations to `DATABASE_URL` (defaults to the compose database)
- `make run-api`: run on `API_ADDR` from the process environment (default `:8080`; `.env` is not loaded). Needs `make db-up` and `make migrate` first. It defaults `JWT_SECRET` to the public development-only secret and sets `ALLOW_DEV_JWT_SECRET=1`, which the API requires before it accepts that secret; every other environment must set its own `JWT_SECRET`.

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
- Logout revokes refresh tokens only: an access token that was already issued keeps working until it expires (at most 15 minutes).
- Every table that hangs off a user must use `ON DELETE CASCADE`, or account deletion leaves orphans and breaks the in-app deletion guarantee (`refresh_tokens` does).
- **Cascades alone are not enough for account deletion.** `users` cascades to `ingredients`, `meals`, `diet_templates` and `plan_entries`, but `meal_ingredients.ingredient_id` and `template_slots`/`plan_entries.meal_id` are deliberately `NO ACTION` (what makes deleting an in-use ingredient or meal a `409`), and Postgres fires a table's own cascade trigger in an order this codebase does not control. `Auth.DeleteUser` deletes explicitly, in one transaction, in the only order that works: `plan_entries` → `diet_templates` (cascades `template_slots`) → `meals` (cascades `meal_ingredients`) → `users` (cascades `ingredients`, now safe since nothing in `meal_ingredients` still references them). Any future table that both cascades from `users` and is referenced with `NO ACTION` from another user-owned table needs the same treatment, in the right position in this chain.
- Rate limits (10 auth requests per minute per client IP, 300 requests per minute per user) are in memory: with several API replicas the effective limit is per replica. `TRUSTED_PROXY_COUNT` must match the real number of proxies, or clients can forge their IP.
- **Rate-limit gaps (accepted for now).** Requests the validator rejects (401 or 400) do not count toward the per-user limit; there is no per-account failed-login throttle, so credential stuffing spread across many IPs is limited only by argon2id cost; a shared NAT egress IP shares one 10-per-minute auth bucket; and unauthenticated traffic to non-`auth/*` routes and the health checks is unlimited at the application layer. Put an edge proxy or a global per-IP limit in front before exposing the API. Planned hardening: a per-account counter with backoff and a shared counter store.
- **Proxies.** `TRUSTED_PROXY_COUNT` must equal the real number of proxies in front of the API: too high lets clients forge their IP, too low makes everyone share the proxy's bucket. Bind the API only to the proxy network. A proxy that puts ports in `X-Forwarded-For` makes every client share one bucket.
- **Personal data in logs.** The request log records `remote_ip`. Decide and document a retention period before production.
- **Header and body edge cases.** A request with more than one `Authorization` header is rejected (401). A body over 64 KiB is `400 request body is too large` on unauthenticated routes; on secured routes it currently reads as `401`, because kin-openapi wraps the read error in a security error.
- **Ingredient search is not paginated.** `GET /ingredients?q=` ranks by trigram similarity and returns up to `limit` results with no cursor; only the plain alphabetical listing (no `q`) paginates. A type-ahead UI never needs a second page of search results, and cursoring a similarity-ranked result set has no stable order to cursor over.
- **Nutrients use two schemas on purpose.** `NutrientAmounts` (the `Ingredient` response) requires all 18 keys, always present, `null` where unknown. `NutrientAmountsInput` (`CreateIngredientRequest`/`UpdateIngredientRequest`) requires none, so a client can send just the nutrients it knows. Reusing `NutrientAmounts` for requests too was tried first and rejected: it made the request validator reject any partial nutrient object, defeating the whole point of optional input. On update, supplying the `nutrients` field at all replaces the ingredient's entire nutrient set (any of the 18 keys not included is cleared, not left alone); only omitting the `nutrients` field entirely leaves the existing nutrients unchanged.
- **Meal nutrition is per serving, and a missing nutrient propagates, not zeros out.** `Meal.NutritionPerServing` is the sum of every ingredient line's contribution divided by `servings`; multiply by `servings` client-side for the meal total. A nutrient key is `null` in the response if *any* ingredient in the meal has no value for it — treating an unknown amount as zero would understate the true total, which a meal-planning app cannot do quietly. A meal with zero ingredients reports `0` for every key (a well-defined empty sum, a different case from "unknown").
- **Clearing an ingredient's conversion factor while a meal depends on it is rejected at write time.** `Ingredients.Update` checks, before writing, whether clearing `grams_per_piece` or `density_g_per_ml` would strand a `meal_ingredients` row that uses this ingredient with the `piece` or `ml` unit (`IngredientHasUnconvertibleMealUsage`, scoped to the caller's own ingredients so it never leaks another user's meal usage). If so it fails with `409 unit_not_convertible` (`ErrIngredientInUseByUnconvertibleUnit`) and nothing is written — the meal, and any plan day or template that schedules it, stays exactly as it was. This mirrors deleting an in-use ingredient outright, which is blocked the same way (`409 ingredient_in_use`, via the `meal_ingredients_ingredient_id_fkey` foreign key). `ErrUnitNotConvertible` (the read-time check in `toMeal`/`gramsFor`, which `GET /plan` also runs through `Plan.GetRange` → `Meals.Get` for every scheduled meal in range) is now unreachable through normal sequential use — every write path that could produce an unconvertible row is validated first — and is kept only as a defensive fallback for a narrow, accepted race: `Ingredients.Update`'s check and a concurrent `Meals.ReplaceIngredients` read of the same ingredient run in separate transactions with no cross-table lock, so adversarial timing can still interleave them badly.
- **`plan_entries` and `template_slots` allow multiple `snack` rows per day; the other three slots don't.** `PUT/DELETE /plan/{date}/{slot}` can only address a slot by its enum value, so `snack` can't be a true upsert the way `breakfast`/`lunch`/`dinner` are: `PUT .../snack` always adds a new entry, and `DELETE .../snack` removes every snack entry for that date. A manual `PUT` (any slot) always clears `from_template_id` to `null`, even when it overwrites an entry that came from applying a template.
- **`diet_templates.day_count` is immutable after creation.** There is no PATCH field for it; recreate or `POST /diet-templates/{id}/copy` a template to change it. `PUT /diet-templates/{id}/slots` validates every slot's `day_index` against the existing `day_count` at write time — there is no database constraint for this (a `CHECK` can't compare against another table's column).
- **Deleting a meal fails with `409 meal_in_use` if a diet template's slot or a plan entry still references it**, the same `NO ACTION` foreign-key pattern as `ingredient_in_use`. `Auth.DeleteUser` deletes `plan_entries`, then `diet_templates` (which cascades `template_slots`), then `meals`, then the user row, in that order, in one transaction — see the note on cascades above.

## Decide before the domain plans

- ~~**Access tokens versus deleted or logged-out users.**~~ Resolved for ingredients: `Ingredients.Create` translates the `ingredients_owner_id_fkey` foreign-key violation into `service.ErrNotFound`, mapped to `401` exactly like `GetUser`. Every future user-owned write table should follow the same pattern (a named FK constraint plus a `store.IsForeignKeyViolation` check) rather than adopting the validator-level fix that was also considered.
- **`DELETE /me` needs no re-authentication.** A stolen 15-minute access token irreversibly deletes the account and, later, everything it owns. Changing this after the web and iOS clients exist is a breaking contract change: decide whether the request must carry the password or a fresh refresh token.
- **Registration reveals whether an email exists** (`409 email_taken`) while login is equalised. Accept it and record it in the privacy notes, or move to an always-`201` flow with email verification once email exists.
- **argon2id memory is not bounded by concurrency.** Each in-flight hash holds 64 MiB and the only brake is the per-IP limit, counted per replica. Add a bounded semaphore (about `GOMAXPROCS`) that answers 503 when full, before public exposure.

## Not built yet

- **Sign in with Apple** (`POST /v1/auth/apple`): its own plan, needs Apple Developer credentials (Service ID, keys) to test against.
- Email verification and password reset; deleting expired or revoked refresh tokens (a scheduled cleanup).
- The domain beyond diets and plan: shopping lists, partners (later plans). **Diet templates have no partner visibility yet**, for the same reason meals don't: the `partnerships` table does not exist until the partner plan (backend build order item 6, after shopping lists). `plan_entries` has no sharing concept in the spec at all — it is always owner-only.
- The contract now declares `500` for every auth operation, but still not `404` or `405`: the router returns problem+json for both, outside the contract, so contract tests cannot check them. `HEAD` and `OPTIONS` answer 405 on every route.

## Carried forward (hardening to schedule)

- `goose` session locker in `db.Migrate` before any multi-replica deploy.
- `recoverer` after a partial write appends a problem body to the partial response.
- `cmd/migrate` has no signal handling and reports errors as plain text.
- Later (shopping-list SSE plan): the 30s server `WriteTimeout` cuts event streams: override it per handler with `http.NewResponseController(w).SetWriteDeadline(time.Time{})` (chi's response wrapper supports `Unwrap`). `Server.Shutdown` does not cancel request contexts and a live stream never goes idle, so set `BaseContext` or `RegisterOnShutdown` so streams end on shutdown, otherwise every shutdown with a connected client burns the full 10s and exits 1.
