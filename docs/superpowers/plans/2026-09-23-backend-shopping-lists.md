# Backend Shopping Lists Domain Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add shopping lists: the `shopping_lists`/`shopping_items` schema, a CRUD API for lists and items with optimistic-concurrency `version`s on item edits, generation of a categorized list from a plan date range (reusing the meals domain's unit conversion), and a live `GET /shopping-lists/{id}/events` Server-Sent Events stream for the owner's own edits. It also closes the two SSE hardening items `backend/CLAUDE.md` carried forward for this plan (the server `WriteTimeout` cutting streams, and `Server.Shutdown` never ending them).

**Architecture:** Two new tables follow the existing `httpapi` → `service` → `store` layering. A new `ShoppingLists` service holds a `*store.Store` and reads `plan_entries`, `meals`, `meal_ingredients` and `ingredients` rows directly for generation. It reuses the package-level `gramsFor` from `meals.go` for unit conversion, which is a function call inside the same package, not a dependency on the `Meals` service. Live updates go through a new in-process `ListEventHub`: a mutex-guarded map from list id to subscriber channels. The service publishes to it after each successful commit, and the SSE handler subscribes to it for the life of the connection. The SSE route is an ordinary generated `api.ServerInterface` method, because this repo's handlers are non-strict `(w, r)`. So it goes through the same OpenAPI validator (authentication) and per-user rate limiter as every other route, with no special wiring. See Global Constraints for the deletion-guard divergence, the version semantics, the merge rule, the backpressure policy and the shutdown design this plan settles.

**Tech Stack:** Go 1.26, chi, pgx/pgxpool, goose, sqlc, oapi-codegen. No new dependencies. There is also no message broker: `go.mod` and `docker-compose.yml` (Postgres only) confirm the stack is one Go process plus Postgres.

**Spec:** `docs/superpowers/specs/2026-09-21-meal-planner-design.md` (sections 3.5, 3.6, 4.1, 4.2, 4.3, 6, 9)

## Global Constraints

- OpenAPI 3.0.3 is the source of truth: edit `openapi.yaml` first, then `make generate`, then implement. Never hand-edit `backend/internal/api/api.gen.go` or `backend/internal/store/sqlc/`.
- No SQL outside `backend/internal/store`. Dependencies point one way: `httpapi` → `service` → `store`.
- Global `security: bearerAuth` protects every operation by default, including the event stream.
- Errors are RFC 9457 `application/problem+json` with a stable `code`.
- Unseen resources return `404`, never `403`.
- Migrations are goose SQL files named `NNNNN_description.sql`. **Both new tables get `created_at`/`updated_at` plus the `set_updated_at()` trigger, including `shopping_items`.** `meal_ingredients` and `template_slots` skip timestamps because they are line items that their parent always replaces wholesale. `shopping_items` is different: its rows are edited in place, one at a time (`PATCH .../items/{item_id}`), so it is a first-class resource like `plan_entries`, which got timestamps for the same reason in the diets-and-plan plan.
- **For each resource group, the OpenAPI contract and its handlers ship in the same task (Tasks 7 and 8)**, following the diets-and-plan plan's precedent. `server.go`'s `var _ api.ServerInterface = (*server)(nil)` breaks as soon as `make generate` adds a method, so the contract change and the handler that implements it must land together to keep `go build` green.
- **Partner visibility is out of scope for this plan**, exactly as it was for meals and diet templates. The `partnerships` table still does not exist: `grep -rni partnership backend/` finds only comments, in `queries/meals.sql`, `queries/diet_templates.sql`, their generated `sqlc` counterparts, and `backend/CLAUDE.md`'s "Not built yet". `shopping_lists.shared_with_partner` is stored (the §3.5 data model is complete), but every read and write checks `owner_id` only. The event stream carries the owner's own edits. That is already useful across one user's devices, because a check made on the phone shows up live on the web. Partner edit access (§3.6 says lists are "editable by both") and partner-driven events are the next plan (§9, "partner and sharing with SSE").
- **`shopping_items.ingredient_id` is `ON DELETE SET NULL`, deliberately unlike the meals and diet-templates guards.** `meal_ingredients.ingredient_id` and `template_slots`/`plan_entries.meal_id` are `NO ACTION`. Services translate the violation into `409 ingredient_in_use` (`Ingredients.Delete`) or `409 meal_in_use` (`Meals.Delete`), because a meal or a plan is a durable thing that must not silently lose a part. A shopping list is a disposable, regenerable snapshot, and a stale item on it must not block deleting a custom ingredient. This only works because each `shopping_items` row stores its own `name`, `quantity`, `unit` and `category` (spec §3.5). The item is never rendered from the live ingredient, so when `ingredient_id` becomes `NULL` the item keeps displaying and behaving exactly like a free-text item. Its `origin` does not change, so a `generated` item whose ingredient was deleted is still replaced on the next regeneration, which is correct because the plan can no longer reference that ingredient. For the same reason, `Ingredients.Update`'s write-time guard (`IngredientHasUnconvertibleMealUsage`) needs no shopping-list counterpart. An item carries its own quantity and unit, so clearing an ingredient's `grams_per_piece` cannot strand it.
- **`shopping_items.checked_by` references `users` with `ON DELETE SET NULL`.** Today it is always the owner. Once partners can check items, deleting a partner's account must neither delete nor block the owner's list.
- **Account deletion: `Auth.DeleteUser` deletes the user's shopping lists first, but the position is not load-bearing.** `shopping_lists` cascades from `users` and cascades to `shopping_items`. Neither of `shopping_items`' references to other user-owned rows (`ingredient_id`, `checked_by`) is `NO ACTION`, so no cascade order can abort the delete, unlike the `meal_ingredients`/`template_slots`/`plan_entries` chain in `backend/CLAUDE.md`. This is scratch-verified against the Task 1 schema, not just asserted. A raw `DELETE FROM users` on an account that owns a shopping list succeeds even with no pre-delete at all, and that list has an item which references the account's own custom ingredient and is `checked_by` the account. Task 3 still adds the pre-delete as the first step of the chain, the same way the diets-and-plan plan added `plan_entries` first "to keep the intent explicit". Its regression test passes both before and after the change, and the test's job is to fail if a future `NO ACTION` reference into these tables ever changes that.
- **Item versions (spec §4.3), pinned exactly:**
  - Every item starts at `version = 1`, and every real change increments it by one. Every successful `PATCH` returns the full item with its new version, so a client can chain edits.
  - An **edit** (the request changes `name`, `quantity`, `unit` or `category`) must carry `version`. A missing `version` is `400 version_required`. A stale one is `409 version_conflict`, and the problem body carries the item's current state as the RFC 9457 extension member `current`, so the client can re-apply its change on top of it.
  - A **check-only** request (only `checked`, with or without `version`) is last-write-wins, as §4.3 says ("Checked state is last-write-wins"), and ignores `version`. Setting `checked` to the value it already has changes nothing: no version bump and no event. That is what makes `{checked: true}` idempotent, so an offline replay that carries a long-stale version neither conflicts nor churns.
  - `DELETE` of an item is unversioned, because "adds and removes merge". A second delete is `404`.
  - A mixed request (an edit plus `checked`) is an edit, so it is versioned as a whole.
- **How a stale write is told apart from a missing item.** A zero-row `UPDATE ... WHERE id = $1 AND version = $2` could mean either one. Instead, `UpdateItem` runs `GetShoppingItemForUserForUpdate` (`SELECT ... FOR UPDATE OF shopping_items`, joined to `shopping_lists` for the owner check) in the same transaction. No row means `404`. A row with the wrong version means `409` carrying that exact locked row. Otherwise it goes on to the unconditional `UpdateShoppingItem ... version = version + 1 RETURNING *`. The service needs the locked row anyway, both for the `409` body and to detect a no-op check. Re-reading only after a failed conditional `UPDATE` would add a second round trip and a window in which the row changes again.
- **Row locking, informed by the concurrent-replace bug this repo has already fixed twice** (`TouchMealForUser`, `TouchDietTemplateForUser`). The rule for each operation:
  - Item edits lock the one item row (`FOR UPDATE`), and the version check makes them safe per item. They never need the list lock, and no operation takes the item lock and then the list lock, so there is no lock-order cycle.
  - **Regenerating** a list (`Generate` with `list_id`) is this domain's "replace the whole child collection" operation, and it has exactly the bug its siblings had. Say two transactions both run `DELETE ... WHERE origin = 'generated'` and then `INSERT`. Under READ COMMITTED the second `DELETE` cannot see the rows the first one inserts, so both generated sets survive. `Generate` therefore opens with `SetShoppingListSourceForUser`. It is an `UPDATE` that records the new source range and, in the same statement, takes the list row's write lock.
  - `AddItem` takes the same list lock (`TouchShoppingListForUser`), so two concurrent adds, or an add racing a regeneration, cannot both read the same `NextShoppingItemPosition`.
  - `PATCH /shopping-lists/{id}` is a single `UPDATE` and needs nothing extra.
- **Generation (spec §3.5), pinned exactly:**
  - The service reads every `plan_entries` row in `[from, to]` inclusive. `from`/`to` reuse `GET /plan`'s 92-day cap and its `ErrPlanRangeTooLong` / `ErrPlanRangeInvalid` errors and codes.
  - Each meal ingredient line contributes `quantity × portion / servings`. `portion` counts servings, the same rule `Plan.GetRange` uses to scale a meal's nutrition.
  - Lines are summed per ingredient and per unit, then merged by one rule (`mergeUnits`):
    - If every line for an ingredient used the same unit, that unit is kept. Six eggs stay "6 piece", not "300 g".
    - If the units are mixed, every per-unit total that converts to grams is merged into one `g` line. The conversion is `gramsFor(quantity, unit, grams_per_piece, density_g_per_ml)`, the exact function meal nutrition uses, reused rather than reimplemented. A total that cannot convert (the ingredient lacks the factor) keeps its own line in its own unit instead of being merged incorrectly.

    The spec says "convert to grams (or keep ml/piece where units cannot merge)". Always converting to grams would satisfy that literally, but it would turn every countable item into an unshoppable gram weight. The same-unit rule keeps the convertible cases exact and the list readable.
  - Each item's `name` and `category` come from the ingredient. The category reuses the existing 10-value `ingredients.category` vocabulary (`00004_ingredients.sql` and the `IngredientCategory` OpenAPI enum); no new enum is introduced.
  - Items are ordered by category, then name, then unit, and get positions after every item the list keeps.
  - Regenerating deletes every `origin = 'generated'` item and inserts the fresh set, in one transaction, and never touches `origin = 'manual'` items. That includes their checked state, while a generated item that was checked comes back unchecked, because it is a new row.
  - A range with no plan entries produces a list with no generated items, not an error.
  - Quantities are stored unrounded. Formatting is a client concern, and rounding could push a tiny quantity to `0`, which the `quantity > 0` CHECK rejects.
- **Regeneration is `POST /shopping-lists/generate` with an optional `list_id`.** §4.1 lists only `generate ({from, to})`, and §3.5 says "regenerating a list replaces generated items", but no endpoint addresses an existing list. Rather than invent an endpoint the spec does not list, `generate` takes an optional `list_id`. Without it, a new list is created (`201`), named by the optional `name` or "Shopping {from} to {to}". With it, that list is regenerated in place (`200`), its `source_from`/`source_to` are updated, and `name` is ignored.
- **`GET /shopping-lists` is cursor-paginated newest first** (`ORDER BY created_at DESC, id DESC`), not alphabetically like meals and diet templates. A user's lists accumulate week after week, so §4.2's "lists that can grow" applies. A shopping screen wants the most recent list first; an alphabetical order has no use here. The cursor is `{created_at, id}`, base64url JSON like the other cursors, and the query uses a row comparison `(created_at, id) < (cursor_created_at, cursor_id)`.
- **Event hub (`ListEventHub`, Task 4), pinned exactly:**
  - It is an in-process map from list id to a set of subscriber channels, guarded by one mutex. Every send and every close happens under that mutex, so a send can never hit a closed channel.
  - The service publishes only after the transaction commits, so a rolled-back write never emits an event.
  - Each subscriber gets a buffer of 32 events. `Publish` never blocks. **A subscriber whose buffer is full is disconnected (its channel is closed), not fed a dropped event.** Here is the concrete reason drop-oldest/drop-newest is wrong for this mechanism. §4.3's recovery is "refetch when they detect a version gap", but versions are per item. A client can only detect a dropped event for item A if a later event for item A arrives, and events for items B, C and D never reveal it. So a dropped event could leave a client stale indefinitely. Closing the stream instead triggers the other §4.3 recovery, "refetch on reconnect", which always converges. A client that falls 32 events behind has a broken connection anyway.
  - `list_deleted` is terminal. It is queued, and then every subscriber of that list is disconnected. A closed channel still yields the values buffered before the close, so the handler writes the event and then returns.
  - `ShoppingLists.Subscribe` registers the subscription **before** it checks ownership. That way a delete committing between the two can never be missed: either the check sees the list gone (`404`), or the subscription already exists when `list_deleted` is published.
  - The hub lives in memory, so a second API replica would not see this replica's events. Clients still converge (they refetch on reconnect and on foreground, per §4.3), and this is recorded in `backend/CLAUDE.md` next to the in-memory rate limits.
- **Event payload (spec §4.3: "list id, item id and new version"):**
  - Each event is `event: <type>` followed by `data: {"type", "list_id", "item_id"?, "version"?}`.
  - The types are `item_changed` (added, edited or checked; `version` is the new version), `item_deleted` (`version` is the version the item had when it was removed), `list_changed` (renamed or regenerated; clients refetch) and `list_deleted`.
  - A `: connected` comment is flushed first, so headers go out immediately, and a `: keep-alive` comment is sent every 25 seconds.
  - The payload is **not** an OpenAPI component. A component that nothing references fails redocly's `no-unused-components` rule (scratch-verified: 1 warning where the repo currently has none), and a `text/event-stream` body cannot `$ref` a JSON schema. The shape is written out in the operation's description instead, and the Go type is a handler-local `listEventData`.
- **The SSE route goes through the generated `api.ServerInterface`, not around it.** `backend/internal/api/oapi.yaml` generates a `chi-server` with non-strict handlers, so every method already takes `http.ResponseWriter` directly. oapi-codegen v2.8.0 was scratch-verified to generate `StreamShoppingListEvents(w http.ResponseWriter, r *http.Request, id openapi_types.UUID)` for a `200` whose only content type is `text/event-stream`. The route is therefore wrapped by `openAPIValidator`, which authenticates it from the spec's global `bearerAuth` exactly like every other route, and by the per-user limiter, which counts each (re)connect once.
  - **The access token is accepted only in the `Authorization` header, never in the query string.** `backend/CLAUDE.md` forbids secrets in query strings and logs. So browsers cannot use the native `EventSource`, which cannot set headers. The web client needs a fetch-based SSE reader. This constraint is written into the operation's description for the web plan to pick up.
- **The write deadline is cleared per stream, not globally.** `http.Server.WriteTimeout` (30s in `cmd/api`) is an absolute deadline for the whole response. The SSE handler calls `http.NewResponseController(w).SetWriteDeadline(time.Time{})` for its own response only.
  - Scratch-verified: chi's `middleware.WrapResponseWriter` (the request logger's wrapper) implements `Unwrap` and `Flush`, so the controller reaches the real connection. A stream with the override outlived a 1.5s `WriteTimeout`, and one without it died at 1.5s.
  - `ReadTimeout` was checked the same way and does **not** cut the stream on Go 1.26: a stream with only the write deadline cleared outlived a 1s `ReadTimeout`. So only the write deadline needs overriding.
  - `httptest.ResponseRecorder` has no deadlines. The handler treats `http.ErrNotSupported` as harmless, so contract tests still work.
- **Shutdown ends streams with `srv.RegisterOnShutdown(listEvents.Close)`, not with a cancellable `BaseContext`.** `Server.Shutdown` never cancels request contexts, and a live stream never goes idle, so without a hook every shutdown with a connected client waits out the full 10s `shutdownTimeout` and returns an error (scratch-verified: the Task 9 test fails that way without the hook). Cancelling `BaseContext` would also end streams, but it would cancel every in-flight ordinary request's context too, aborting their database work mid-request and defeating the graceful drain that `Shutdown` exists for. Closing the hub ends only the streams: their channels close, their handlers return, their connections go idle, and `Shutdown` completes. A `Subscribe` that races shutdown gets `ErrEventStreamsClosed`, which maps to `503 not_ready`.
- **The contract tests need a body decoder for `text/event-stream`.** kin-openapi v0.149.0 registers none (it has `text/plain`, `text/csv` and JSON variants), so validating the stream's `200` response would fail as an unsupported content type. `contract_test.go` registers `openapi3filter.PlainBodyDecoder` for it in an `init()`, test-only. The stream's schema is `type: string`, which is exactly what that decoder validates.
- Nutrient data is not touched by this plan. Shopping items carry quantities, never nutrition.
- Any new environment variable is added to `.env.example` in the same commit. None are expected in this plan.

**The whole plan is scratch-verified end to end, not only the query types.** During planning, every file below was assembled in a throwaway copy of the repo in exactly its post-Task-9 state:

- `sqlc generate` (v1.31.1) and `oapi-codegen` (v2.8.0) produced exactly the signatures and types each task's Interfaces block quotes, and `redocly lint` reported no warnings.
- `go build ./...`, `go vet` and `golangci-lint` v2.13.2 (the repo's CI command, including `gosec`, `bodyclose` and `sqlclosecheck`) reported 0 issues.
- `CI=1 go test ./...` passed every new test and every pre-existing one, against real Postgres via testcontainers.
- The two SSE fixes were each shown to be load-bearing: removing the write-deadline override makes Task 8's `TestShoppingListEventsOutliveTheServerWriteTimeout` fail ("stream ended early"), and removing the shutdown hook makes Task 9's `TestServeEndsOpenEventStreamsOnShutdown` fail ("serve did not return within 5s").

Only the split into tasks was done after that verification.

---

## File Structure

- `backend/migrations/00007_shopping_lists.sql`: the two tables. Default constraint names that tasks rely on: `shopping_lists_owner_id_fkey` (Task 5 translates it into `ErrNotFound`/401), `shopping_items_list_id_fkey` (`ON DELETE CASCADE`), `shopping_items_ingredient_id_fkey` (`ON DELETE SET NULL`; Task 5 translates a racing violation), and `shopping_items_checked_by_fkey` (`ON DELETE SET NULL`).
- `backend/internal/db/schema_test.go`: gains `TestShoppingListsSchemaEnforcesItsConstraints`.
- `backend/internal/store/queries/shopping_lists.sql`: sqlc source for both tables.
- `backend/internal/store/queries/meals.sql`: gains `GetMealIngredientsForMeals` (the batch lookup generation needs).
- `backend/internal/service/auth.go` / `auth_test.go`: `DeleteUser` gains the shopping-list pre-delete and a regression test.
- `backend/internal/service/shopping_events.go` / `shopping_events_test.go`: `ListEventHub`, `ListSubscription`, `ListEvent`, and pure unit tests (no database).
- `backend/internal/service/shopping_lists.go` / `shopping_lists_test.go`: the `ShoppingLists` service and its tests, including the §6 shopping-list merging test.
- `openapi.yaml`: the `ShoppingLists` tag, eight paths and their schemas.
- `backend/internal/httpapi/shopping_lists.go`: the ten handlers, including the SSE stream, the cursor codec, the version-conflict writer, and the `ShoppingListsService` interface.
- `backend/internal/httpapi/problem.go`, `account.go`: new codes and `writeServiceError` cases.
- `backend/internal/httpapi/server.go`, `router.go`: wire `ShoppingLists` into `server` and `Deps`.
- `backend/internal/httpapi/contract_test.go`: `stubShoppingLists`, the `text/event-stream` decoder, and a new case in the router's required-dependency test.
- `backend/internal/httpapi/shopping_lists_flow_test.go`: end-to-end contract tests plus a real-server `WriteTimeout` test.
- `backend/cmd/api/main.go` / `main_test.go`: construct the hub and service, register the shutdown hook, and add a shutdown-with-an-open-stream test.
- `backend/CLAUDE.md`: documents the domain, updates the cascade note and "Not built yet", and removes the SSE item from "Carried forward".

---

### Task 1: Migration: `shopping_lists` and `shopping_items`

**Files:**
- Create: `backend/migrations/00007_shopping_lists.sql`
- Modify: `backend/internal/db/schema_test.go`

**Interfaces:**
- Produces: the `shopping_lists` table (`id, owner_id, name, shared_with_partner, source_from, source_to, created_at, updated_at`) and the `shopping_items` table (`id, list_id, ingredient_id, name, quantity, unit, category, checked, checked_by, position, version, origin, created_at, updated_at`). The default constraint names are listed under File Structure.

- [x] **Step 1: Write the failing test**

Add to the end of `backend/internal/db/schema_test.go` (it uses the file's existing `migratedConn` helper and needs no new imports):

```go
func TestShoppingListsSchemaEnforcesItsConstraints(t *testing.T) {
	ctx := context.Background()
	conn := migratedConn(t)

	var userID, ingredientID string
	if err := conn.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, display_name) VALUES ('a@example.com', 'h', 'A') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err := conn.QueryRow(ctx,
		`INSERT INTO ingredients (name, category, owner_id) VALUES ('Tofu', 'legumes_nuts_seeds', $1) RETURNING id`, userID,
	).Scan(&ingredientID); err != nil {
		t.Fatalf("insert ingredient: %v", err)
	}

	var listID string
	if err := conn.QueryRow(ctx,
		`INSERT INTO shopping_lists (owner_id, name, source_from, source_to) VALUES ($1, 'Week', '2026-06-01', '2026-06-07') RETURNING id`, userID,
	).Scan(&listID); err != nil {
		t.Fatalf("valid list: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO shopping_lists (owner_id, name) VALUES (gen_random_uuid(), 'Ghost')`); err == nil {
		t.Error("an owner_id that does not reference a user was accepted, want a foreign key violation")
	}
	if _, err := conn.Exec(ctx, `INSERT INTO shopping_lists (owner_id, name, source_from) VALUES ($1, 'Half', '2026-06-01')`, userID); err == nil {
		t.Error("source_from without source_to was accepted, want a constraint violation")
	}
	if _, err := conn.Exec(ctx, `INSERT INTO shopping_lists (owner_id, name, source_from, source_to) VALUES ($1, 'Backwards', '2026-06-07', '2026-06-01')`, userID); err == nil {
		t.Error("source_to before source_from was accepted, want a constraint violation")
	}

	insertItem := func(values string, args ...any) error {
		_, err := conn.Exec(ctx, "INSERT INTO shopping_items (list_id, ingredient_id, name, quantity, unit, category, position, origin) VALUES "+values, args...)
		return err
	}
	if err := insertItem(`($1, $2, 'Tofu', 400, 'g', 'legumes_nuts_seeds', 0, 'generated')`, listID, ingredientID); err != nil {
		t.Fatalf("valid generated item: %v", err)
	}
	if err := insertItem(`($1, NULL, 'Paper towels', NULL, NULL, 'other', 1, 'manual')`, listID); err != nil {
		t.Fatalf("valid free-text item: %v", err)
	}
	for name, values := range map[string]string{
		"zero quantity":     `($1, NULL, 'X', 0, 'g', 'other', 2, 'manual')`,
		"invalid unit":      `($1, NULL, 'X', 1, 'cup', 'other', 2, 'manual')`,
		"invalid category":  `($1, NULL, 'X', 1, 'g', 'aisle_9', 2, 'manual')`,
		"negative position": `($1, NULL, 'X', 1, 'g', 'other', -1, 'manual')`,
		"invalid origin":    `($1, NULL, 'X', 1, 'g', 'other', 2, 'imported')`,
	} {
		if err := insertItem(values, listID); err == nil {
			t.Errorf("%s was accepted, want a constraint violation", name)
		}
	}

	var version int
	if err := conn.QueryRow(ctx, `SELECT version FROM shopping_items WHERE list_id = $1 AND position = 0`, listID).Scan(&version); err != nil || version != 1 {
		t.Errorf("a new item's version = %d (err %v), want 1", version, err)
	}

	// Deleting an ingredient a shopping item references is allowed and only
	// forgets the link (unlike meal_ingredients, which blocks it).
	if _, err := conn.Exec(ctx, `DELETE FROM ingredients WHERE id = $1`, ingredientID); err != nil {
		t.Fatalf("delete an ingredient a shopping item references: %v", err)
	}
	var linked *string
	var name string
	if err := conn.QueryRow(ctx, `SELECT ingredient_id, name FROM shopping_items WHERE list_id = $1 AND position = 0`, listID).Scan(&linked, &name); err != nil {
		t.Fatalf("read item after ingredient delete: %v", err)
	}
	if linked != nil || name != "Tofu" {
		t.Errorf("item after ingredient delete = (%v, %q), want (NULL, Tofu): ON DELETE SET NULL, item kept", linked, name)
	}

	if _, err := conn.Exec(ctx, `DELETE FROM shopping_lists WHERE id = $1`, listID); err != nil {
		t.Fatalf("delete list: %v", err)
	}
	var n int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM shopping_items WHERE list_id = $1`, listID).Scan(&n); err != nil || n != 0 {
		t.Errorf("shopping_items rows after deleting the list = %d (err %v), want 0", n, err)
	}
}
```

- [x] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/db/... -run TestShoppingListsSchemaEnforcesItsConstraints -v`
Expected: FAIL, `relation "shopping_lists" does not exist`. This needs Docker. Without it the test skips locally but fails under `CI=1`.

- [x] **Step 3: Write the migration**

Create `backend/migrations/00007_shopping_lists.sql`:

```sql
-- +goose Up
CREATE TABLE shopping_lists (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id            uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name                text NOT NULL,
    shared_with_partner boolean NOT NULL DEFAULT false,
    source_from         date,
    source_to           date,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    CHECK ((source_from IS NULL) = (source_to IS NULL)),
    CHECK (source_to >= source_from)
);

CREATE INDEX shopping_lists_owner_id_created_at_idx ON shopping_lists (owner_id, created_at DESC, id DESC);

CREATE TRIGGER shopping_lists_set_updated_at
    BEFORE UPDATE ON shopping_lists
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE shopping_items (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    list_id       uuid NOT NULL REFERENCES shopping_lists (id) ON DELETE CASCADE,
    ingredient_id uuid REFERENCES ingredients (id) ON DELETE SET NULL,
    name          text NOT NULL,
    quantity      double precision CHECK (quantity > 0),
    unit          text CHECK (unit IN ('g', 'ml', 'piece')),
    category      text NOT NULL CHECK (category IN (
        'produce', 'dairy_eggs', 'meat_seafood', 'grains_bread', 'legumes_nuts_seeds',
        'condiments_oils', 'spices_herbs', 'beverages', 'sweets_snacks', 'other'
    )),
    checked       boolean NOT NULL DEFAULT false,
    checked_by    uuid REFERENCES users (id) ON DELETE SET NULL,
    position      integer NOT NULL CHECK (position >= 0),
    version       integer NOT NULL DEFAULT 1 CHECK (version >= 1),
    origin        text NOT NULL CHECK (origin IN ('generated', 'manual')),
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX shopping_items_list_id_idx ON shopping_items (list_id);
CREATE INDEX shopping_items_ingredient_id_idx ON shopping_items (ingredient_id);
CREATE INDEX shopping_items_checked_by_idx ON shopping_items (checked_by);

CREATE TRIGGER shopping_items_set_updated_at
    BEFORE UPDATE ON shopping_items
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE shopping_items;
DROP TABLE shopping_lists;
```

Notes on choices already made (do not redesign these):
- `ingredient_id` is `ON DELETE SET NULL`, not `NO ACTION`, and `checked_by` is too. Global Constraints explains why. The `ingredient_id` and `checked_by` indexes exist so those `SET NULL` actions (fired by deleting an ingredient or a user) find referencing rows without a sequential scan.
- `quantity` and `unit` are nullable because a free-text item ("paper towels") may have neither. There is deliberately no cross-column CHECK such as "a unit needs a quantity". `PATCH` can clear either field on its own, and a CHECK would turn a legitimate clear into a raw `23514` that nothing translates.
- `category` repeats `ingredients.category`'s exact CHECK list (`00004_ingredients.sql`), because the item's category is its own copy.
- There is no `UNIQUE (list_id, position)`. Positions are assigned under the list row lock (see Global Constraints), and deleting items leaves gaps anyway. The order is `ORDER BY position, id`.
- The `shopping_lists` index matches the newest-first cursor query (`owner_id`, then `created_at DESC, id DESC`).

- [x] **Step 4: Run the test to verify it passes**

Run: `cd backend && go test ./internal/db/... -run TestShoppingListsSchemaEnforcesItsConstraints -v`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add backend/migrations/00007_shopping_lists.sql backend/internal/db/schema_test.go
git commit -m "feat(backend): add the shopping_lists and shopping_items tables"
```

---

### Task 2: sqlc queries for shopping lists

**Files:**
- Create: `backend/internal/store/queries/shopping_lists.sql`
- Modify: `backend/internal/store/queries/meals.sql` (add `GetMealIngredientsForMeals`)
- Modify (generated, commit the output): `backend/internal/store/sqlc/shopping_lists.sql.go`, `backend/internal/store/sqlc/meals.sql.go`, `backend/internal/store/sqlc/models.go`

**Interfaces:**
- Consumes: the schema from Task 1.
- Produces (used by Tasks 3, 5 and 6):
  - `sqlc.ShoppingList{ID, OwnerID uuid.UUID; Name string; SharedWithPartner bool; SourceFrom, SourceTo pgtype.Date; CreatedAt, UpdatedAt time.Time}`
  - `sqlc.ShoppingItem{ID, ListID uuid.UUID; IngredientID *uuid.UUID; Name string; Quantity *float64; Unit *string; Category string; Checked bool; CheckedBy *uuid.UUID; Position, Version int32; Origin string; CreatedAt, UpdatedAt time.Time}`
  - These `*sqlc.Queries` methods:
    - `CreateShoppingList(ctx, CreateShoppingListParams{OwnerID uuid.UUID; Name string; SharedWithPartner bool; SourceFrom, SourceTo pgtype.Date}) (ShoppingList, error)`
    - `GetShoppingListForUser(ctx, GetShoppingListForUserParams{ID, UserID uuid.UUID}) (ShoppingList, error)`
    - `TouchShoppingListForUser(ctx, TouchShoppingListForUserParams{ID, UserID uuid.UUID}) (ShoppingList, error)`
    - `ListShoppingListsForUser(ctx, ListShoppingListsForUserParams{UserID uuid.UUID; HasCursor bool; CursorCreatedAt time.Time; CursorID uuid.UUID; RowLimit int32}) ([]ShoppingList, error)`
    - `UpdateShoppingList(ctx, UpdateShoppingListParams{Name *string; SharedWithPartner *bool; ID, UserID uuid.UUID}) (ShoppingList, error)`
    - `SetShoppingListSourceForUser(ctx, SetShoppingListSourceForUserParams{SourceFrom, SourceTo pgtype.Date; ID, UserID uuid.UUID}) (ShoppingList, error)`
    - `DeleteShoppingList(ctx, DeleteShoppingListParams{ID, UserID uuid.UUID}) (int64, error)`
    - `DeleteShoppingListsForUser(ctx, userID uuid.UUID) error`
    - `GetShoppingItems(ctx, listID uuid.UUID) ([]ShoppingItem, error)`
    - `GetShoppingItemForUserForUpdate(ctx, GetShoppingItemForUserForUpdateParams{ID, ListID, UserID uuid.UUID}) (ShoppingItem, error)`. sqlc reuses the `ShoppingItem` model for `SELECT shopping_items.*` from the join, so no separate `...Row` type is generated.
    - `NextShoppingItemPosition(ctx, listID uuid.UUID) (int32, error)`
    - `InsertShoppingItem(ctx, InsertShoppingItemParams{ListID uuid.UUID; IngredientID *uuid.UUID; Name string; Quantity *float64; Unit *string; Category string; Position int32; Origin string}) (ShoppingItem, error)`
    - `UpdateShoppingItem(ctx, UpdateShoppingItemParams{Name *string; SetQuantity bool; Quantity *float64; SetUnit bool; Unit *string; Category *string; Checked *bool; CheckedBy *uuid.UUID; ID uuid.UUID}) (ShoppingItem, error)`
    - `DeleteShoppingItemForUser(ctx, DeleteShoppingItemForUserParams{ID, ListID, UserID uuid.UUID}) (DeleteShoppingItemForUserRow, error)` with `DeleteShoppingItemForUserRow{ID uuid.UUID; Version int32}`
    - `DeleteGeneratedShoppingItems(ctx, listID uuid.UUID) error`
    - `GetMealIngredientsForMeals(ctx, mealIds []uuid.UUID) ([]MealIngredient, error)`

  The nullable `date` columns come out as `pgtype.Date`, not `*time.Time`: the repo's `sqlc.yaml` overrides only `uuid` and `timestamptz`, the same reason `plan_entries.date` is `pgtype.Date`, which `util.go`'s `toPgDate`/`fromPgDate` already bridge. All of the above is scratch-verified. A real `sqlc generate` (v1.31.1, the repo's `sqlc.yaml`, these exact query texts) produced exactly these signatures with no errors. Beyond compiling, the queries' runtime behaviour was exercised against a migrated Postgres:
  - `UpdateShoppingItem` bumps `version` on every call. On a check it sets `checked_by`, on an uncheck it clears it, and on a mixed edit that re-sends `checked: true` it keeps the existing `checked_by`.
  - `GetShoppingItemForUserForUpdate` returns no rows for a different user.
  - `NextShoppingItemPosition` returns `max + 1`.
  - Deleting a referenced ingredient `NULL`s `ingredient_id` and keeps the item.

- [x] **Step 1: Write the shopping-list queries**

Create `backend/internal/store/queries/shopping_lists.sql`:

```sql
-- name: CreateShoppingList :one
INSERT INTO shopping_lists (owner_id, name, shared_with_partner, source_from, source_to)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetShoppingListForUser :one
-- Owner-only visibility for now: shared_with_partner has no effect until the
-- partner plan adds the partnerships table and an active-partner lookup. See
-- "Not built yet" in backend/CLAUDE.md.
SELECT * FROM shopping_lists
WHERE id = sqlc.arg('id') AND owner_id = sqlc.arg('user_id');

-- name: TouchShoppingListForUser :one
-- Bumps updated_at (via the shopping_lists_set_updated_at trigger) and, just
-- as importantly, takes the list row's write lock: AddItem uses this instead
-- of a plain SELECT so two concurrent adds cannot read the same
-- NextShoppingItemPosition. See TouchMealForUser in meals.sql for the same
-- pattern in the meals domain.
UPDATE shopping_lists SET updated_at = now()
WHERE id = sqlc.arg('id') AND owner_id = sqlc.arg('user_id')
RETURNING *;

-- name: ListShoppingListsForUser :many
-- Newest first. The row comparison is the keyset cursor over
-- (created_at DESC, id DESC).
SELECT * FROM shopping_lists
WHERE owner_id = sqlc.arg('user_id')
  AND (
    NOT sqlc.arg('has_cursor')::boolean
    OR (created_at, id) < (sqlc.arg('cursor_created_at')::timestamptz, sqlc.arg('cursor_id')::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('row_limit');

-- name: UpdateShoppingList :one
UPDATE shopping_lists SET
    name                = COALESCE(sqlc.narg('name'), name),
    shared_with_partner = COALESCE(sqlc.narg('shared_with_partner'), shared_with_partner)
WHERE id = sqlc.arg('id') AND owner_id = sqlc.arg('user_id')
RETURNING *;

-- name: SetShoppingListSourceForUser :one
-- Used by ShoppingLists.Generate when regenerating an existing list. Being an
-- UPDATE, it also takes the list row's write lock before the DELETE+INSERT
-- of generated items that follows it, so two concurrent regenerations of one
-- list serialize (see Generate in internal/service/shopping_lists.go).
UPDATE shopping_lists SET source_from = sqlc.arg('source_from'), source_to = sqlc.arg('source_to')
WHERE id = sqlc.arg('id') AND owner_id = sqlc.arg('user_id')
RETURNING *;

-- name: DeleteShoppingList :execrows
DELETE FROM shopping_lists WHERE id = sqlc.arg('id') AND owner_id = sqlc.arg('user_id');

-- name: DeleteShoppingListsForUser :exec
-- Used by Auth.DeleteUser, first in its chain of explicit pre-deletes. Not
-- load-bearing today (shopping_items' references to other user-owned rows are
-- ON DELETE SET NULL, not NO ACTION); see DeleteUser's doc comment.
DELETE FROM shopping_lists WHERE owner_id = sqlc.arg('user_id');

-- name: GetShoppingItems :many
SELECT * FROM shopping_items WHERE list_id = sqlc.arg('list_id') ORDER BY position, id;

-- name: GetShoppingItemForUserForUpdate :one
-- Locks the one item row for UpdateItem's version check and the UPDATE after
-- it. The join is the ownership check; only the item row is locked.
SELECT shopping_items.* FROM shopping_items
JOIN shopping_lists ON shopping_lists.id = shopping_items.list_id
WHERE shopping_items.id = sqlc.arg('id')
  AND shopping_items.list_id = sqlc.arg('list_id')
  AND shopping_lists.owner_id = sqlc.arg('user_id')
FOR UPDATE OF shopping_items;

-- name: NextShoppingItemPosition :one
SELECT COALESCE(MAX(position) + 1, 0)::integer AS next_position
FROM shopping_items WHERE list_id = sqlc.arg('list_id');

-- name: InsertShoppingItem :one
INSERT INTO shopping_items (list_id, ingredient_id, name, quantity, unit, category, position, origin)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: UpdateShoppingItem :one
-- Unconditional on version: ShoppingLists.UpdateItem has already locked the
-- row (GetShoppingItemForUserForUpdate) and checked the version in the same
-- transaction. checked_by follows checked, and is left alone when checked is
-- absent or unchanged (the right-hand "checked" is the row's old value).
UPDATE shopping_items SET
    name       = COALESCE(sqlc.narg('name'), name),
    quantity   = CASE WHEN sqlc.arg('set_quantity')::boolean THEN sqlc.narg('quantity') ELSE quantity END,
    unit       = CASE WHEN sqlc.arg('set_unit')::boolean THEN sqlc.narg('unit') ELSE unit END,
    category   = COALESCE(sqlc.narg('category'), category),
    checked_by = CASE
        WHEN sqlc.narg('checked')::boolean IS NULL OR sqlc.narg('checked')::boolean = checked THEN checked_by
        WHEN sqlc.narg('checked')::boolean THEN sqlc.narg('checked_by')::uuid
        ELSE NULL
    END,
    checked    = COALESCE(sqlc.narg('checked')::boolean, checked),
    version    = version + 1
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteShoppingItemForUser :one
-- :one, not :execrows: the deleted item's last version goes into the
-- item_deleted event.
DELETE FROM shopping_items
USING shopping_lists
WHERE shopping_items.id = sqlc.arg('id')
  AND shopping_items.list_id = sqlc.arg('list_id')
  AND shopping_lists.id = shopping_items.list_id
  AND shopping_lists.owner_id = sqlc.arg('user_id')
RETURNING shopping_items.id, shopping_items.version;

-- name: DeleteGeneratedShoppingItems :exec
DELETE FROM shopping_items WHERE list_id = sqlc.arg('list_id') AND origin = 'generated';
```

Notes on choices already made (do not redesign these):
- `UpdateShoppingItem` uses the same two patterns `UpdateMeal` uses. `name` and `category` are `NOT NULL`, so a plain `COALESCE(narg, column)` is enough. `quantity` and `unit` can legitimately be cleared to `NULL`, so they need the `set_*` boolean + `CASE` pattern.
- `UpdateShoppingItem`'s `WHERE` has no owner check because the locking `SELECT` before it already did that, in the same transaction.

- [x] **Step 2: Add `GetMealIngredientsForMeals` to the meals queries**

In `backend/internal/store/queries/meals.sql`, after `GetMealsForUser`, add:

```sql
-- name: GetMealIngredientsForMeals :many
-- The batch counterpart to GetMealIngredients, for ShoppingLists.Generate:
-- every ingredient line of every meal scheduled in a date range, in one query.
SELECT * FROM meal_ingredients
WHERE meal_id = ANY(sqlc.arg('meal_ids')::uuid[])
ORDER BY meal_id, position;
```

- [x] **Step 3: Regenerate and verify it compiles**

Run: `cd backend && go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate`
Expected: exits 0, creates `internal/store/sqlc/shopping_lists.sql.go`, updates `meals.sql.go`, and adds `ShoppingList`/`ShoppingItem` to `models.go`.

Run: `cd backend && go build ./...`
Expected: builds cleanly.

- [x] **Step 4: Commit**

```bash
git add backend/internal/store/queries/shopping_lists.sql backend/internal/store/queries/meals.sql backend/internal/store/sqlc/
git commit -m "feat(backend): add sqlc queries for shopping lists"
```

---

### Task 3: Account deletion: shopping lists first in the explicit chain

**Files:**
- Modify: `backend/internal/service/auth.go`
- Modify: `backend/internal/service/auth_test.go`

**Interfaces:**
- Consumes: `DeleteShoppingListsForUser`, `CreateShoppingList`, `InsertShoppingItem`, `UpdateShoppingItem`, `GetShoppingListForUser` (Task 2); the existing `newFixture`/`register` helpers in `auth_test.go`.
- Produces: nothing new. `Auth.DeleteUser` keeps its signature.

- [x] **Step 1: Write the regression test**

Add to `backend/internal/service/auth_test.go`, after `TestDeleteUserWithAPlanEntryAndTemplateUsingTheirOwnMeal`. The file already imports `context`, `errors`, `service`, `store` and `sqlc`:

```go
// TestDeleteUserWithAShoppingListThatUsesTheirOwnCustomIngredient pins the
// reasoning in DeleteUser's doc comment: shopping_items references the
// caller's own ingredient and user row with ON DELETE SET NULL, so no cascade
// order can abort the delete. It fails if a future NO ACTION reference into
// shopping_lists or shopping_items changes that.
func TestDeleteUserWithAShoppingListThatUsesTheirOwnCustomIngredient(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := register(t, f, "deleteme@example.com")

	tofu, err := service.NewIngredients(f.store).Create(ctx, s.User.ID, service.CreateIngredientInput{Name: "Tofu", Category: "legumes_nuts_seeds"})
	if err != nil {
		t.Fatalf("create ingredient: %v", err)
	}
	list, err := f.store.CreateShoppingList(ctx, sqlc.CreateShoppingListParams{OwnerID: s.User.ID, Name: "Groceries"})
	if err != nil {
		t.Fatalf("create list: %v", err)
	}
	quantity, unit := 400.0, "g"
	item, err := f.store.InsertShoppingItem(ctx, sqlc.InsertShoppingItemParams{
		ListID: list.ID, IngredientID: &tofu.ID, Name: "Tofu", Quantity: &quantity, Unit: &unit,
		Category: "legumes_nuts_seeds", Position: 0, Origin: "manual",
	})
	if err != nil {
		t.Fatalf("insert item: %v", err)
	}
	checked := true
	if _, err := f.store.UpdateShoppingItem(ctx, sqlc.UpdateShoppingItemParams{ID: item.ID, Checked: &checked, CheckedBy: &s.User.ID}); err != nil {
		t.Fatalf("check item: %v", err)
	}

	if err := f.svc.DeleteUser(ctx, s.User.ID); err != nil {
		t.Errorf("DeleteUser with a shopping list that references the caller's own ingredient: %v", err)
	}
	if _, err := f.svc.GetUser(ctx, s.User.ID); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("GetUser after DeleteUser: err = %v, want ErrNotFound", err)
	}
	if _, err := f.store.GetShoppingListForUser(ctx, sqlc.GetShoppingListForUserParams{ID: list.ID, UserID: s.User.ID}); !store.IsNotFound(err) {
		t.Errorf("list after DeleteUser: err = %v, want no rows", err)
	}
}
```

- [x] **Step 2: Run the test and confirm it already passes**

Run: `cd backend && go test ./internal/service/... -run TestDeleteUserWithAShoppingListThatUsesTheirOwnCustomIngredient -v`
Expected: **PASS, before any change to `auth.go`.** This is deliberate, and it is the empirical check of the Global Constraints claim: with `SET NULL` references, the existing chain already deletes such an account. If it FAILS, the Task 1 schema has drifted from this plan (some reference became `NO ACTION`), so stop and fix the schema rather than the test.

- [x] **Step 3: Add the explicit pre-delete**

In `backend/internal/service/auth.go`, append this paragraph to the end of `DeleteUser`'s doc comment (after "Meals must go before users, as before."):

```go
//
// Shopping lists go first of all, but only to keep the chain explicit, not
// because the order is load-bearing: shopping_lists cascades to
// shopping_items, and shopping_items' two references to other user-owned
// rows (ingredient_id, checked_by) are ON DELETE SET NULL, not NO ACTION, so
// no cascade order can make them fail. A future NO ACTION reference into
// shopping_lists or shopping_items would change that.
```

and make the pre-delete the transaction's first statement:

```go
func (a *Auth) DeleteUser(ctx context.Context, id uuid.UUID) error {
	return a.st.InTx(ctx, func(q *sqlc.Queries) error {
		if err := q.DeleteShoppingListsForUser(ctx, id); err != nil {
			return fmt.Errorf("delete shopping lists: %w", err)
		}
		if err := q.DeletePlanEntriesForUser(ctx, id); err != nil {
			return fmt.Errorf("delete plan entries: %w", err)
		}
		if err := q.DeleteDietTemplatesForUser(ctx, id); err != nil {
			return fmt.Errorf("delete diet templates: %w", err)
		}
		if err := q.DeleteMealsForUser(ctx, id); err != nil {
			return fmt.Errorf("delete meals: %w", err)
		}
		n, err := q.DeleteUser(ctx, id)
		if err != nil {
			return fmt.Errorf("delete user: %w", err)
		}
		if n == 0 {
			return ErrNotFound
		}
		return nil
	})
}
```

- [x] **Step 4: Run the tests to verify they still pass**

Run: `cd backend && go test ./internal/service/... -run TestDeleteUser -v`
Expected: PASS, all four `TestDeleteUser*` tests.

- [x] **Step 5: Commit**

```bash
git add backend/internal/service/auth.go backend/internal/service/auth_test.go
git commit -m "feat(backend): delete a user's shopping lists explicitly on account deletion"
```

---

### Task 4: `ListEventHub`, the in-process pub/sub for list events

**Files:**
- Create: `backend/internal/service/shopping_events.go`
- Create: `backend/internal/service/shopping_events_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks (a pure in-memory type; no database).
- Produces (used by Tasks 5, 8 and 9):
  - Constants `service.ListEventItemChanged = "item_changed"`, `ListEventItemDeleted = "item_deleted"`, `ListEventListChanged = "list_changed"`, `ListEventListDeleted = "list_deleted"`.
  - `var service.ErrEventStreamsClosed`.
  - `type ListEvent struct { Type string; ListID uuid.UUID; ItemID *uuid.UUID; Version *int }`
  - `func NewListEventHub() *ListEventHub`
  - `func (*ListEventHub) Subscribe(listID uuid.UUID) (*ListSubscription, error)`
  - `func (*ListEventHub) Publish(ev ListEvent)`
  - `func (*ListEventHub) Subscribers(listID uuid.UUID) int`
  - `func (*ListEventHub) Close()`
  - `func (*ListSubscription) Events() <-chan ListEvent`
  - `func (*ListSubscription) Close()`

- [x] **Step 1: Write the failing tests**

Create `backend/internal/service/shopping_events_test.go`:

```go
package service_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/service"
)

func itemEvent(listID uuid.UUID, version int) service.ListEvent {
	itemID := uuid.New()
	return service.ListEvent{Type: service.ListEventItemChanged, ListID: listID, ItemID: &itemID, Version: &version}
}

func TestListEventHubDeliversOnlyToSubscribersOfThatList(t *testing.T) {
	hub := service.NewListEventHub()
	listA, listB := uuid.New(), uuid.New()
	subA, err := hub.Subscribe(listA)
	if err != nil {
		t.Fatalf("Subscribe A: %v", err)
	}
	defer subA.Close()
	subB, err := hub.Subscribe(listB)
	if err != nil {
		t.Fatalf("Subscribe B: %v", err)
	}
	defer subB.Close()

	hub.Publish(itemEvent(listA, 2))

	select {
	case ev := <-subA.Events():
		if ev.ListID != listA || *ev.Version != 2 {
			t.Errorf("A got %+v, want list A at version 2", ev)
		}
	default:
		t.Fatal("A received nothing")
	}
	select {
	case ev := <-subB.Events():
		t.Errorf("B received %+v, want nothing (different list)", ev)
	default:
	}
}

func TestListEventHubDisconnectsASubscriberThatFallsBehind(t *testing.T) {
	hub := service.NewListEventHub()
	list := uuid.New()
	slow, err := hub.Subscribe(list)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer slow.Close()

	// 32 fit in the buffer; the 33rd finds it full.
	for v := 1; v <= 33; v++ {
		hub.Publish(itemEvent(list, v))
	}

	received := 0
	for range slow.Events() {
		received++
	}
	if received != 32 {
		t.Errorf("received %d buffered events before the channel closed, want 32", received)
	}
	if n := hub.Subscribers(list); n != 0 {
		t.Errorf("Subscribers after the overflow = %d, want 0 (disconnected, not silently dropping)", n)
	}
}

func TestListEventHubEndsStreamsAfterListDeleted(t *testing.T) {
	hub := service.NewListEventHub()
	list := uuid.New()
	sub, err := hub.Subscribe(list)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer sub.Close()

	hub.Publish(service.ListEvent{Type: service.ListEventListDeleted, ListID: list})

	ev, open := <-sub.Events()
	if !open || ev.Type != service.ListEventListDeleted {
		t.Fatalf("first receive = %+v (open %v), want the list_deleted event", ev, open)
	}
	if _, open := <-sub.Events(); open {
		t.Error("channel still open after list_deleted, want closed")
	}
}

func TestListEventHubCloseEndsEveryStreamAndRefusesNewOnes(t *testing.T) {
	hub := service.NewListEventHub()
	sub, err := hub.Subscribe(uuid.New())
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	hub.Close()

	if _, open := <-sub.Events(); open {
		t.Error("channel still open after Close, want closed")
	}
	sub.Close() // must not panic on an already-closed subscription
	if _, err := hub.Subscribe(uuid.New()); !errors.Is(err, service.ErrEventStreamsClosed) {
		t.Errorf("Subscribe after Close: err = %v, want ErrEventStreamsClosed", err)
	}
}
```

- [x] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/service/... -run TestListEventHub -v`
Expected: FAIL to compile, because `service.NewListEventHub`, `service.ListEvent` and the rest do not exist yet.

- [x] **Step 3: Implement the hub**

Create `backend/internal/service/shopping_events.go`:

```go
// Package service holds business rules. It calls the store and never speaks HTTP.
package service

import (
	"errors"
	"sync"

	"github.com/google/uuid"
)

// Shopping-list event types, sent as the SSE "event:" field and the "type"
// member of each event's JSON data.
const (
	ListEventItemChanged = "item_changed"
	ListEventItemDeleted = "item_deleted"
	ListEventListChanged = "list_changed"
	ListEventListDeleted = "list_deleted"
)

// listSubscriptionBuffer is how many undelivered events one subscriber may
// have queued before it is considered too slow and disconnected (see
// ListEventHub.Publish).
const listSubscriptionBuffer = 32

// ErrEventStreamsClosed means the hub has been closed for shutdown and
// accepts no new subscribers.
var ErrEventStreamsClosed = errors.New("event streams are closed")

// ListEvent is one change to a shopping list, as delivered to its event
// streams. ItemID and Version are set for item_changed and item_deleted
// (Version is the item's version after the change, or for item_deleted the
// version it had when it was removed), and nil for list_changed and
// list_deleted.
type ListEvent struct {
	Type    string
	ListID  uuid.UUID
	ItemID  *uuid.UUID
	Version *int
}

// ListEventHub fans shopping-list events out to the event streams open in
// this API process, keyed by list id. It is in memory: this API is one Go
// process backed only by Postgres, with no message broker, so a second API
// replica would not see this replica's events (clients still converge,
// because they refetch on reconnect and on foreground; see spec §4.3).
//
// Every subscriber has a bounded buffer. Publish never blocks: a subscriber
// whose buffer is full is disconnected (its channel is closed) rather than
// having events silently dropped. Item versions are per item, so a client
// can only notice a dropped event if a later event arrives for that same
// item; closing the stream instead makes the client reconnect and refetch,
// which always converges.
type ListEventHub struct {
	mu     sync.Mutex
	subs   map[uuid.UUID]map[*ListSubscription]struct{}
	closed bool
}

// NewListEventHub returns an empty hub.
func NewListEventHub() *ListEventHub {
	return &ListEventHub{subs: make(map[uuid.UUID]map[*ListSubscription]struct{})}
}

// ListSubscription is one open event stream for one list.
type ListSubscription struct {
	hub    *ListEventHub
	listID uuid.UUID
	events chan ListEvent
}

// Events delivers the list's events in publish order. It is closed when the
// list is deleted (after the list_deleted event), when the subscriber falls
// too far behind, when the hub shuts down, or after Close.
func (s *ListSubscription) Events() <-chan ListEvent { return s.events }

// Close unsubscribes. It is safe to call more than once, and after the hub
// has already closed the channel.
func (s *ListSubscription) Close() {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()
	s.hub.removeLocked(s)
}

// Subscribe opens a subscription to listID's events. It does not check who
// may see the list: callers go through ShoppingLists.Subscribe, which does.
func (h *ListEventHub) Subscribe(listID uuid.UUID) (*ListSubscription, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, ErrEventStreamsClosed
	}
	sub := &ListSubscription{hub: h, listID: listID, events: make(chan ListEvent, listSubscriptionBuffer)}
	if h.subs[listID] == nil {
		h.subs[listID] = make(map[*ListSubscription]struct{})
	}
	h.subs[listID][sub] = struct{}{}
	return sub, nil
}

// Publish delivers ev to every subscriber of ev.ListID without blocking. A
// subscriber whose buffer is full is disconnected. A list_deleted event is
// the last one: every subscriber of that list is disconnected right after it
// is queued (a closed channel still yields the values buffered before it).
func (h *ListEventHub) Publish(ev ListEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for sub := range h.subs[ev.ListID] {
		select {
		case sub.events <- ev:
		default:
			h.removeLocked(sub)
		}
	}
	if ev.Type == ListEventListDeleted {
		for sub := range h.subs[ev.ListID] {
			h.removeLocked(sub)
		}
	}
}

// Subscribers reports how many subscriptions listID currently has. Used by
// tests to wait until a stream is listening before publishing.
func (h *ListEventHub) Subscribers(listID uuid.UUID) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs[listID])
}

// Close disconnects every subscriber and refuses new ones. cmd/api registers
// it with http.Server.RegisterOnShutdown: Shutdown does not cancel request
// contexts, so without this an open event stream would never finish and
// every shutdown with a connected client would wait out the full timeout.
func (h *ListEventHub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for _, set := range h.subs {
		for sub := range set {
			h.removeLocked(sub)
		}
	}
}

// removeLocked unsubscribes sub and closes its channel, at most once. The
// caller holds h.mu; every send and close happens under it, so a send can
// never hit a closed channel.
func (h *ListEventHub) removeLocked(sub *ListSubscription) {
	set := h.subs[sub.listID]
	if _, ok := set[sub]; !ok {
		return
	}
	delete(set, sub)
	if len(set) == 0 {
		delete(h.subs, sub.listID)
	}
	close(sub.events)
}
```

Deleting map entries while ranging over the same map (in `Publish` and `Close`) is well defined in Go: a deleted entry that has not been reached yet is simply not visited.

- [x] **Step 4: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/service/... -run TestListEventHub -race -v`
Expected: PASS, all four tests. They need no Docker. `-race` is worth running here because this is the one concurrent data structure in the plan.

- [x] **Step 5: Commit**

```bash
git add backend/internal/service/shopping_events.go backend/internal/service/shopping_events_test.go
git commit -m "feat(backend): add the in-process shopping-list event hub"
```

---

### Task 5: `ShoppingLists` service: lists, items, versions, subscriptions

**Files:**
- Create: `backend/internal/service/shopping_lists.go`
- Create: `backend/internal/service/shopping_lists_test.go`

**Interfaces:**
- Consumes: the `sqlc.Queries` methods from Task 2; `GetIngredientsForUser` (existing); the `ListEventHub` from Task 4; `store.IsNotFound`, `store.IsForeignKeyViolation`, `store.InTx` (existing); `toRowLimit` (`ingredients.go`), `Optional[T]`/`Set[T]` (`auth.go`) and `ErrNotFound` (existing).
- Produces (consumed by Task 6, and by Task 7's and Task 8's handlers):
  - Errors `service.ErrShoppingListNotFound`, `ErrShoppingItemNotFound`, `ErrShoppingItemIngredientNotFound`, `ErrShoppingItemVersionRequired` (all `errors.New`), and `type ShoppingItemVersionConflictError struct { Current ShoppingItem }` (a pointer receiver implements `error`; match it with `errors.As`).
  - `type ShoppingItem struct { ID, ListID uuid.UUID; IngredientID *uuid.UUID; Name string; Quantity *float64; Unit *string; Category string; Checked bool; CheckedBy *uuid.UUID; Position, Version int; Origin string; CreatedAt, UpdatedAt time.Time }`
  - `type ShoppingList struct { ID uuid.UUID; Name string; SharedWithPartner bool; SourceFrom, SourceTo *time.Time; Items []ShoppingItem; CreatedAt, UpdatedAt time.Time }`
  - `type ShoppingListSummary struct { ID uuid.UUID; Name string; SharedWithPartner bool; SourceFrom, SourceTo *time.Time; CreatedAt, UpdatedAt time.Time }`
  - `type ShoppingListCursor struct { CreatedAt time.Time; ID uuid.UUID }`, `type ListShoppingListsInput struct { Cursor *ShoppingListCursor; Limit int }`, `type ShoppingListPage struct { Items []ShoppingListSummary; NextCursor *ShoppingListCursor }`
  - `type CreateShoppingListInput struct { Name string; SharedWithPartner bool }`, `type UpdateShoppingListInput struct { Name *string; SharedWithPartner *bool }`
  - `type CreateShoppingItemInput struct { IngredientID *uuid.UUID; Name string; Quantity *float64; Unit, Category *string }`
  - `type UpdateShoppingItemInput struct { Version *int; Name *string; Quantity Optional[float64]; Unit Optional[string]; Category *string; Checked *bool }`
  - `func NewShoppingLists(st *store.Store, events *ListEventHub) *ShoppingLists`
  - `func (*ShoppingLists) Create(ctx, ownerID uuid.UUID, in CreateShoppingListInput) (ShoppingList, error)`
  - `func (*ShoppingLists) Get(ctx, ownerID, id uuid.UUID) (ShoppingList, error)`
  - `func (*ShoppingLists) List(ctx, ownerID uuid.UUID, in ListShoppingListsInput) (ShoppingListPage, error)`
  - `func (*ShoppingLists) Update(ctx, ownerID, id uuid.UUID, in UpdateShoppingListInput) (ShoppingList, error)`
  - `func (*ShoppingLists) Delete(ctx, ownerID, id uuid.UUID) error`
  - `func (*ShoppingLists) AddItem(ctx, ownerID, listID uuid.UUID, in CreateShoppingItemInput) (ShoppingItem, error)`
  - `func (*ShoppingLists) UpdateItem(ctx, ownerID, listID, itemID uuid.UUID, in UpdateShoppingItemInput) (ShoppingItem, error)`
  - `func (*ShoppingLists) DeleteItem(ctx, ownerID, listID, itemID uuid.UUID) error`
  - `func (*ShoppingLists) Subscribe(ctx, ownerID, listID uuid.UUID) (*ListSubscription, error)`
  - Unexported, used by Task 6: `toShoppingList(row sqlc.ShoppingList, itemRows []sqlc.ShoppingItem) ShoppingList`, `toShoppingItem(sqlc.ShoppingItem) ShoppingItem`, `fromPgDatePtr(pgtype.Date) *time.Time`, `(*ShoppingLists).publishItem(typ string, listID, itemID uuid.UUID, version int)`.

- [x] **Step 1: Write the failing tests**

Create `backend/internal/service/shopping_lists_test.go`. It reuses `newIngredientsFixture`, `newTestUser` and `mustCreateIngredient` (`ingredients_test.go`, `meals_test.go`) and the generic `ptr` helper already defined in `auth_test.go`, all in the same `service_test` package:

```go
package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/InzKazik/mealplanner/backend/internal/service"
	"github.com/InzKazik/mealplanner/backend/internal/store"
)

type shoppingFixture struct {
	lists  *service.ShoppingLists
	events *service.ListEventHub
	meals  *service.Meals
	ing    *service.Ingredients
	plan   *service.Plan
	st     *store.Store
}

func newShoppingListsFixture(t *testing.T) shoppingFixture {
	t.Helper()
	ing, st := newIngredientsFixture(t)
	meals := service.NewMeals(st)
	events := service.NewListEventHub()
	return shoppingFixture{
		lists: service.NewShoppingLists(st, events), events: events,
		meals: meals, ing: ing, plan: service.NewPlan(st, meals), st: st,
	}
}

// nextEvent waits briefly for the subscription's next event.
func nextEvent(t *testing.T, sub *service.ListSubscription) service.ListEvent {
	t.Helper()
	select {
	case ev, open := <-sub.Events():
		if !open {
			t.Fatal("subscription closed, want an event")
		}
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("no event within 2s")
	}
	return service.ListEvent{}
}

func TestShoppingListsUpdateItemEnforcesVersionsForEditsButNotForChecks(t *testing.T) {
	f := newShoppingListsFixture(t)
	ctx := context.Background()
	owner := newTestUser(t, f.st, "shopper3@example.com")
	other := newTestUser(t, f.st, "other3@example.com")

	list, err := f.lists.Create(ctx, owner, service.CreateShoppingListInput{Name: "Groceries"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	item, err := f.lists.AddItem(ctx, owner, list.ID, service.CreateShoppingItemInput{Name: "Milk"})
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if item.Version != 1 || item.Category != "other" || item.Origin != "manual" {
		t.Fatalf("new item = %+v, want version 1, category other, origin manual", item)
	}

	item, err = f.lists.UpdateItem(ctx, owner, list.ID, item.ID, service.UpdateShoppingItemInput{Version: ptr(1), Name: ptr("Oat milk")})
	if err != nil {
		t.Fatalf("rename at the current version: %v", err)
	}
	if item.Version != 2 || item.Name != "Oat milk" {
		t.Fatalf("after rename = %+v, want version 2 named Oat milk", item)
	}

	_, err = f.lists.UpdateItem(ctx, owner, list.ID, item.ID, service.UpdateShoppingItemInput{
		Version: ptr(1), Quantity: service.Set(ptr(2.0)),
	})
	var conflict *service.ShoppingItemVersionConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("edit at a stale version: err = %v, want a version conflict", err)
	}
	if conflict.Current.Version != 2 || conflict.Current.Name != "Oat milk" {
		t.Errorf("conflict.Current = %+v, want the item at version 2", conflict.Current)
	}

	if _, err := f.lists.UpdateItem(ctx, owner, list.ID, item.ID, service.UpdateShoppingItemInput{Name: ptr("Soy milk")}); !errors.Is(err, service.ErrShoppingItemVersionRequired) {
		t.Errorf("edit without a version: err = %v, want ErrShoppingItemVersionRequired", err)
	}

	item, err = f.lists.UpdateItem(ctx, owner, list.ID, item.ID, service.UpdateShoppingItemInput{Checked: ptr(true)})
	if err != nil {
		t.Fatalf("check without a version: %v", err)
	}
	if !item.Checked || item.Version != 3 || item.CheckedBy == nil || *item.CheckedBy != owner {
		t.Errorf("after check = %+v, want checked by the owner at version 3", item)
	}

	// A replay of the same check, carrying a version that is stale by now,
	// is neither a conflict nor a change.
	replayed, err := f.lists.UpdateItem(ctx, owner, list.ID, item.ID, service.UpdateShoppingItemInput{Version: ptr(2), Checked: ptr(true)})
	if err != nil {
		t.Fatalf("replayed check: %v", err)
	}
	if replayed.Version != 3 {
		t.Errorf("version after a replayed check = %d, want 3 (no-op)", replayed.Version)
	}

	item, err = f.lists.UpdateItem(ctx, owner, list.ID, item.ID, service.UpdateShoppingItemInput{Checked: ptr(false)})
	if err != nil {
		t.Fatalf("uncheck: %v", err)
	}
	if item.Checked || item.CheckedBy != nil || item.Version != 4 {
		t.Errorf("after uncheck = %+v, want unchecked, checked_by nil, version 4", item)
	}

	if _, err := f.lists.UpdateItem(ctx, other, list.ID, item.ID, service.UpdateShoppingItemInput{Checked: ptr(true)}); !errors.Is(err, service.ErrShoppingItemNotFound) {
		t.Errorf("another user's check: err = %v, want ErrShoppingItemNotFound", err)
	}
}

func TestShoppingListsItemChangesReachTheListsEventStream(t *testing.T) {
	f := newShoppingListsFixture(t)
	ctx := context.Background()
	owner := newTestUser(t, f.st, "shopper4@example.com")
	other := newTestUser(t, f.st, "other4@example.com")

	list, err := f.lists.Create(ctx, owner, service.CreateShoppingListInput{Name: "Groceries"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.lists.Subscribe(ctx, other, list.ID); !errors.Is(err, service.ErrShoppingListNotFound) {
		t.Errorf("another user's Subscribe: err = %v, want ErrShoppingListNotFound", err)
	}
	if n := f.events.Subscribers(list.ID); n != 0 {
		t.Errorf("Subscribers after a refused Subscribe = %d, want 0", n)
	}
	sub, err := f.lists.Subscribe(ctx, owner, list.ID)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer sub.Close()

	item, err := f.lists.AddItem(ctx, owner, list.ID, service.CreateShoppingItemInput{Name: "Bread"})
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if ev := nextEvent(t, sub); ev.Type != service.ListEventItemChanged || *ev.ItemID != item.ID || *ev.Version != 1 {
		t.Errorf("after add: event = %+v, want item_changed at version 1", ev)
	}
	if _, err := f.lists.UpdateItem(ctx, owner, list.ID, item.ID, service.UpdateShoppingItemInput{Checked: ptr(true)}); err != nil {
		t.Fatalf("check: %v", err)
	}
	if ev := nextEvent(t, sub); ev.Type != service.ListEventItemChanged || *ev.Version != 2 {
		t.Errorf("after check: event = %+v, want item_changed at version 2", ev)
	}
	// A no-op check publishes nothing: the next event must be the delete.
	if _, err := f.lists.UpdateItem(ctx, owner, list.ID, item.ID, service.UpdateShoppingItemInput{Checked: ptr(true)}); err != nil {
		t.Fatalf("no-op check: %v", err)
	}
	if err := f.lists.DeleteItem(ctx, owner, list.ID, item.ID); err != nil {
		t.Fatalf("DeleteItem: %v", err)
	}
	if ev := nextEvent(t, sub); ev.Type != service.ListEventItemDeleted || *ev.ItemID != item.ID || *ev.Version != 2 {
		t.Errorf("after delete: event = %+v, want item_deleted at version 2", ev)
	}
	if err := f.lists.Delete(ctx, owner, list.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if ev := nextEvent(t, sub); ev.Type != service.ListEventListDeleted {
		t.Errorf("after list delete: event = %+v, want list_deleted", ev)
	}
	if _, open := <-sub.Events(); open {
		t.Error("stream still open after list_deleted, want closed")
	}
}

func TestShoppingListsDeletingAnIngredientKeepsItsItems(t *testing.T) {
	f := newShoppingListsFixture(t)
	ctx := context.Background()
	owner := newTestUser(t, f.st, "shopper5@example.com")
	other := newTestUser(t, f.st, "other5@example.com")

	tofu := mustCreateIngredient(t, f.ing, owner, service.CreateIngredientInput{Name: "Tofu", Category: "legumes_nuts_seeds"})
	list, err := f.lists.Create(ctx, owner, service.CreateShoppingListInput{Name: "Groceries"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	item, err := f.lists.AddItem(ctx, owner, list.ID, service.CreateShoppingItemInput{
		IngredientID: &tofu.ID, Name: "Tofu", Quantity: ptr(400.0), Unit: ptr("g"),
	})
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if item.Category != "legumes_nuts_seeds" {
		t.Errorf("category = %q, want the ingredient's category", item.Category)
	}

	othersIngredient := mustCreateIngredient(t, f.ing, other, service.CreateIngredientInput{Name: "Secret", Category: "other"})
	if _, err := f.lists.AddItem(ctx, owner, list.ID, service.CreateShoppingItemInput{IngredientID: &othersIngredient.ID, Name: "Secret"}); !errors.Is(err, service.ErrShoppingItemIngredientNotFound) {
		t.Errorf("AddItem with another user's ingredient: err = %v, want ErrShoppingItemIngredientNotFound", err)
	}

	// Unlike a meal line, a shopping item never blocks deleting its ingredient.
	if err := f.ing.Delete(ctx, owner, tofu.ID); err != nil {
		t.Fatalf("delete an ingredient a shopping item references: %v", err)
	}
	got, err := f.lists.Get(ctx, owner, list.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].IngredientID != nil || got.Items[0].Name != "Tofu" || *got.Items[0].Quantity != 400 {
		t.Errorf("items after the ingredient was deleted = %+v, want the same item with ingredient_id nil", got.Items)
	}
}

func TestShoppingListsAreVisibleToTheirOwnerOnly(t *testing.T) {
	f := newShoppingListsFixture(t)
	ctx := context.Background()
	owner := newTestUser(t, f.st, "shopper6@example.com")
	other := newTestUser(t, f.st, "other6@example.com")

	list, err := f.lists.Create(ctx, owner, service.CreateShoppingListInput{Name: "Mine", SharedWithPartner: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.lists.Get(ctx, other, list.ID); !errors.Is(err, service.ErrShoppingListNotFound) {
		t.Errorf("Get by another user (even with shared_with_partner): err = %v, want ErrShoppingListNotFound", err)
	}
	if _, err := f.lists.AddItem(ctx, other, list.ID, service.CreateShoppingItemInput{Name: "Sneaky"}); !errors.Is(err, service.ErrShoppingListNotFound) {
		t.Errorf("AddItem by another user: err = %v, want ErrShoppingListNotFound", err)
	}
	if _, err := f.lists.Update(ctx, other, list.ID, service.UpdateShoppingListInput{Name: ptr("Mine now")}); !errors.Is(err, service.ErrShoppingListNotFound) {
		t.Errorf("Update by another user: err = %v, want ErrShoppingListNotFound", err)
	}
	if err := f.lists.Delete(ctx, other, list.ID); !errors.Is(err, service.ErrShoppingListNotFound) {
		t.Errorf("Delete by another user: err = %v, want ErrShoppingListNotFound", err)
	}
	page, err := f.lists.List(ctx, other, service.ListShoppingListsInput{Limit: 10})
	if err != nil || len(page.Items) != 0 {
		t.Errorf("List by another user = %+v (err %v), want empty", page.Items, err)
	}
}
```

- [x] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/service/... -run TestShoppingLists -v`
Expected: FAIL to compile, because `service.NewShoppingLists` and the rest do not exist yet.

- [x] **Step 3: Implement the service**

Create `backend/internal/service/shopping_lists.go`:

```go
// Package service holds business rules. It calls the store and never speaks HTTP.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"
)

// Errors returned by ShoppingLists. Handlers map them to problem responses.
var (
	// ErrShoppingListNotFound means the list does not exist or is not owned
	// by the caller. Partner visibility is not implemented yet (see the
	// ShoppingLists doc comment).
	ErrShoppingListNotFound = errors.New("shopping list not found")
	// ErrShoppingItemNotFound means the item does not exist, is not on the
	// given list, or the list is not visible to the caller.
	ErrShoppingItemNotFound = errors.New("shopping item not found")
	// ErrShoppingItemIngredientNotFound means a new item's ingredient_id does
	// not exist or is not visible to the caller.
	ErrShoppingItemIngredientNotFound = errors.New("ingredient does not exist or is not visible to you")
	// ErrShoppingItemVersionRequired means an item edit changes name,
	// quantity, unit or category without saying which version it edits.
	ErrShoppingItemVersionRequired = errors.New("version is required to change an item's name, quantity, unit or category")
)

// ShoppingItemVersionConflictError means an item edit carried a version that
// is no longer current. Current is the item as it is now, so the client can
// re-apply its change on top of it (spec §4.3).
type ShoppingItemVersionConflictError struct {
	Current ShoppingItem
}

func (e *ShoppingItemVersionConflictError) Error() string {
	return fmt.Sprintf("shopping item %s is at version %d", e.Current.ID, e.Current.Version)
}

// ShoppingItem is one line of a shopping list. Name, Quantity, Unit and
// Category are the item's own copy, not read live from the ingredient:
// IngredientID becomes nil if the ingredient is later deleted, and the item
// keeps working exactly like a free-text item.
type ShoppingItem struct {
	ID           uuid.UUID
	ListID       uuid.UUID
	IngredientID *uuid.UUID
	Name         string
	Quantity     *float64
	Unit         *string
	Category     string
	Checked      bool
	CheckedBy    *uuid.UUID
	Position     int
	Version      int
	Origin       string // "generated" | "manual"
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ShoppingList is a list with its items, ordered by position.
type ShoppingList struct {
	ID                uuid.UUID
	Name              string
	SharedWithPartner bool
	SourceFrom        *time.Time
	SourceTo          *time.Time
	Items             []ShoppingItem
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// ShoppingListSummary is a list without its items, for the list endpoint.
type ShoppingListSummary struct {
	ID                uuid.UUID
	Name              string
	SharedWithPartner bool
	SourceFrom        *time.Time
	SourceTo          *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// ShoppingListCursor is an opaque position in the newest-first list.
type ShoppingListCursor struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// ListShoppingListsInput selects a page of the caller's newest-first lists.
type ListShoppingListsInput struct {
	Cursor *ShoppingListCursor
	Limit  int
}

// ShoppingListPage is one page of summaries plus the cursor for the next one
// (nil on the last page).
type ShoppingListPage struct {
	Items      []ShoppingListSummary
	NextCursor *ShoppingListCursor
}

// CreateShoppingListInput creates an empty list.
type CreateShoppingListInput struct {
	Name              string
	SharedWithPartner bool
}

// UpdateShoppingListInput is a partial update of a list's own fields.
type UpdateShoppingListInput struct {
	Name              *string
	SharedWithPartner *bool
}

// CreateShoppingItemInput adds a manual item. Category defaults to the
// ingredient's category when IngredientID is set, and to "other" otherwise.
type CreateShoppingItemInput struct {
	IngredientID *uuid.UUID
	Name         string
	Quantity     *float64
	Unit         *string
	Category     *string
}

// UpdateShoppingItemInput is a partial item update. Changing Name, Quantity,
// Unit or Category is an edit and needs Version (optimistic concurrency);
// changing only Checked is last-write-wins and ignores Version (spec §4.3).
type UpdateShoppingItemInput struct {
	Version  *int
	Name     *string
	Quantity Optional[float64]
	Unit     Optional[string]
	Category *string
	Checked  *bool
}

// ShoppingLists implements shopping lists owned by a single user, their
// generation from the plan, item edits with optimistic concurrency, and the
// live event streams. Partner sharing is not implemented: shared_with_partner
// is stored, but every read and write here checks owner_id only, and only
// the owner's own edits reach a list's event streams. See the "Not built
// yet" note in backend/CLAUDE.md.
type ShoppingLists struct {
	st     *store.Store
	events *ListEventHub
}

// NewShoppingLists returns a ShoppingLists service that publishes to events.
func NewShoppingLists(st *store.Store, events *ListEventHub) *ShoppingLists {
	return &ShoppingLists{st: st, events: events}
}

// Create adds an empty list owned by ownerID.
func (s *ShoppingLists) Create(ctx context.Context, ownerID uuid.UUID, in CreateShoppingListInput) (ShoppingList, error) {
	row, err := s.st.CreateShoppingList(ctx, sqlc.CreateShoppingListParams{
		OwnerID: ownerID, Name: in.Name, SharedWithPartner: in.SharedWithPartner,
	})
	if store.IsForeignKeyViolation(err, "shopping_lists_owner_id_fkey") {
		// Mirrors Ingredients.Create: an access token for a user that no
		// longer exists is unauthorized, not a 500.
		return ShoppingList{}, ErrNotFound
	}
	if err != nil {
		return ShoppingList{}, fmt.Errorf("create shopping list: %w", err)
	}
	return toShoppingList(row, nil), nil
}

// Get returns a list owned by ownerID, with its items.
func (s *ShoppingLists) Get(ctx context.Context, ownerID, id uuid.UUID) (ShoppingList, error) {
	row, err := s.st.GetShoppingListForUser(ctx, sqlc.GetShoppingListForUserParams{ID: id, UserID: ownerID})
	if store.IsNotFound(err) {
		return ShoppingList{}, ErrShoppingListNotFound
	}
	if err != nil {
		return ShoppingList{}, fmt.Errorf("get shopping list: %w", err)
	}
	items, err := s.st.GetShoppingItems(ctx, id)
	if err != nil {
		return ShoppingList{}, fmt.Errorf("get shopping items: %w", err)
	}
	return toShoppingList(row, items), nil
}

// List returns a page of the caller's lists, newest first.
func (s *ShoppingLists) List(ctx context.Context, ownerID uuid.UUID, in ListShoppingListsInput) (ShoppingListPage, error) {
	if in.Limit < 1 {
		in.Limit = 1
	}
	params := sqlc.ListShoppingListsForUserParams{UserID: ownerID, RowLimit: toRowLimit(in.Limit + 1)}
	if in.Cursor != nil {
		params.HasCursor = true
		params.CursorCreatedAt = in.Cursor.CreatedAt
		params.CursorID = in.Cursor.ID
	}
	rows, err := s.st.ListShoppingListsForUser(ctx, params)
	if err != nil {
		return ShoppingListPage{}, fmt.Errorf("list shopping lists: %w", err)
	}
	var next *ShoppingListCursor
	if len(rows) > in.Limit {
		last := rows[in.Limit-1]
		next = &ShoppingListCursor{CreatedAt: last.CreatedAt, ID: last.ID}
		rows = rows[:in.Limit]
	}
	items := make([]ShoppingListSummary, len(rows))
	for i, r := range rows {
		items[i] = ShoppingListSummary{
			ID: r.ID, Name: r.Name, SharedWithPartner: r.SharedWithPartner,
			SourceFrom: fromPgDatePtr(r.SourceFrom), SourceTo: fromPgDatePtr(r.SourceTo),
			CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		}
	}
	return ShoppingListPage{Items: items, NextCursor: next}, nil
}

// Update applies a partial update to a list owned by ownerID and tells its
// event streams the list changed.
func (s *ShoppingLists) Update(ctx context.Context, ownerID, id uuid.UUID, in UpdateShoppingListInput) (ShoppingList, error) {
	var list ShoppingList
	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
		row, err := q.UpdateShoppingList(ctx, sqlc.UpdateShoppingListParams{
			ID: id, UserID: ownerID, Name: in.Name, SharedWithPartner: in.SharedWithPartner,
		})
		if store.IsNotFound(err) {
			return ErrShoppingListNotFound
		}
		if err != nil {
			return fmt.Errorf("update shopping list: %w", err)
		}
		items, err := q.GetShoppingItems(ctx, id)
		if err != nil {
			return fmt.Errorf("get shopping items: %w", err)
		}
		list = toShoppingList(row, items)
		return nil
	})
	if err != nil {
		return ShoppingList{}, err
	}
	s.events.Publish(ListEvent{Type: ListEventListChanged, ListID: id})
	return list, nil
}

// Delete removes a list owned by ownerID (its items go with it, ON DELETE
// CASCADE) and ends its event streams with a list_deleted event.
func (s *ShoppingLists) Delete(ctx context.Context, ownerID, id uuid.UUID) error {
	n, err := s.st.DeleteShoppingList(ctx, sqlc.DeleteShoppingListParams{ID: id, UserID: ownerID})
	if err != nil {
		return fmt.Errorf("delete shopping list: %w", err)
	}
	if n == 0 {
		return ErrShoppingListNotFound
	}
	s.events.Publish(ListEvent{Type: ListEventListDeleted, ListID: id})
	return nil
}

// AddItem appends a manual item to a list owned by ownerID.
func (s *ShoppingLists) AddItem(ctx context.Context, ownerID, listID uuid.UUID, in CreateShoppingItemInput) (ShoppingItem, error) {
	var item ShoppingItem
	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
		// TouchShoppingListForUser checks ownership and takes the list row's
		// write lock, so two concurrent adds (or an add racing a
		// regeneration) cannot both read the same next position.
		if _, err := q.TouchShoppingListForUser(ctx, sqlc.TouchShoppingListForUserParams{ID: listID, UserID: ownerID}); err != nil {
			if store.IsNotFound(err) {
				return ErrShoppingListNotFound
			}
			return fmt.Errorf("get shopping list: %w", err)
		}
		category := "other"
		if in.IngredientID != nil {
			rows, err := q.GetIngredientsForUser(ctx, sqlc.GetIngredientsForUserParams{Ids: []uuid.UUID{*in.IngredientID}, UserID: &ownerID})
			if err != nil {
				return fmt.Errorf("get ingredient: %w", err)
			}
			if len(rows) == 0 {
				return ErrShoppingItemIngredientNotFound
			}
			category = rows[0].Category
		}
		if in.Category != nil {
			category = *in.Category
		}
		pos, err := q.NextShoppingItemPosition(ctx, listID)
		if err != nil {
			return fmt.Errorf("next item position: %w", err)
		}
		row, err := q.InsertShoppingItem(ctx, sqlc.InsertShoppingItemParams{
			ListID: listID, IngredientID: in.IngredientID, Name: in.Name, Quantity: in.Quantity, Unit: in.Unit,
			Category: category, Position: pos, Origin: "manual",
		})
		// The ingredient was visible a moment ago in this same transaction;
		// a concurrent delete of it can still win the race. Answer as if it
		// never existed rather than with a raw foreign-key 500.
		if store.IsForeignKeyViolation(err, "shopping_items_ingredient_id_fkey") {
			return ErrShoppingItemIngredientNotFound
		}
		if err != nil {
			return fmt.Errorf("insert shopping item: %w", err)
		}
		item = toShoppingItem(row)
		return nil
	})
	if err != nil {
		return ShoppingItem{}, err
	}
	s.publishItem(ListEventItemChanged, item.ListID, item.ID, item.Version)
	return item, nil
}

// UpdateItem edits or checks off an item on a list owned by ownerID.
//
// An edit (Name, Quantity, Unit or Category present) needs in.Version and
// fails with *ShoppingItemVersionConflictError, carrying the current item,
// if the version is stale. A request that only sets Checked is
// last-write-wins: its Version is ignored, and setting Checked to the value
// it already has changes nothing (no version bump, no event), so retries
// and offline replays are safe. Every real change bumps the version by one
// and returns the item with its new version for the client to chain on.
func (s *ShoppingLists) UpdateItem(ctx context.Context, ownerID, listID, itemID uuid.UUID, in UpdateShoppingItemInput) (ShoppingItem, error) {
	isEdit := in.Name != nil || in.Quantity.Specified || in.Unit.Specified || in.Category != nil
	if isEdit && in.Version == nil {
		return ShoppingItem{}, ErrShoppingItemVersionRequired
	}
	var (
		item    ShoppingItem
		changed bool
	)
	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
		// FOR UPDATE: the version check below and the UPDATE after it must
		// see the same row, so a concurrent edit waits here instead of
		// slipping in between them.
		cur, err := q.GetShoppingItemForUserForUpdate(ctx, sqlc.GetShoppingItemForUserForUpdateParams{
			ID: itemID, ListID: listID, UserID: ownerID,
		})
		if store.IsNotFound(err) {
			return ErrShoppingItemNotFound
		}
		if err != nil {
			return fmt.Errorf("get shopping item: %w", err)
		}
		if isEdit && int(cur.Version) != *in.Version {
			return &ShoppingItemVersionConflictError{Current: toShoppingItem(cur)}
		}
		if !isEdit && (in.Checked == nil || *in.Checked == cur.Checked) {
			item = toShoppingItem(cur)
			return nil
		}
		row, err := q.UpdateShoppingItem(ctx, sqlc.UpdateShoppingItemParams{
			ID: itemID, Name: in.Name,
			SetQuantity: in.Quantity.Specified, Quantity: in.Quantity.Value,
			SetUnit: in.Unit.Specified, Unit: in.Unit.Value,
			Category: in.Category, Checked: in.Checked, CheckedBy: &ownerID,
		})
		if err != nil {
			return fmt.Errorf("update shopping item: %w", err)
		}
		item, changed = toShoppingItem(row), true
		return nil
	})
	if err != nil {
		return ShoppingItem{}, err
	}
	if changed {
		s.publishItem(ListEventItemChanged, item.ListID, item.ID, item.Version)
	}
	return item, nil
}

// DeleteItem removes an item from a list owned by ownerID, whatever its
// version: removes merge (spec §4.3), so a remove is never a conflict.
func (s *ShoppingLists) DeleteItem(ctx context.Context, ownerID, listID, itemID uuid.UUID) error {
	row, err := s.st.DeleteShoppingItemForUser(ctx, sqlc.DeleteShoppingItemForUserParams{
		ID: itemID, ListID: listID, UserID: ownerID,
	})
	if store.IsNotFound(err) {
		return ErrShoppingItemNotFound
	}
	if err != nil {
		return fmt.Errorf("delete shopping item: %w", err)
	}
	s.publishItem(ListEventItemDeleted, listID, row.ID, int(row.Version))
	return nil
}

// Subscribe opens an event stream for a list owned by ownerID. It subscribes
// before checking ownership, so a delete that commits between the two can
// never be missed: either the check sees the list gone (404), or the
// subscription is already registered when the list_deleted event is
// published.
func (s *ShoppingLists) Subscribe(ctx context.Context, ownerID, listID uuid.UUID) (*ListSubscription, error) {
	sub, err := s.events.Subscribe(listID)
	if err != nil {
		return nil, err
	}
	if _, err := s.st.GetShoppingListForUser(ctx, sqlc.GetShoppingListForUserParams{ID: listID, UserID: ownerID}); err != nil {
		sub.Close()
		if store.IsNotFound(err) {
			return nil, ErrShoppingListNotFound
		}
		return nil, fmt.Errorf("get shopping list: %w", err)
	}
	return sub, nil
}

func (s *ShoppingLists) publishItem(typ string, listID, itemID uuid.UUID, version int) {
	s.events.Publish(ListEvent{Type: typ, ListID: listID, ItemID: &itemID, Version: &version})
}

func toShoppingList(row sqlc.ShoppingList, itemRows []sqlc.ShoppingItem) ShoppingList {
	items := make([]ShoppingItem, len(itemRows))
	for i, r := range itemRows {
		items[i] = toShoppingItem(r)
	}
	return ShoppingList{
		ID: row.ID, Name: row.Name, SharedWithPartner: row.SharedWithPartner,
		SourceFrom: fromPgDatePtr(row.SourceFrom), SourceTo: fromPgDatePtr(row.SourceTo),
		Items: items, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func toShoppingItem(r sqlc.ShoppingItem) ShoppingItem {
	return ShoppingItem{
		ID: r.ID, ListID: r.ListID, IngredientID: r.IngredientID, Name: r.Name,
		Quantity: r.Quantity, Unit: r.Unit, Category: r.Category,
		Checked: r.Checked, CheckedBy: r.CheckedBy, Position: int(r.Position), Version: int(r.Version),
		Origin: r.Origin, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

// fromPgDatePtr is fromPgDate for a nullable date column: nil when the
// column is NULL.
func fromPgDatePtr(d pgtype.Date) *time.Time {
	if !d.Valid {
		return nil
	}
	t := d.Time
	return &t
}
```

Two conversions need a word. `int(cur.Version)` and `int(row.Position)` widen `int32` to `int`, which `gosec` G115 accepts. The narrowing direction, `int` → `int32`, only happens in Task 6, through the existing `toInt32` helper in `util.go`. `CheckedBy: &ownerID` is passed on every update, and the query itself decides whether to use it (only on an actual check, see `UpdateShoppingItem` in Task 2).

- [x] **Step 4: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/service/... -run 'TestShoppingLists|TestListEventHub' -v`
Expected: PASS, including all four new `TestShoppingLists*` tests.

Run: `cd backend && go build ./...`
Expected: builds cleanly.

- [x] **Step 5: Commit**

```bash
git add backend/internal/service/shopping_lists.go backend/internal/service/shopping_lists_test.go
git commit -m "feat(backend): add the shopping lists service with versioned item edits and events"
```

---

### Task 6: `ShoppingLists.Generate`: a list from the plan

**Files:**
- Modify: `backend/internal/service/shopping_lists.go`
- Modify: `backend/internal/service/shopping_lists_test.go`

**Interfaces:**
- Consumes: `GetPlanEntriesForUserInRange`, `GetMealsForUser`, `GetIngredientsForUser` (existing); `GetMealIngredientsForMeals`, `SetShoppingListSourceForUser`, `DeleteGeneratedShoppingItems`, `NextShoppingItemPosition`, `InsertShoppingItem`, `GetShoppingItems`, `CreateShoppingList` (Task 2); `gramsFor`, `ErrUnitNotConvertible`, `ErrMealIngredientNotFound` (`meals.go`); `maxPlanRangeDays`, `ErrPlanRangeInvalid`, `ErrPlanRangeTooLong` (`plan.go`); `uniqueUUIDs`, `toPgDate`, `toInt32` (`util.go`); `toShoppingList` (Task 5).
- Produces (consumed by Task 7's handler):
  - `type GenerateShoppingListInput struct { From, To time.Time; Name *string; ListID *uuid.UUID }`
  - `func (*ShoppingLists) Generate(ctx, ownerID uuid.UUID, in GenerateShoppingListInput) (list ShoppingList, created bool, err error)`
  - Unexported: `generatedLine`, `generateLines`, `mergeUnits`.

- [x] **Step 1: Write the failing tests**

In `backend/internal/service/shopping_lists_test.go`, add `"github.com/google/uuid"` and `"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"` to the imports, then append:

```go
func mustSetEntry(t *testing.T, plan *service.Plan, owner uuid.UUID, date time.Time, slot string, mealID uuid.UUID, portion float64) {
	t.Helper()
	if _, err := plan.SetEntry(context.Background(), owner, date, slot, service.SetPlanEntryInput{MealID: mealID, Portion: portion}); err != nil {
		t.Fatalf("SetEntry %s %s: %v", date.Format(time.DateOnly), slot, err)
	}
}

func deref[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

// TestShoppingListsGenerateSumsMergesAndGroupsByCategory is the shopping-list
// merging test spec §6 calls for.
func TestShoppingListsGenerateSumsMergesAndGroupsByCategory(t *testing.T) {
	f := newShoppingListsFixture(t)
	ctx := context.Background()
	owner := newTestUser(t, f.st, "shopper1@example.com")

	rice := mustCreateIngredient(t, f.ing, owner, service.CreateIngredientInput{Name: "Rice", Category: "grains_bread"})
	egg := mustCreateIngredient(t, f.ing, owner, service.CreateIngredientInput{Name: "Egg", Category: "dairy_eggs", GramsPerPiece: ptr(50.0)})
	oil := mustCreateIngredient(t, f.ing, owner, service.CreateIngredientInput{Name: "Olive Oil", Category: "condiments_oils", DensityGPerMl: ptr(0.92)})
	flour := mustCreateIngredient(t, f.ing, owner, service.CreateIngredientInput{Name: "Flour", Category: "grains_bread"})

	bowl, err := f.meals.Create(ctx, owner, service.CreateMealInput{Name: "Rice Bowl", Servings: 2})
	if err != nil {
		t.Fatalf("create meal: %v", err)
	}
	if _, err := f.meals.ReplaceIngredients(ctx, owner, bowl.ID, []service.MealIngredientInput{
		{IngredientID: rice.ID, Quantity: 200, Unit: "g"},
		{IngredientID: oil.ID, Quantity: 10, Unit: "ml"},
		{IngredientID: egg.ID, Quantity: 2, Unit: "piece"},
	}); err != nil {
		t.Fatalf("ReplaceIngredients (bowl): %v", err)
	}
	pancakes, err := f.meals.Create(ctx, owner, service.CreateMealInput{Name: "Pancakes", Servings: 1})
	if err != nil {
		t.Fatalf("create meal: %v", err)
	}
	if _, err := f.meals.ReplaceIngredients(ctx, owner, pancakes.ID, []service.MealIngredientInput{
		{IngredientID: flour.ID, Quantity: 100, Unit: "g"},
		{IngredientID: oil.ID, Quantity: 5, Unit: "g"},
	}); err != nil {
		t.Fatalf("ReplaceIngredients (pancakes): %v", err)
	}
	// Flour has no grams_per_piece, so Meals.ReplaceIngredients would refuse
	// a "piece" line. Insert one directly: it stands for the narrow, accepted
	// race backend/CLAUDE.md documents (an ingredient edit interleaving a
	// meal write), and generation must still never merge it into grams.
	if _, err := f.st.InsertMealIngredient(ctx, sqlc.InsertMealIngredientParams{
		MealID: pancakes.ID, IngredientID: flour.ID, Quantity: 1, Unit: "piece", Position: 2,
	}); err != nil {
		t.Fatalf("insert unconvertible line: %v", err)
	}

	day1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	day2 := day1.AddDate(0, 0, 1)
	mustSetEntry(t, f.plan, owner, day1, "breakfast", bowl.ID, 1)
	mustSetEntry(t, f.plan, owner, day2, "breakfast", bowl.ID, 1)
	mustSetEntry(t, f.plan, owner, day2, "lunch", pancakes.ID, 2)
	// Outside the range: must not be counted.
	mustSetEntry(t, f.plan, owner, day2.AddDate(0, 0, 1), "dinner", pancakes.ID, 1)

	list, created, err := f.lists.Generate(ctx, owner, service.GenerateShoppingListInput{From: day1, To: day2})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !created {
		t.Error("created = false, want true (no list_id)")
	}
	if list.SourceFrom == nil || !list.SourceFrom.Equal(day1) || list.SourceTo == nil || !list.SourceTo.Equal(day2) {
		t.Errorf("source range = %v..%v, want %v..%v", list.SourceFrom, list.SourceTo, day1, day2)
	}

	// Bowl (2 servings) twice at portion 1: half the recipe each time, so
	// the whole recipe once: rice 200 g, oil 10 ml, egg 2 piece.
	// Pancakes (1 serving) once at portion 2: flour 200 g + 2 piece, oil 10 g.
	// Egg: one unit only, kept as piece. Oil: ml and g mixed, both
	// convertible, merged into grams: 10 ml * 0.92 + 10 g = 19.2 g. Flour: g
	// and an unconvertible piece, two lines. Ordered by category, name, unit.
	want := []struct {
		name, category, unit string
		quantity             float64
	}{
		{"Olive Oil", "condiments_oils", "g", 19.2},
		{"Egg", "dairy_eggs", "piece", 2},
		{"Flour", "grains_bread", "g", 200},
		{"Flour", "grains_bread", "piece", 2},
		{"Rice", "grains_bread", "g", 200},
	}
	if len(list.Items) != len(want) {
		t.Fatalf("items = %+v, want %d lines", list.Items, len(want))
	}
	for i, w := range want {
		got := list.Items[i]
		if got.Name != w.name || got.Category != w.category || got.Unit == nil || *got.Unit != w.unit ||
			got.Quantity == nil || !almostEqual(*got.Quantity, w.quantity) {
			t.Errorf("item %d = %s %s %v %v, want %s %s %v %s", i, got.Name, got.Category, deref(got.Quantity), deref(got.Unit), w.name, w.category, w.quantity, w.unit)
		}
		if got.Origin != "generated" || got.IngredientID == nil || got.Position != i || got.Version != 1 || got.Checked {
			t.Errorf("item %d = %+v, want a fresh generated item at position %d with its ingredient id", i, got, i)
		}
	}
}

func TestShoppingListsRegenerateReplacesGeneratedItemsAndKeepsManualOnes(t *testing.T) {
	f := newShoppingListsFixture(t)
	ctx := context.Background()
	owner := newTestUser(t, f.st, "shopper2@example.com")

	rice := mustCreateIngredient(t, f.ing, owner, service.CreateIngredientInput{Name: "Rice", Category: "grains_bread"})
	meal, err := f.meals.Create(ctx, owner, service.CreateMealInput{Name: "Rice", Servings: 1})
	if err != nil {
		t.Fatalf("create meal: %v", err)
	}
	if _, err := f.meals.ReplaceIngredients(ctx, owner, meal.ID, []service.MealIngredientInput{{IngredientID: rice.ID, Quantity: 100, Unit: "g"}}); err != nil {
		t.Fatalf("ReplaceIngredients: %v", err)
	}
	day := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	mustSetEntry(t, f.plan, owner, day, "lunch", meal.ID, 1)

	list, _, err := f.lists.Generate(ctx, owner, service.GenerateShoppingListInput{From: day, To: day, Name: ptr("Week 23")})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if list.Name != "Week 23" || len(list.Items) != 1 {
		t.Fatalf("list = %+v, want Week 23 with one generated item", list)
	}
	oldGenerated := list.Items[0]
	manual, err := f.lists.AddItem(ctx, owner, list.ID, service.CreateShoppingItemInput{Name: "Paper towels"})
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if _, err := f.lists.UpdateItem(ctx, owner, list.ID, manual.ID, service.UpdateShoppingItemInput{Checked: ptr(true)}); err != nil {
		t.Fatalf("check manual item: %v", err)
	}

	mustSetEntry(t, f.plan, owner, day, "lunch", meal.ID, 3)
	sub, err := f.lists.Subscribe(ctx, owner, list.ID)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer sub.Close()

	regenerated, created, err := f.lists.Generate(ctx, owner, service.GenerateShoppingListInput{From: day, To: day, ListID: &list.ID})
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if created {
		t.Error("created = true, want false (list_id given)")
	}
	if len(regenerated.Items) != 2 {
		t.Fatalf("items after regenerate = %+v, want the manual item plus one generated item", regenerated.Items)
	}
	kept, fresh := regenerated.Items[0], regenerated.Items[1]
	if kept.ID != manual.ID || !kept.Checked || kept.Origin != "manual" {
		t.Errorf("first item = %+v, want the manual item, untouched and still checked", kept)
	}
	if fresh.ID == oldGenerated.ID || fresh.Origin != "generated" || *fresh.Quantity != 300 || fresh.Position <= kept.Position {
		t.Errorf("second item = %+v, want a new generated rice line of 300 g after the manual item", fresh)
	}
	if ev := nextEvent(t, sub); ev.Type != service.ListEventListChanged || ev.ListID != list.ID {
		t.Errorf("event = %+v, want list_changed for the list", ev)
	}
}

func TestShoppingListsGenerateValidatesTheRangeAndTheListsOwner(t *testing.T) {
	f := newShoppingListsFixture(t)
	ctx := context.Background()
	owner := newTestUser(t, f.st, "shopper7@example.com")
	other := newTestUser(t, f.st, "other7@example.com")

	list, err := f.lists.Create(ctx, owner, service.CreateShoppingListInput{Name: "Mine"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	day := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if _, _, err := f.lists.Generate(ctx, other, service.GenerateShoppingListInput{From: day, To: day, ListID: &list.ID}); !errors.Is(err, service.ErrShoppingListNotFound) {
		t.Errorf("regenerate another user's list: err = %v, want ErrShoppingListNotFound", err)
	}
	if _, _, err := f.lists.Generate(ctx, owner, service.GenerateShoppingListInput{From: day, To: day.AddDate(0, 0, -1)}); !errors.Is(err, service.ErrPlanRangeInvalid) {
		t.Errorf("to before from: err = %v, want ErrPlanRangeInvalid", err)
	}
	if _, _, err := f.lists.Generate(ctx, owner, service.GenerateShoppingListInput{From: day, To: day.AddDate(0, 0, 92)}); !errors.Is(err, service.ErrPlanRangeTooLong) {
		t.Errorf("93-day range: err = %v, want ErrPlanRangeTooLong", err)
	}
	empty, created, err := f.lists.Generate(ctx, owner, service.GenerateShoppingListInput{From: day, To: day.AddDate(0, 0, 91)})
	if err != nil || !created || len(empty.Items) != 0 || empty.Name != "Shopping 2026-06-01 to 2026-08-31" {
		t.Errorf("92-day range with no plan entries = %+v, %v, %v; want a new empty list with the default name", empty, created, err)
	}
}
```

`almostEqual` is the existing tolerance helper in `meals_test.go`. It is needed because `10 * 0.92` is not exact in binary floating point, which is the same lesson the meals golden tests learned.

- [x] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/service/... -run TestShoppingLists -v`
Expected: FAIL to compile, because `f.lists.Generate` and `service.GenerateShoppingListInput` do not exist yet.

- [x] **Step 3: Implement generation**

In `backend/internal/service/shopping_lists.go`, add `"sort"` to the imports. After `UpdateShoppingListInput`, add the input type:

```go
// GenerateShoppingListInput selects the plan range to shop for. With ListID
// nil a new list is created (named Name, or a default); with ListID set,
// that list's generated items are replaced and Name is ignored.
type GenerateShoppingListInput struct {
	From   time.Time
	To     time.Time
	Name   *string
	ListID *uuid.UUID
}
```

After `Delete`, add `Generate`:

```go
// Generate builds generated items from the caller's plan entries in [From,
// To] inclusive (at most maxPlanRangeDays days, like GET /plan). Without
// ListID it creates a new list and reports created = true. With ListID it
// replaces that list's generated items, keeps its manual items untouched,
// records the new source range, and tells the list's event streams.
func (s *ShoppingLists) Generate(ctx context.Context, ownerID uuid.UUID, in GenerateShoppingListInput) (list ShoppingList, created bool, err error) {
	if in.To.Before(in.From) {
		return ShoppingList{}, false, ErrPlanRangeInvalid
	}
	if diffDays := int(in.To.Sub(in.From).Hours() / 24); diffDays >= maxPlanRangeDays {
		return ShoppingList{}, false, ErrPlanRangeTooLong
	}
	created = in.ListID == nil

	err = s.st.InTx(ctx, func(q *sqlc.Queries) error {
		var row sqlc.ShoppingList
		if created {
			name := fmt.Sprintf("Shopping %s to %s", in.From.Format(time.DateOnly), in.To.Format(time.DateOnly))
			if in.Name != nil {
				name = *in.Name
			}
			var err error
			row, err = q.CreateShoppingList(ctx, sqlc.CreateShoppingListParams{
				OwnerID: ownerID, Name: name, SourceFrom: toPgDate(in.From), SourceTo: toPgDate(in.To),
			})
			if store.IsForeignKeyViolation(err, "shopping_lists_owner_id_fkey") {
				return ErrNotFound
			}
			if err != nil {
				return fmt.Errorf("create shopping list: %w", err)
			}
		} else {
			// SetShoppingListSourceForUser is an UPDATE, so besides recording
			// the new range it takes the list row's write lock before the
			// DELETE+INSERT below: two concurrent regenerations of one list
			// serialize instead of both inserting a full generated set (the
			// second one's DELETE cannot see rows the first one inserts).
			// Same fix as TouchMealForUser / TouchDietTemplateForUser.
			var err error
			row, err = q.SetShoppingListSourceForUser(ctx, sqlc.SetShoppingListSourceForUserParams{
				ID: *in.ListID, UserID: ownerID, SourceFrom: toPgDate(in.From), SourceTo: toPgDate(in.To),
			})
			if store.IsNotFound(err) {
				return ErrShoppingListNotFound
			}
			if err != nil {
				return fmt.Errorf("set shopping list source: %w", err)
			}
			if err := q.DeleteGeneratedShoppingItems(ctx, row.ID); err != nil {
				return fmt.Errorf("clear generated items: %w", err)
			}
		}

		lines, err := generateLines(ctx, q, ownerID, in.From, in.To)
		if err != nil {
			return err
		}
		// Generated items go after whatever manual items the list keeps.
		base, err := q.NextShoppingItemPosition(ctx, row.ID)
		if err != nil {
			return fmt.Errorf("next item position: %w", err)
		}
		for i, l := range lines {
			ingredientID, quantity, unit := l.IngredientID, l.Quantity, l.Unit
			if _, err := q.InsertShoppingItem(ctx, sqlc.InsertShoppingItemParams{
				ListID: row.ID, IngredientID: &ingredientID, Name: l.Name, Quantity: &quantity, Unit: &unit,
				Category: l.Category, Position: base + toInt32(i), Origin: "generated",
			}); err != nil {
				return fmt.Errorf("insert generated item: %w", err)
			}
		}
		items, err := q.GetShoppingItems(ctx, row.ID)
		if err != nil {
			return fmt.Errorf("get shopping items: %w", err)
		}
		list = toShoppingList(row, items)
		return nil
	})
	if err != nil {
		return ShoppingList{}, false, err
	}
	if !created {
		s.events.Publish(ListEvent{Type: ListEventListChanged, ListID: list.ID})
	}
	return list, created, nil
}
```

After `publishItem`, add the generation helpers:

```go
// generatedLine is one item Generate is about to write.
type generatedLine struct {
	IngredientID uuid.UUID
	Name         string
	Category     string
	Quantity     float64
	Unit         string
}

// generateLines sums every ingredient line of every meal scheduled in [from,
// to] (each scaled by the entry's portion over the meal's servings, the same
// per-serving rule Plan uses for nutrition), merges them per ingredient with
// mergeUnits, and orders the result by category, then name, then unit.
func generateLines(ctx context.Context, q *sqlc.Queries, ownerID uuid.UUID, from, to time.Time) ([]generatedLine, error) {
	entries, err := q.GetPlanEntriesForUserInRange(ctx, sqlc.GetPlanEntriesForUserInRangeParams{
		UserID: ownerID, FromDate: toPgDate(from), ToDate: toPgDate(to),
	})
	if err != nil {
		return nil, fmt.Errorf("get plan entries: %w", err)
	}
	if len(entries) == 0 {
		return nil, nil
	}

	mealIDs := uniqueUUIDs(entries, func(e sqlc.PlanEntry) uuid.UUID { return e.MealID })
	mealRows, err := q.GetMealsForUser(ctx, sqlc.GetMealsForUserParams{Ids: mealIDs, UserID: ownerID})
	if err != nil {
		return nil, fmt.Errorf("get meals: %w", err)
	}
	servings := make(map[uuid.UUID]float64, len(mealRows))
	for _, m := range mealRows {
		servings[m.ID] = m.Servings
	}
	lineRows, err := q.GetMealIngredientsForMeals(ctx, mealIDs)
	if err != nil {
		return nil, fmt.Errorf("get meal ingredients: %w", err)
	}
	linesByMeal := make(map[uuid.UUID][]sqlc.MealIngredient, len(mealIDs))
	for _, l := range lineRows {
		linesByMeal[l.MealID] = append(linesByMeal[l.MealID], l)
	}
	ingredientIDs := uniqueUUIDs(lineRows, func(l sqlc.MealIngredient) uuid.UUID { return l.IngredientID })
	if len(ingredientIDs) == 0 {
		return nil, nil
	}
	ingredientRows, err := q.GetIngredientsForUser(ctx, sqlc.GetIngredientsForUserParams{Ids: ingredientIDs, UserID: &ownerID})
	if err != nil {
		return nil, fmt.Errorf("get ingredients: %w", err)
	}
	ingredients := make(map[uuid.UUID]sqlc.Ingredient, len(ingredientRows))
	for _, ing := range ingredientRows {
		ingredients[ing.ID] = ing
	}

	// sums[ingredient][unit] is the total quantity needed in that unit.
	sums := make(map[uuid.UUID]map[string]float64, len(ingredientIDs))
	for _, e := range entries {
		mealServings, ok := servings[e.MealID]
		if !ok {
			// plan_entries.meal_id is a NO ACTION foreign key and plan
			// entries are owner-only, so every entry's meal exists and is the
			// caller's own; this is a defensive check, not a reachable path.
			return nil, fmt.Errorf("meal %s of a plan entry is not visible to its owner", e.MealID)
		}
		for _, l := range linesByMeal[e.MealID] {
			if sums[l.IngredientID] == nil {
				sums[l.IngredientID] = make(map[string]float64, 1)
			}
			sums[l.IngredientID][l.Unit] += l.Quantity * e.Portion / mealServings
		}
	}

	var lines []generatedLine
	for ingredientID, byUnit := range sums {
		ing, ok := ingredients[ingredientID]
		if !ok {
			// Unreachable for the same reason as ErrMealIngredientNotFound
			// in meals.go: an ingredient a meal line references cannot be
			// deleted, and a custom ingredient never changes owner.
			return nil, ErrMealIngredientNotFound
		}
		merged, err := mergeUnits(ing, byUnit)
		if err != nil {
			return nil, err
		}
		lines = append(lines, merged...)
	}
	sort.Slice(lines, func(i, j int) bool {
		a, b := lines[i], lines[j]
		if a.Category != b.Category {
			return a.Category < b.Category
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Unit < b.Unit
	})
	return lines, nil
}

// mergeUnits turns one ingredient's per-unit totals into shopping lines.
// When every line used the same unit, that unit is kept as is (six eggs stay
// "6 piece", not "300 g"). When units are mixed, every total that can be
// converted to grams (with gramsFor, the same conversion meal nutrition
// uses) is merged into one "g" line, and every total that cannot (the
// ingredient lacks grams_per_piece or density_g_per_ml) stays a line of its
// own in its own unit rather than being merged incorrectly.
func mergeUnits(ing sqlc.Ingredient, byUnit map[string]float64) ([]generatedLine, error) {
	line := func(unit string, quantity float64) generatedLine {
		return generatedLine{IngredientID: ing.ID, Name: ing.Name, Category: ing.Category, Quantity: quantity, Unit: unit}
	}
	if len(byUnit) == 1 {
		for unit, quantity := range byUnit {
			return []generatedLine{line(unit, quantity)}, nil
		}
	}
	var (
		out      []generatedLine
		grams    float64
		hasGrams bool
	)
	// A fixed unit order keeps the floating-point sum deterministic.
	for _, unit := range []string{"g", "ml", "piece"} {
		quantity, ok := byUnit[unit]
		if !ok {
			continue
		}
		g, err := gramsFor(quantity, unit, ing.GramsPerPiece, ing.DensityGPerMl)
		if errors.Is(err, ErrUnitNotConvertible) {
			out = append(out, line(unit, quantity))
			continue
		}
		if err != nil {
			return nil, err
		}
		grams += g
		hasGrams = true
	}
	if hasGrams {
		out = append(out, line("g", grams))
	}
	return out, nil
}
```

`gramsFor` is linear in `quantity`, so converting each per-unit total once gives the same result as converting every line and then summing.

- [x] **Step 4: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/service/... -run 'TestShoppingLists|TestMeals|TestPlan' -v`
Expected: PASS, including the three new generation tests. The meals and plan tests are unchanged, and running them proves nothing they share (`gramsFor`, `uniqueUUIDs`, the plan queries) regressed.

Run: `cd backend && go build ./...`
Expected: builds cleanly.

- [x] **Step 5: Commit**

```bash
git add backend/internal/service/shopping_lists.go backend/internal/service/shopping_lists_test.go
git commit -m "feat(backend): generate shopping lists from a plan date range"
```

---

### Task 7: OpenAPI contract and handlers: lists and items

The contract and the handlers are one task (see Global Constraints). `cmd/api` is wired in this task too, because `NewRouter` panics without `Deps.ShoppingLists`, and the existing `cmd/api` tests would fail the moment the router requires it.

**Files:**
- Modify: `openapi.yaml`
- Modify (generated, commit the output): `backend/internal/api/api.gen.go`
- Create: `backend/internal/httpapi/shopping_lists.go`
- Modify: `backend/internal/httpapi/problem.go`, `account.go`, `server.go`, `router.go`
- Modify: `backend/internal/httpapi/contract_test.go`
- Create: `backend/internal/httpapi/shopping_lists_flow_test.go`
- Modify: `backend/cmd/api/main.go`

**Interfaces:**
- Consumes: everything `ShoppingLists` exports from Tasks 5 and 6; the existing `requireUser`, `decodeJSON`, `writeJSON`, `toNullable`, `toOptional` helpers (`account.go`) and `stubTwoUserTokens`, `contract`, `decodeAs`, `problemCode`, `withBody`, `withBearer` (test helpers in `httpapi_test`).
- Produces:
  - `httpapi.ShoppingListsService` (with `Subscribe` added in Task 8), `httpapi.Deps.ShoppingLists`, `httpapi.CodeVersionConflict = "version_conflict"`, `httpapi.CodeVersionRequired = "version_required"`.
  - The generated types, scratch-verified with oapi-codegen v2.8.0:
    - `api.ShoppingItem{Category api.IngredientCategory; Checked bool; CheckedBy nullable.Nullable[openapi_types.UUID]; CreatedAt time.Time; Id openapi_types.UUID; IngredientId nullable.Nullable[openapi_types.UUID]; ListId openapi_types.UUID; Name string; Origin api.ShoppingItemOrigin; Position int; Quantity nullable.Nullable[float64]; Unit nullable.Nullable[api.ShoppingItemUnit]; UpdatedAt time.Time; Version int}`
    - `api.ShoppingList{...; Items []api.ShoppingItem; SourceFrom, SourceTo nullable.Nullable[openapi_types.Date]; ...}`
    - `api.ShoppingListSummary` and `api.ShoppingListPage{Items []api.ShoppingListSummary; NextCursor nullable.Nullable[string]}`
    - `api.CreateShoppingListRequest{Name string; SharedWithPartner *bool}` and `api.UpdateShoppingListRequest{Name *string; SharedWithPartner *bool}`
    - `api.GenerateShoppingListRequest{From, To openapi_types.Date; ListId *openapi_types.UUID; Name *string}`
    - `api.CreateShoppingItemRequest{Category *api.IngredientCategory; IngredientId *openapi_types.UUID; Name string; Quantity *float64; Unit *api.Unit}`
    - `api.UpdateShoppingItemRequest{Category *api.IngredientCategory; Checked *bool; Name *string; Quantity nullable.Nullable[float64]; Unit nullable.Nullable[api.UpdateShoppingItemRequestUnit]; Version *int}`
    - `api.ShoppingItemConflict{Code string; Current api.ShoppingItem; Detail *string; Status int; Title string; Type string}`
    - `api.ListShoppingListsParams{Cursor *string; Limit *int}`
    - These `ServerInterface` methods: `ListShoppingLists(w, r, params api.ListShoppingListsParams)`, `CreateShoppingList(w, r)`, `GenerateShoppingList(w, r)`, `GetShoppingList(w, r, id openapi_types.UUID)`, `UpdateShoppingList(w, r, id openapi_types.UUID)`, `DeleteShoppingList(w, r, id openapi_types.UUID)`, `CreateShoppingItem(w, r, id openapi_types.UUID)`, `UpdateShoppingItem(w, r, id openapi_types.UUID, itemId openapi_types.UUID)`, `DeleteShoppingItem(w, r, id openapi_types.UUID, itemId openapi_types.UUID)`.

  The `item_id` path parameter becomes the Go parameter `itemId`. The item and request `unit` fields are inline nullable enums, because a `$ref` cannot carry `nullable: true` in 3.0.3. They therefore generate their own `ShoppingItemUnit` and `UpdateShoppingItemRequestUnit` types, while `CreateShoppingItemRequest.unit`, which is not nullable, reuses the existing `Unit`. `openapi_types.UUID` is an alias of `uuid.UUID`, so handlers declare `id uuid.UUID` exactly like `diet_templates.go` does.

- [x] **Step 1: Add the tag**

In `openapi.yaml`, after the `Plan` tag, add:

```yaml
  - name: ShoppingLists
    description: Shopping lists generated from the plan or built by hand, with live updates over Server-Sent Events.
```

- [x] **Step 2: Add the schemas**

In `openapi.yaml`'s `components.schemas`, after `SetPlanEntryRequest`, add:

```yaml
    ShoppingItemOrigin:
      type: string
      enum: [generated, manual]
    ShoppingItem:
      type: object
      required: [id, list_id, ingredient_id, name, quantity, unit, category, checked, checked_by, position, version, origin, created_at, updated_at]
      properties:
        id:
          type: string
          format: uuid
        list_id:
          type: string
          format: uuid
        ingredient_id:
          type: string
          format: uuid
          nullable: true
          description: The ingredient this item came from. Null for a free-text item, and for any item whose ingredient has since been deleted; the item keeps its own name, quantity, unit and category either way.
        name:
          type: string
        quantity:
          type: number
          format: double
          nullable: true
        unit:
          type: string
          enum: [g, ml, piece]
          nullable: true
        category:
          $ref: '#/components/schemas/IngredientCategory'
        checked:
          type: boolean
        checked_by:
          type: string
          format: uuid
          nullable: true
        position:
          type: integer
        version:
          type: integer
        origin:
          $ref: '#/components/schemas/ShoppingItemOrigin'
        created_at:
          type: string
          format: date-time
        updated_at:
          type: string
          format: date-time
    ShoppingList:
      type: object
      required: [id, name, shared_with_partner, source_from, source_to, items, created_at, updated_at]
      properties:
        id:
          type: string
          format: uuid
        name:
          type: string
        shared_with_partner:
          type: boolean
        source_from:
          type: string
          format: date
          nullable: true
        source_to:
          type: string
          format: date
          nullable: true
        items:
          type: array
          items:
            $ref: '#/components/schemas/ShoppingItem'
        created_at:
          type: string
          format: date-time
        updated_at:
          type: string
          format: date-time
    ShoppingListSummary:
      type: object
      required: [id, name, shared_with_partner, source_from, source_to, created_at, updated_at]
      properties:
        id:
          type: string
          format: uuid
        name:
          type: string
        shared_with_partner:
          type: boolean
        source_from:
          type: string
          format: date
          nullable: true
        source_to:
          type: string
          format: date
          nullable: true
        created_at:
          type: string
          format: date-time
        updated_at:
          type: string
          format: date-time
    ShoppingListPage:
      type: object
      required: [items, next_cursor]
      properties:
        items:
          type: array
          items:
            $ref: '#/components/schemas/ShoppingListSummary'
        next_cursor:
          type: string
          nullable: true
    CreateShoppingListRequest:
      type: object
      additionalProperties: false
      required: [name]
      properties:
        name:
          type: string
          minLength: 1
          maxLength: 200
        shared_with_partner:
          type: boolean
    UpdateShoppingListRequest:
      type: object
      additionalProperties: false
      properties:
        name:
          type: string
          minLength: 1
          maxLength: 200
        shared_with_partner:
          type: boolean
    GenerateShoppingListRequest:
      type: object
      additionalProperties: false
      required: [from, to]
      properties:
        from:
          type: string
          format: date
        to:
          type: string
          format: date
        name:
          type: string
          minLength: 1
          maxLength: 200
          description: Name for a new list. Ignored when `list_id` is set. Defaults to "Shopping {from} to {to}".
        list_id:
          type: string
          format: uuid
          description: Regenerate this existing list instead of creating a new one.
    CreateShoppingItemRequest:
      type: object
      additionalProperties: false
      required: [name]
      properties:
        ingredient_id:
          type: string
          format: uuid
        name:
          type: string
          minLength: 1
          maxLength: 200
        quantity:
          type: number
          format: double
          exclusiveMinimum: true
          minimum: 0
          maximum: 100000
        unit:
          $ref: '#/components/schemas/Unit'
        category:
          $ref: '#/components/schemas/IngredientCategory'
    UpdateShoppingItemRequest:
      type: object
      additionalProperties: false
      properties:
        version:
          type: integer
          minimum: 1
        name:
          type: string
          minLength: 1
          maxLength: 200
        quantity:
          type: number
          format: double
          exclusiveMinimum: true
          minimum: 0
          maximum: 100000
          nullable: true
        unit:
          type: string
          enum: [g, ml, piece]
          nullable: true
        category:
          $ref: '#/components/schemas/IngredientCategory'
        checked:
          type: boolean
    ShoppingItemConflict:
      type: object
      description: A `version_conflict` problem (RFC 9457) with the item's current state as the `current` extension member.
      required: [type, title, status, code, current]
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
        current:
          $ref: '#/components/schemas/ShoppingItem'
```

- [x] **Step 3: Add the list and item paths**

In `openapi.yaml`, immediately before `/healthz:`, add. The event stream path comes in Task 8.

```yaml
  /shopping-lists:
    get:
      tags: [ShoppingLists]
      operationId: listShoppingLists
      summary: List the caller's shopping lists
      description: Cursor-paginated, newest first. Does not include items; fetch a single list for those.
      parameters:
        - name: cursor
          in: query
          schema:
            type: string
        - name: limit
          in: query
          schema:
            type: integer
            minimum: 1
            maximum: 100
            default: 20
      responses:
        '200':
          description: The caller's shopping lists.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ShoppingListPage'
        '400':
          $ref: '#/components/responses/BadRequest'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '429':
          $ref: '#/components/responses/TooManyRequests'
        '500':
          $ref: '#/components/responses/Problem'
    post:
      tags: [ShoppingLists]
      operationId: createShoppingList
      summary: Create an empty shopping list
      description: Creates a list with no items and no source date range. Add items with `POST /shopping-lists/{id}/items`, or use `POST /shopping-lists/generate` to build one from the plan.
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/CreateShoppingListRequest'
      responses:
        '201':
          description: The list was created.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ShoppingList'
        '400':
          $ref: '#/components/responses/BadRequest'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '429':
          $ref: '#/components/responses/TooManyRequests'
        '500':
          $ref: '#/components/responses/Problem'
  /shopping-lists/generate:
    post:
      tags: [ShoppingLists]
      operationId: generateShoppingList
      summary: Generate a shopping list from the plan
      description: Sums the ingredients of every plan entry in [from, to] inclusive (capped at 92 days), merging lines of the same ingredient, and groups them by category. Without `list_id` this creates a new list (201). With `list_id` it regenerates that list (200), replacing every `generated` item, keeping every `manual` item untouched, and updating its source range.
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/GenerateShoppingListRequest'
      responses:
        '200':
          description: The existing list was regenerated.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ShoppingList'
        '201':
          description: A new list was generated.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ShoppingList'
        '400':
          $ref: '#/components/responses/BadRequest'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '404':
          $ref: '#/components/responses/NotFound'
        '429':
          $ref: '#/components/responses/TooManyRequests'
        '500':
          $ref: '#/components/responses/Problem'
  /shopping-lists/{id}:
    parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
          format: uuid
    get:
      tags: [ShoppingLists]
      operationId: getShoppingList
      summary: Get a shopping list
      description: Includes every item, ordered by position.
      responses:
        '200':
          description: The list.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ShoppingList'
        '400':
          $ref: '#/components/responses/BadRequest'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '404':
          $ref: '#/components/responses/NotFound'
        '429':
          $ref: '#/components/responses/TooManyRequests'
        '500':
          $ref: '#/components/responses/Problem'
    patch:
      tags: [ShoppingLists]
      operationId: updateShoppingList
      summary: Update a shopping list
      description: Fields that are absent are left unchanged.
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/UpdateShoppingListRequest'
      responses:
        '200':
          description: The updated list.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ShoppingList'
        '400':
          $ref: '#/components/responses/BadRequest'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '404':
          $ref: '#/components/responses/NotFound'
        '429':
          $ref: '#/components/responses/TooManyRequests'
        '500':
          $ref: '#/components/responses/Problem'
    delete:
      tags: [ShoppingLists]
      operationId: deleteShoppingList
      summary: Delete a shopping list
      description: Deletes the list and all of its items. Open event streams for the list receive a final `list_deleted` event and are closed.
      responses:
        '204':
          description: The list was deleted.
        '400':
          $ref: '#/components/responses/BadRequest'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '404':
          $ref: '#/components/responses/NotFound'
        '429':
          $ref: '#/components/responses/TooManyRequests'
        '500':
          $ref: '#/components/responses/Problem'
  /shopping-lists/{id}/items:
    parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
          format: uuid
    post:
      tags: [ShoppingLists]
      operationId: createShoppingItem
      summary: Add an item to a shopping list
      description: Adds a `manual` item at the end of the list. With `ingredient_id`, the ingredient must be visible to the caller, and `category` defaults to the ingredient's category; without it, `category` defaults to `other`.
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/CreateShoppingItemRequest'
      responses:
        '201':
          description: The item was added.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ShoppingItem'
        '400':
          $ref: '#/components/responses/BadRequest'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '404':
          $ref: '#/components/responses/NotFound'
        '429':
          $ref: '#/components/responses/TooManyRequests'
        '500':
          $ref: '#/components/responses/Problem'
  /shopping-lists/{id}/items/{item_id}:
    parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
          format: uuid
      - name: item_id
        in: path
        required: true
        schema:
          type: string
          format: uuid
    patch:
      tags: [ShoppingLists]
      operationId: updateShoppingItem
      summary: Edit or check off a shopping item
      description: Fields that are absent are left unchanged. Changing `name`, `quantity`, `unit` or `category` requires `version`, which must equal the item's current version, or the request fails with `409 version_conflict` carrying the current item in `current`. A request that changes only `checked` is last-write-wins and does not need `version`; setting `checked` to the value it already has is a no-op that returns the item unchanged. Every change increments `version`.
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/UpdateShoppingItemRequest'
      responses:
        '200':
          description: The item, with its new version.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/ShoppingItem'
        '400':
          $ref: '#/components/responses/BadRequest'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '404':
          $ref: '#/components/responses/NotFound'
        '409':
          description: The request's `version` is stale. `current` is the item as it is now.
          content:
            application/problem+json:
              schema:
                $ref: '#/components/schemas/ShoppingItemConflict'
        '429':
          $ref: '#/components/responses/TooManyRequests'
        '500':
          $ref: '#/components/responses/Problem'
    delete:
      tags: [ShoppingLists]
      operationId: deleteShoppingItem
      summary: Remove an item from a shopping list
      description: Removes the item regardless of its version (removes merge, per the sync rules).
      responses:
        '204':
          description: The item was removed.
        '400':
          $ref: '#/components/responses/BadRequest'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '404':
          $ref: '#/components/responses/NotFound'
        '429':
          $ref: '#/components/responses/TooManyRequests'
        '500':
          $ref: '#/components/responses/Problem'
```

One YAML trap was hit during scratch verification: a plain (unquoted) description must not contain `": "`. An earlier draft of the `generate` description ("(200: every ...") failed to parse, which is why it now reads "(200), replacing ...". `/shopping-lists/generate` and `/shopping-lists/{id}` coexist without ambiguity. chi prefers the static segment, and kin-openapi's router orders paths by specificity. There is also no `POST /shopping-lists/{id}` for `generate` to collide with.

Run: `make lint-api`. Expected: valid, `2 problems are explicitly ignored`, and no warnings.
Run: `make generate`. This regenerates `backend/internal/api/api.gen.go`.

- [x] **Step 4: Add the problem codes and `writeServiceError` cases**

In `backend/internal/httpapi/problem.go`, add to the `Code*` constants:

```go
	CodeVersionConflict     = "version_conflict"
	CodeVersionRequired     = "version_required"
```

In `backend/internal/httpapi/account.go`, in `writeServiceError`, add after the `ErrPlanRangeInvalid` case:

```go
	case errors.Is(err, service.ErrShoppingListNotFound):
		WriteProblem(w, http.StatusNotFound, CodeNotFound, "")
	case errors.Is(err, service.ErrShoppingItemNotFound):
		WriteProblem(w, http.StatusNotFound, CodeNotFound, "")
	case errors.Is(err, service.ErrShoppingItemIngredientNotFound):
		WriteProblem(w, http.StatusBadRequest, CodeInvalidIngredient, "")
	case errors.Is(err, service.ErrShoppingItemVersionRequired):
		WriteProblem(w, http.StatusBadRequest, CodeVersionRequired, "version is required to change name, quantity, unit or category")
```

`ShoppingItemVersionConflictError` gets no case here, because its body is not a plain problem. `UpdateShoppingItem` matches it with `errors.As` before falling back to `writeServiceError` (Step 5).

- [x] **Step 5: Write the handlers**

Create `backend/internal/httpapi/shopping_lists.go`:

```go
package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/InzKazik/mealplanner/backend/internal/api"
	"github.com/InzKazik/mealplanner/backend/internal/service"
)

// ShoppingListsService is what the shopping-list handlers need from the
// shopping lists service.
type ShoppingListsService interface {
	Create(ctx context.Context, ownerID uuid.UUID, in service.CreateShoppingListInput) (service.ShoppingList, error)
	Get(ctx context.Context, ownerID, id uuid.UUID) (service.ShoppingList, error)
	List(ctx context.Context, ownerID uuid.UUID, in service.ListShoppingListsInput) (service.ShoppingListPage, error)
	Update(ctx context.Context, ownerID, id uuid.UUID, in service.UpdateShoppingListInput) (service.ShoppingList, error)
	Delete(ctx context.Context, ownerID, id uuid.UUID) error
	Generate(ctx context.Context, ownerID uuid.UUID, in service.GenerateShoppingListInput) (service.ShoppingList, bool, error)
	AddItem(ctx context.Context, ownerID, listID uuid.UUID, in service.CreateShoppingItemInput) (service.ShoppingItem, error)
	UpdateItem(ctx context.Context, ownerID, listID, itemID uuid.UUID, in service.UpdateShoppingItemInput) (service.ShoppingItem, error)
	DeleteItem(ctx context.Context, ownerID, listID, itemID uuid.UUID) error
}

const defaultShoppingListLimit = 20

func (s *server) ListShoppingLists(w http.ResponseWriter, r *http.Request, params api.ListShoppingListsParams) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	limit := defaultShoppingListLimit
	if params.Limit != nil {
		limit = *params.Limit
	}
	var cursor *service.ShoppingListCursor
	if params.Cursor != nil {
		c, ok := decodeShoppingListCursor(*params.Cursor)
		if !ok {
			WriteValidationProblem(w, "cursor is invalid", []FieldError{{Field: "cursor", Code: FieldInvalidForm}})
			return
		}
		cursor = &c
	}
	page, err := s.shoppingLists.List(r.Context(), userID, service.ListShoppingListsInput{Cursor: cursor, Limit: limit})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	list := api.ShoppingListPage{Items: make([]api.ShoppingListSummary, len(page.Items)), NextCursor: nullable.NewNullNullable[string]()}
	for i, l := range page.Items {
		list.Items[i] = api.ShoppingListSummary{
			Id: l.ID, Name: l.Name, SharedWithPartner: l.SharedWithPartner,
			SourceFrom: toNullableDate(l.SourceFrom), SourceTo: toNullableDate(l.SourceTo),
			CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt,
		}
	}
	if page.NextCursor != nil {
		list.NextCursor = nullable.NewNullableWithValue(encodeShoppingListCursor(*page.NextCursor))
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *server) CreateShoppingList(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.CreateShoppingListRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in := service.CreateShoppingListInput{Name: req.Name}
	if req.SharedWithPartner != nil {
		in.SharedWithPartner = *req.SharedWithPartner
	}
	list, err := s.shoppingLists.Create(r.Context(), userID, in)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAPIShoppingList(list))
}

func (s *server) GenerateShoppingList(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.GenerateShoppingListRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	list, created, err := s.shoppingLists.Generate(r.Context(), userID, service.GenerateShoppingListInput{
		From: req.From.Time, To: req.To.Time, Name: req.Name, ListID: req.ListId,
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, toAPIShoppingList(list))
}

func (s *server) GetShoppingList(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	list, err := s.shoppingLists.Get(r.Context(), userID, id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIShoppingList(list))
}

func (s *server) UpdateShoppingList(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.UpdateShoppingListRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	list, err := s.shoppingLists.Update(r.Context(), userID, id, service.UpdateShoppingListInput{
		Name: req.Name, SharedWithPartner: req.SharedWithPartner,
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIShoppingList(list))
}

func (s *server) DeleteShoppingList(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	if err := s.shoppingLists.Delete(r.Context(), userID, id); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) CreateShoppingItem(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.CreateShoppingItemRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in := service.CreateShoppingItemInput{IngredientID: req.IngredientId, Name: req.Name, Quantity: req.Quantity}
	if req.Unit != nil {
		unit := string(*req.Unit)
		in.Unit = &unit
	}
	if req.Category != nil {
		category := string(*req.Category)
		in.Category = &category
	}
	item, err := s.shoppingLists.AddItem(r.Context(), userID, id, in)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAPIShoppingItem(item))
}

func (s *server) UpdateShoppingItem(w http.ResponseWriter, r *http.Request, id, itemID uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.UpdateShoppingItemRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in := service.UpdateShoppingItemInput{
		Version: req.Version, Name: req.Name, Quantity: toOptional(req.Quantity), Checked: req.Checked,
	}
	switch {
	case !req.Unit.IsSpecified():
	case req.Unit.IsNull():
		in.Unit = service.Set[string](nil)
	default:
		unit := string(req.Unit.MustGet())
		in.Unit = service.Set(&unit)
	}
	if req.Category != nil {
		category := string(*req.Category)
		in.Category = &category
	}
	item, err := s.shoppingLists.UpdateItem(r.Context(), userID, id, itemID, in)
	var conflict *service.ShoppingItemVersionConflictError
	if errors.As(err, &conflict) {
		writeVersionConflict(w, conflict.Current)
		return
	}
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIShoppingItem(item))
}

func (s *server) DeleteShoppingItem(w http.ResponseWriter, r *http.Request, id, itemID uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	if err := s.shoppingLists.DeleteItem(r.Context(), userID, id, itemID); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeVersionConflict writes the 409 version_conflict problem with the
// item's current state as the "current" extension member (RFC 9457 allows
// extension members; the shape is ShoppingItemConflict in openapi.yaml).
func writeVersionConflict(w http.ResponseWriter, current service.ShoppingItem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusConflict)
	_ = json.NewEncoder(w).Encode(api.ShoppingItemConflict{
		Type: "urn:mealplanner:problem:" + CodeVersionConflict, Title: http.StatusText(http.StatusConflict),
		Status: http.StatusConflict, Code: CodeVersionConflict, Current: toAPIShoppingItem(current),
	})
}

func toAPIShoppingList(l service.ShoppingList) api.ShoppingList {
	items := make([]api.ShoppingItem, len(l.Items))
	for i, it := range l.Items {
		items[i] = toAPIShoppingItem(it)
	}
	return api.ShoppingList{
		Id: l.ID, Name: l.Name, SharedWithPartner: l.SharedWithPartner,
		SourceFrom: toNullableDate(l.SourceFrom), SourceTo: toNullableDate(l.SourceTo),
		Items: items, CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt,
	}
}

func toAPIShoppingItem(it service.ShoppingItem) api.ShoppingItem {
	unit := nullable.NewNullNullable[api.ShoppingItemUnit]()
	if it.Unit != nil {
		unit = nullable.NewNullableWithValue(api.ShoppingItemUnit(*it.Unit))
	}
	return api.ShoppingItem{
		Id: it.ID, ListId: it.ListID, IngredientId: toNullableUUID(it.IngredientID), Name: it.Name,
		Quantity: toNullable(it.Quantity), Unit: unit, Category: api.IngredientCategory(it.Category),
		Checked: it.Checked, CheckedBy: toNullableUUID(it.CheckedBy), Position: it.Position, Version: it.Version,
		Origin: api.ShoppingItemOrigin(it.Origin), CreatedAt: it.CreatedAt, UpdatedAt: it.UpdatedAt,
	}
}

func toNullableUUID(v *uuid.UUID) nullable.Nullable[openapi_types.UUID] {
	if v == nil {
		return nullable.NewNullNullable[openapi_types.UUID]()
	}
	return nullable.NewNullableWithValue(*v)
}

func toNullableDate(v *time.Time) nullable.Nullable[openapi_types.Date] {
	if v == nil {
		return nullable.NewNullNullable[openapi_types.Date]()
	}
	return nullable.NewNullableWithValue(openapi_types.Date{Time: *v})
}

type shoppingListCursorPayload struct {
	CreatedAt time.Time `json:"c"`
	ID        uuid.UUID `json:"i"`
}

func encodeShoppingListCursor(c service.ShoppingListCursor) string {
	b, _ := json.Marshal(shoppingListCursorPayload{CreatedAt: c.CreatedAt, ID: c.ID})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeShoppingListCursor(s string) (service.ShoppingListCursor, bool) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return service.ShoppingListCursor{}, false
	}
	var p shoppingListCursorPayload
	if err := json.Unmarshal(b, &p); err != nil || p.CreatedAt.IsZero() || p.ID == uuid.Nil {
		return service.ShoppingListCursor{}, false
	}
	return service.ShoppingListCursor{CreatedAt: p.CreatedAt, ID: p.ID}, true
}
```

`time.Time` round-trips through JSON as RFC 3339 with nanoseconds. Postgres `timestamptz` has microsecond precision, so the decoded cursor equals the row's `created_at` exactly, and the `(created_at, id) <` comparison neither skips nor repeats a row.

- [x] **Step 6: Wire the service into `server`, the router and `cmd/api`**

In `backend/internal/httpapi/server.go`, add `shoppingLists ShoppingListsService` as the last field of the `server` struct.

In `backend/internal/httpapi/router.go`, add to `Deps`, after `Plan`:

```go
	// ShoppingLists implements the shopping-list endpoints and their event
	// streams.
	ShoppingLists ShoppingListsService
```

replace the required-dependency guard with:

```go
	if d.Logger == nil || d.Ready == nil || d.Auth == nil || d.Ingredients == nil || d.Meals == nil || d.DietTemplates == nil || d.Plan == nil ||
		d.ShoppingLists == nil || d.Tokens == nil || d.WebOrigin == "" || d.WebOrigin == "*" {
		panic("httpapi: Deps.Logger, Ready, Auth, Ingredients, Meals, DietTemplates, Plan, ShoppingLists and Tokens are required, and WebOrigin must be a single origin (not empty or *)")
	}
```

and replace the `srv := &server{...}` line with:

```go
	srv := &server{
		logger: d.Logger, ready: d.Ready, auth: d.Auth, ingredients: d.Ingredients, meals: d.Meals,
		dietTemplates: d.DietTemplates, plan: d.Plan, shoppingLists: d.ShoppingLists,
	}
```

In `backend/cmd/api/main.go`, after `plan := service.NewPlan(st, meals)`, add:

```go
	listEvents := service.NewListEventHub()
	shoppingLists := service.NewShoppingLists(st, listEvents)
```

and add `ShoppingLists:  shoppingLists,` to the `httpapi.Deps{...}` literal after `Plan: plan,`. Task 9 adds the shutdown hook on `listEvents`.

- [x] **Step 7: Add the stub and extend the router test**

In `backend/internal/httpapi/contract_test.go`, add after `stubPlan`:

```go
// stubShoppingLists panics on any call, so tests that must not reach the
// shopping lists service fail loudly if they do.
type stubShoppingLists struct{ httpapi.ShoppingListsService }
```

Add `ShoppingLists: stubShoppingLists{},` to `newTestRouter`'s `httpapi.Deps{...}` literal (after `Plan: stubPlan{},`). In `TestNewRouterPanicsWithoutRequiredDependencies`, add `ShoppingLists: stubShoppingLists{},` to the `full` literal and this case to `tests`:

```go
		"no shopping lists":   func(d *httpapi.Deps) { d.ShoppingLists = nil },
```

- [x] **Step 8: Write the end-to-end test**

Create `backend/internal/httpapi/shopping_lists_flow_test.go`. The router helper returns the service and the hub as well as the router, because Task 8's stream tests act on a list while a stream is open:

```go
package httpapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/api"
	"github.com/InzKazik/mealplanner/backend/internal/db"
	"github.com/InzKazik/mealplanner/backend/internal/httpapi"
	"github.com/InzKazik/mealplanner/backend/internal/service"
	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"
	"github.com/InzKazik/mealplanner/backend/internal/testutil"
)

// shoppingEnv is a router over the real services and Postgres, plus direct
// handles on the shopping lists service and its event hub for tests that
// need to act while a stream is open.
type shoppingEnv struct {
	router http.Handler
	lists  *service.ShoppingLists
	events *service.ListEventHub
	user1  uuid.UUID
}

func newShoppingListsRouter(t *testing.T) shoppingEnv {
	t.Helper()
	pool, err := db.Connect(context.Background(), testutil.NewMigratedDatabase(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	st := store.New(pool)

	hash := "hash"
	u1, err := st.CreateUser(context.Background(), sqlc.CreateUserParams{Email: "a@example.com", PasswordHash: &hash, DisplayName: "A"})
	if err != nil {
		t.Fatalf("create user 1: %v", err)
	}
	u2, err := st.CreateUser(context.Background(), sqlc.CreateUserParams{Email: "b@example.com", PasswordHash: &hash, DisplayName: "B"})
	if err != nil {
		t.Fatalf("create user 2: %v", err)
	}

	meals := service.NewMeals(st)
	events := service.NewListEventHub()
	lists := service.NewShoppingLists(st, events)
	router := newTestRouter(t, func(d *httpapi.Deps) {
		d.Ingredients = service.NewIngredients(st)
		d.Meals = meals
		d.Plan = service.NewPlan(st, meals)
		d.ShoppingLists = lists
		d.Tokens = stubTwoUserTokens{u1: u1.ID, u2: u2.ID}
		d.Limits = httpapi.RateLimits{AuthPerMinute: 1000, UserPerMinute: 1000}
	})
	return shoppingEnv{router: router, lists: lists, events: events, user1: u1.ID}
}

func TestShoppingListsLifecycle(t *testing.T) {
	env := newShoppingListsRouter(t)
	router, token1, token2 := env.router, "user1-token", "user2-token"

	rec := contract(t, router, http.MethodPost, "/ingredients", withBearer(token1),
		withBody(`{"name":"Oats","category":"grains_bread","nutrients":{"calories":389}}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create ingredient: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	oats := decodeAs[api.Ingredient](t, rec)
	rec = contract(t, router, http.MethodPost, "/meals", withBearer(token1), withBody(`{"name":"Porridge","servings":1}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create meal: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	meal := decodeAs[api.Meal](t, rec)
	rec = contract(t, router, http.MethodPut, "/meals/"+meal.Id.String()+"/ingredients", withBearer(token1),
		withBody(`{"items":[{"ingredient_id":"`+oats.Id.String()+`","quantity":80,"unit":"g"}]}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("replace meal ingredients: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	for _, date := range []string{"2026-06-01", "2026-06-02"} {
		rec = contract(t, router, http.MethodPut, "/plan/"+date+"/breakfast", withBearer(token1), withBody(`{"meal_id":"`+meal.Id.String()+`"}`))
		if rec.Code != http.StatusOK {
			t.Fatalf("set plan entry %s: status = %d, body = %s", date, rec.Code, rec.Body.String())
		}
	}

	rec = contract(t, router, http.MethodPost, "/shopping-lists/generate", withBearer(token1),
		withBody(`{"from":"2026-06-01","to":"2026-06-02","name":"Week 23"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("generate: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	list := decodeAs[api.ShoppingList](t, rec)
	if len(list.Items) != 1 || list.Items[0].Quantity.MustGet() != 160 || list.Items[0].Origin != api.ShoppingItemOriginGenerated {
		t.Fatalf("generated items = %+v, want one generated oats line of 160 g", list.Items)
	}
	listPath := "/shopping-lists/" + list.Id.String()

	rec = contract(t, router, http.MethodGet, listPath, withBearer(token2))
	if rec.Code != http.StatusNotFound {
		t.Errorf("get another user's list: status = %d, want 404", rec.Code)
	}

	rec = contract(t, router, http.MethodPost, listPath+"/items", withBearer(token1), withBody(`{"name":"Coffee","quantity":1,"unit":"piece","category":"beverages"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("add item: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	coffee := decodeAs[api.ShoppingItem](t, rec)
	itemPath := listPath + "/items/" + coffee.Id.String()

	rec = contract(t, router, http.MethodPatch, itemPath, withBearer(token1), withBody(`{"checked":true}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("check: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if checked := decodeAs[api.ShoppingItem](t, rec); !checked.Checked || checked.Version != 2 {
		t.Errorf("after check = %+v, want checked at version 2", checked)
	}

	rec = contract(t, router, http.MethodPatch, itemPath, withBearer(token1), withBody(`{"version":1,"name":"Decaf"}`))
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale edit: status = %d, want 409, body = %s", rec.Code, rec.Body.String())
	}
	conflict := decodeAs[api.ShoppingItemConflict](t, rec)
	if conflict.Code != "version_conflict" || conflict.Current.Version != 2 || conflict.Current.Name != "Coffee" {
		t.Errorf("conflict = %+v, want version_conflict carrying the item at version 2", conflict)
	}

	rec = contract(t, router, http.MethodPatch, itemPath, withBearer(token1), withBody(`{"name":"Decaf"}`))
	if rec.Code != http.StatusBadRequest || problemCode(t, rec) != "version_required" {
		t.Errorf("edit without version: status = %d, body = %s, want 400 version_required", rec.Code, rec.Body.String())
	}

	rec = contract(t, router, http.MethodPatch, itemPath, withBearer(token1), withBody(`{"version":2,"name":"Decaf","quantity":null,"unit":null}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("edit at the current version: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if edited := decodeAs[api.ShoppingItem](t, rec); edited.Name != "Decaf" || !edited.Quantity.IsNull() || !edited.Unit.IsNull() || edited.Version != 3 {
		t.Errorf("after edit = %+v, want Decaf with quantity and unit cleared at version 3", edited)
	}

	rec = contract(t, router, http.MethodPost, "/shopping-lists/generate", withBearer(token1),
		withBody(`{"from":"2026-06-01","to":"2026-06-01","list_id":"`+list.Id.String()+`"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("regenerate: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	regenerated := decodeAs[api.ShoppingList](t, rec)
	if len(regenerated.Items) != 2 || regenerated.Items[0].Id != coffee.Id || regenerated.Items[1].Quantity.MustGet() != 80 {
		t.Errorf("regenerated items = %+v, want the manual item kept and one 80 g oats line", regenerated.Items)
	}

	rec = contract(t, router, http.MethodDelete, itemPath, withBearer(token1))
	if rec.Code != http.StatusNoContent {
		t.Errorf("delete item: status = %d, want 204", rec.Code)
	}
	rec = contract(t, router, http.MethodDelete, itemPath, withBearer(token1))
	if rec.Code != http.StatusNotFound {
		t.Errorf("delete item again: status = %d, want 404", rec.Code)
	}

	rec = contract(t, router, http.MethodPatch, listPath, withBearer(token1), withBody(`{"name":"Week 23 (final)"}`))
	if rec.Code != http.StatusOK || decodeAs[api.ShoppingList](t, rec).Name != "Week 23 (final)" {
		t.Errorf("rename: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = contract(t, router, http.MethodPost, "/shopping-lists", withBearer(token1), withBody(`{"name":"Party"}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create list: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var names []string
	path := "/shopping-lists?limit=1"
	for range 5 {
		rec = contract(t, router, http.MethodGet, path, withBearer(token1))
		if rec.Code != http.StatusOK {
			t.Fatalf("list %s: status = %d", path, rec.Code)
		}
		page := decodeAs[api.ShoppingListPage](t, rec)
		for _, l := range page.Items {
			names = append(names, l.Name)
		}
		if page.NextCursor.IsNull() {
			break
		}
		path = "/shopping-lists?limit=1&cursor=" + page.NextCursor.MustGet()
	}
	if len(names) != 2 || names[0] != "Party" || names[1] != "Week 23 (final)" {
		t.Errorf("paginated names = %v, want [Party, Week 23 (final)] (newest first)", names)
	}

	rec = contract(t, router, http.MethodDelete, listPath, withBearer(token1))
	if rec.Code != http.StatusNoContent {
		t.Errorf("delete list: status = %d, want 204", rec.Code)
	}
	rec = contract(t, router, http.MethodGet, listPath, withBearer(token1))
	if rec.Code != http.StatusNotFound {
		t.Errorf("get deleted list: status = %d, want 404", rec.Code)
	}
}
```

- [x] **Step 9: Run the tests**

Run: `cd backend && go build ./... && go vet ./...`
Expected: clean.

Run: `cd backend && go test ./internal/httpapi/... ./cmd/... -v -run 'TestShoppingListsLifecycle|TestNewRouterPanicsWithoutRequiredDependencies|TestServe'`
Expected: PASS, including the new `"no shopping lists"` router case and the existing `cmd/api` tests, which now build the real hub and service.

- [x] **Step 10: Commit**

```bash
git add openapi.yaml backend/internal/api/api.gen.go backend/internal/httpapi/shopping_lists.go \
  backend/internal/httpapi/problem.go backend/internal/httpapi/account.go \
  backend/internal/httpapi/server.go backend/internal/httpapi/router.go \
  backend/internal/httpapi/contract_test.go backend/internal/httpapi/shopping_lists_flow_test.go \
  backend/cmd/api/main.go
git commit -m "feat(api): add the shopping-list and item endpoints and their handlers"
```

---

### Task 8: OpenAPI contract and handler: the event stream, with the write-deadline override

This task closes the first half of the "Carried forward" SSE item: the 30s `WriteTimeout` would otherwise cut every stream.

**Files:**
- Modify: `openapi.yaml`
- Modify (generated, commit the output): `backend/internal/api/api.gen.go`
- Modify: `backend/internal/httpapi/shopping_lists.go`, `account.go`
- Modify: `backend/internal/httpapi/contract_test.go`
- Modify: `backend/internal/httpapi/shopping_lists_flow_test.go`

**Interfaces:**
- Consumes: `ShoppingLists.Subscribe`, `ListSubscription`, `ListEvent`, `ErrEventStreamsClosed` (Tasks 4 and 5); `shoppingEnv`/`newShoppingListsRouter` (Task 7).
- Produces: `ServerInterface.StreamShoppingListEvents(w http.ResponseWriter, r *http.Request, id openapi_types.UUID)` (scratch-verified signature); `ShoppingListsService.Subscribe(ctx, ownerID, listID uuid.UUID) (*service.ListSubscription, error)`.

- [x] **Step 1: Add the events path**

In `openapi.yaml`, after the `/shopping-lists/{id}/items/{item_id}` path and before `/healthz:`, add:

```yaml
  /shopping-lists/{id}/events:
    parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
          format: uuid
    get:
      tags: [ShoppingLists]
      operationId: streamShoppingListEvents
      summary: Stream live changes to a shopping list
      description: |
        A Server-Sent Events stream. Each event's `event:` field is its type and its `data:` field is one JSON object `{"type", "list_id", "item_id"?, "version"?}`. `item_changed` and `item_deleted` carry `item_id` and the item's `version` (for `item_deleted`, the version the item had when it was removed); `list_changed` (renamed or regenerated) and `list_deleted` carry only `type` and `list_id`. After `list_deleted` the server closes the stream. Comment lines (`: keep-alive`) are sent every 25 seconds. The server may also close the stream at any time (shutdown, or a client too slow to keep up); clients reconnect and refetch the list, and refetch whenever they see a version gap. Authenticate with the `Authorization` header like every other route: access tokens are never accepted in the query string.
      responses:
        '200':
          description: The event stream.
          content:
            text/event-stream:
              schema:
                type: string
        '400':
          $ref: '#/components/responses/BadRequest'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '404':
          $ref: '#/components/responses/NotFound'
        '429':
          $ref: '#/components/responses/TooManyRequests'
        '500':
          $ref: '#/components/responses/Problem'
        '503':
          $ref: '#/components/responses/Problem'
```

The description is a `|` block scalar because it contains `": "`. The `503` is the shutdown race (`ErrEventStreamsClosed`).

Run: `make lint-api`. Expected: valid, no warnings.
Run: `make generate`. The build now fails until Step 5 implements `StreamShoppingListEvents`, which is expected.

- [x] **Step 2: Register the test decoder and write the failing tests**

In `backend/internal/httpapi/contract_test.go`, after `stubShoppingLists`, add:

```go
func init() {
	// kin-openapi ships no body decoder for text/event-stream, so validating
	// the events stream's response would fail as an unsupported content type.
	// Its schema is a plain string: the text/plain decoder is exactly right.
	openapi3filter.RegisterBodyDecoder("text/event-stream", openapi3filter.PlainBodyDecoder)
}
```

`openapi3filter` is already imported there.

In `backend/internal/httpapi/shopping_lists_flow_test.go`, add `"bufio"`, `"encoding/json"`, `"net/http/httptest"`, `"strings"` and `"time"` to the imports, then append:

```go
// TestShoppingListEventsThroughTheContract checks the events route's
// declared responses: 401 and 404 like any other secured route, and a 200
// text/event-stream body that ends with list_deleted when the list goes.
func TestShoppingListEventsThroughTheContract(t *testing.T) {
	env := newShoppingListsRouter(t)
	list, err := env.lists.Create(context.Background(), env.user1, service.CreateShoppingListInput{Name: "Groceries"})
	if err != nil {
		t.Fatalf("create list: %v", err)
	}
	eventsPath := "/shopping-lists/" + list.ID.String() + "/events"

	rec := contract(t, env.router, http.MethodGet, eventsPath)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", rec.Code)
	}
	rec = contract(t, env.router, http.MethodGet, eventsPath, withBearer("user2-token"))
	if rec.Code != http.StatusNotFound {
		t.Errorf("another user's list: status = %d, want 404", rec.Code)
	}

	// contract() serves the request synchronously, so end the stream from
	// the side: once the handler has subscribed, delete the list.
	go func() {
		deadline := time.Now().Add(5 * time.Second)
		for env.events.Subscribers(list.ID) == 0 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if err := env.lists.Delete(context.Background(), env.user1, list.ID); err != nil {
			t.Errorf("delete list: %v", err)
		}
	}()
	rec = contract(t, env.router, http.MethodGet, eventsPath, withBearer("user1-token"))
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream: status = %d, content type %q, want 200 text/event-stream", rec.Code, rec.Header().Get("Content-Type"))
	}
	want := ": connected\n\nevent: list_deleted\ndata: {\"type\":\"list_deleted\",\"list_id\":\"" + list.ID.String() + "\"}\n\n"
	if rec.Body.String() != want {
		t.Errorf("stream body = %q, want %q", rec.Body.String(), want)
	}
}

// TestShoppingListEventsOutliveTheServerWriteTimeout runs the router on a
// real server whose WriteTimeout is far shorter than the wait before the
// event, the way cmd/api's 30s WriteTimeout would otherwise cut every stream.
func TestShoppingListEventsOutliveTheServerWriteTimeout(t *testing.T) {
	env := newShoppingListsRouter(t)
	ctx := context.Background()
	list, err := env.lists.Create(ctx, env.user1, service.CreateShoppingListInput{Name: "Groceries"})
	if err != nil {
		t.Fatalf("create list: %v", err)
	}

	srv := httptest.NewUnstartedServer(env.router)
	srv.Config.WriteTimeout = 200 * time.Millisecond
	srv.Start()
	defer srv.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/v1/shopping-lists/"+list.ID.String()+"/events", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer user1-token")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	lines := make(chan string)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()
	next := func() string {
		t.Helper()
		for {
			select {
			case l, ok := <-lines:
				if !ok {
					t.Fatal("stream ended early")
				}
				if l != "" {
					return l
				}
			case <-time.After(3 * time.Second):
				t.Fatal("no line within 3s")
			}
		}
	}

	if l := next(); l != ": connected" {
		t.Fatalf("first line = %q, want \": connected\"", l)
	}
	time.Sleep(600 * time.Millisecond) // three times the server's WriteTimeout

	item, err := env.lists.AddItem(ctx, env.user1, list.ID, service.CreateShoppingItemInput{Name: "Bread"})
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}
	if l := next(); l != "event: item_changed" {
		t.Fatalf("event line = %q, want \"event: item_changed\"", l)
	}
	var data struct {
		Type    string    `json:"type"`
		ListID  uuid.UUID `json:"list_id"`
		ItemID  uuid.UUID `json:"item_id"`
		Version int       `json:"version"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(next(), "data: ")), &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if data.Type != "item_changed" || data.ListID != list.ID || data.ItemID != item.ID || data.Version != 1 {
		t.Errorf("data = %+v, want item_changed for the new item at version 1", data)
	}

	if err := env.lists.Delete(ctx, env.user1, list.ID); err != nil {
		t.Fatalf("delete list: %v", err)
	}
	if l := next(); l != "event: list_deleted" {
		t.Errorf("event line = %q, want \"event: list_deleted\"", l)
	}
}
```

The test deletes the list at the end so the handler returns on its own. `httptest.Server.Close` waits for active handlers, so the test must not leave a stream open when it finishes.

- [x] **Step 3: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/httpapi/... -run TestShoppingListEvents -v`
Expected: FAIL to compile, because `*server` does not implement `api.ServerInterface` (it is missing `StreamShoppingListEvents`).

- [x] **Step 4: Map the shutdown error**

In `backend/internal/httpapi/account.go`, in `writeServiceError`, add after the `ErrShoppingItemVersionRequired` case:

```go
	case errors.Is(err, service.ErrEventStreamsClosed):
		// Only reachable while the server is shutting down.
		WriteProblem(w, http.StatusServiceUnavailable, CodeNotReady, "the server is shutting down")
```

- [x] **Step 5: Write the stream handler**

In `backend/internal/httpapi/shopping_lists.go`:

1. Add `"fmt"`, `"io"` and `"log/slog"` to the imports.
2. Add `Subscribe` as the last method of `ShoppingListsService`:

```go
	Subscribe(ctx context.Context, ownerID, listID uuid.UUID) (*service.ListSubscription, error)
```

3. Replace `const defaultShoppingListLimit = 20` with:

```go
const (
	defaultShoppingListLimit = 20
	// sseKeepAlive is how often an idle event stream gets a comment line, so
	// proxies do not close it as idle and a vanished client is noticed by
	// the failed write.
	sseKeepAlive = 25 * time.Second
)
```

4. After `DeleteShoppingItem`, add:

```go
// StreamShoppingListEvents is an ordinary generated ServerInterface method:
// the handlers are non-strict (w, r), so a long-lived text/event-stream
// response needs no special wiring, and the route goes through the same
// OpenAPI validator (authentication) and per-user rate limit as every other
// operation. It runs until the client disconnects, the list is deleted, the
// subscriber falls too far behind, or the server shuts down (ListEventHub
// closes every subscription from http.Server.RegisterOnShutdown).
func (s *server) StreamShoppingListEvents(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	sub, err := s.shoppingLists.Subscribe(r.Context(), userID, id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	defer sub.Close()

	rc := http.NewResponseController(w)
	// http.Server.WriteTimeout (30s in cmd/api) is an absolute deadline for
	// the whole response, which would cut every stream at 30 seconds. Clear
	// it for this response only. chi's response wrapper implements Unwrap, so
	// the controller reaches the real connection; a writer without deadline
	// support (httptest.ResponseRecorder) reports ErrNotSupported, which is
	// harmless there.
	if err := rc.SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		s.logger.WarnContext(r.Context(), "could not clear the write deadline for an event stream",
			slog.String("request_id", RequestID(r.Context())), slog.Any("err", err))
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if _, err := io.WriteString(w, ": connected\n\n"); err != nil {
		return
	}
	if err := rc.Flush(); err != nil {
		return
	}

	keepAlive := time.NewTicker(sseKeepAlive)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, open := <-sub.Events():
			if !open {
				return
			}
			if err := writeListEvent(w, ev); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		case <-keepAlive.C:
			if _, err := io.WriteString(w, ": keep-alive\n\n"); err != nil {
				return
			}
			if err := rc.Flush(); err != nil {
				return
			}
		}
	}
}

// listEventData is the JSON "data:" of one event. It is not an OpenAPI
// component (an unreferenced component fails the lint's no-unused-components
// rule, and a text/event-stream body cannot $ref it); its shape is
// documented in the events operation's description instead.
type listEventData struct {
	Type    string     `json:"type"`
	ListID  uuid.UUID  `json:"list_id"`
	ItemID  *uuid.UUID `json:"item_id,omitempty"`
	Version *int       `json:"version,omitempty"`
}

func writeListEvent(w io.Writer, ev service.ListEvent) error {
	data, err := json.Marshal(listEventData{Type: ev.Type, ListID: ev.ListID, ItemID: ev.ItemID, Version: ev.Version})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, data)
	return err
}
```

Every write goes through `w`, which is the request logger's chi wrapper, so the logged `bytes` stays accurate for streams. `requestLogger` logs the stream's line when the stream ends, with its full duration.

- [x] **Step 6: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/httpapi/... -run 'TestShoppingList' -v`
Expected: PASS, including `TestShoppingListEventsThroughTheContract` and `TestShoppingListEventsOutliveTheServerWriteTimeout`.

- [x] **Step 7: Prove the deadline override is load-bearing**

Temporarily replace `rc.SetWriteDeadline(time.Time{})` in `StreamShoppingListEvents` with `error(nil)`, then run:

Run: `cd backend && go test ./internal/httpapi/... -run TestShoppingListEventsOutliveTheServerWriteTimeout -v`
Expected: FAIL with `stream ended early`. The server cut the stream at its 200ms `WriteTimeout`. This was confirmed during planning.

Restore `rc.SetWriteDeadline(time.Time{})` and re-run: PASS.

- [x] **Step 8: Commit**

```bash
git add openapi.yaml backend/internal/api/api.gen.go backend/internal/httpapi/shopping_lists.go \
  backend/internal/httpapi/account.go backend/internal/httpapi/contract_test.go \
  backend/internal/httpapi/shopping_lists_flow_test.go
git commit -m "feat(api): stream shopping-list events over SSE past the server write timeout"
```

---

### Task 9: Shutdown ends open event streams

This task closes the second half of the "Carried forward" SSE item.

**Files:**
- Modify: `backend/cmd/api/main.go`
- Modify: `backend/cmd/api/main_test.go`

**Interfaces:**
- Consumes: `(*service.ListEventHub).Close` (Task 4); the `listEvents` variable Task 7 added to `serve`; the existing `startServer`/`runningServer` test helpers.
- Produces: nothing new.

- [x] **Step 1: Write the failing test**

Add to `backend/cmd/api/main_test.go`. Add `"bufio"` to its imports; `encoding/json`, `io`, `net/http`, `strings`, `time` and `testutil` are already there:

```go
// TestServeEndsOpenEventStreamsOnShutdown proves shutdown does not wait out
// shutdownTimeout (and fail) while a shopping-list event stream is open:
// http.Server.Shutdown never cancels request contexts, so the stream only
// ends because serve registers the event hub's Close with RegisterOnShutdown.
func TestServeEndsOpenEventStreamsOnShutdown(t *testing.T) {
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
	authed := func(method, path, body string) *http.Request {
		req, err := http.NewRequest(method, s.baseURL+path, strings.NewReader(body))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+session.AccessToken)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		return req
	}

	resp, err := s.client.Do(authed(http.MethodPost, "/shopping-lists", `{"name":"Groceries"}`))
	if err != nil {
		t.Fatalf("create list: %v", err)
	}
	var list struct {
		ID string `json:"id"`
	}
	err = json.NewDecoder(resp.Body).Decode(&list)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated || err != nil {
		t.Fatalf("create list: status = %d, err %v", resp.StatusCode, err)
	}

	// The stream outlives s.client's 2s timeout by design, so use a client
	// without one.
	stream, err := (&http.Client{}).Do(authed(http.MethodGet, "/shopping-lists/"+list.ID+"/events", ""))
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer func() { _ = stream.Body.Close() }()
	if line, err := bufio.NewReader(stream.Body).ReadString('\n'); err != nil || line != ": connected\n" {
		t.Fatalf("first line = %q (err %v), want \": connected\"", line, err)
	}

	s.stop()

	select {
	case err := <-s.done:
		if err != nil {
			t.Fatalf("serve returned %v with an open event stream, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return within 5s of cancellation with an open event stream")
	}
	if _, err := io.ReadAll(stream.Body); err != nil {
		t.Errorf("reading the rest of the stream: %v, want a clean end of stream", err)
	}
}
```

- [x] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./cmd/api/... -run TestServeEndsOpenEventStreamsOnShutdown -v`
Expected: FAIL, `serve did not return within 5s of cancellation with an open event stream`. `Shutdown` is waiting on a connection that never goes idle, and would give up only at the 10s `shutdownTimeout` with an error.

- [x] **Step 3: Register the hook**

In `backend/cmd/api/main.go`, change the tail of the `srv := &http.Server{...}` literal and add the hook right after it:

```go
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		// WriteTimeout bounds every ordinary response. The shopping-list event
		// stream clears it for its own response (see StreamShoppingListEvents).
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	// Shutdown stops accepting connections and waits for active ones to go
	// idle, but never cancels a request's context, so an open event stream
	// would never finish on its own. Closing the hub ends every stream
	// (their handlers return), while ordinary in-flight requests still drain
	// normally. Cancelling a BaseContext instead would also abort those
	// ordinary requests mid-write, defeating the graceful drain.
	srv.RegisterOnShutdown(listEvents.Close)
```

- [x] **Step 4: Run the tests to verify they pass**

Run: `cd backend && go test ./cmd/api/... -v`
Expected: PASS, including `TestServeEndsOpenEventStreamsOnShutdown` and every existing `TestServe*` test.

- [x] **Step 5: Commit**

```bash
git add backend/cmd/api/main.go backend/cmd/api/main_test.go
git commit -m "fix(backend): end open shopping-list event streams on shutdown"
```

---

### Task 10: Documentation and final checks

**Files:**
- Modify: `backend/CLAUDE.md`

**Interfaces:** none (documentation only).

- [x] **Step 1: Update `backend/CLAUDE.md`**

In "Behaviour worth knowing", replace the "Cascades alone are not enough for account deletion" bullet with:

```
- **Cascades alone are not enough for account deletion.** `users` cascades to `ingredients`, `meals`, `diet_templates`, `plan_entries` and `shopping_lists`, but `meal_ingredients.ingredient_id` and `template_slots`/`plan_entries.meal_id` are deliberately `NO ACTION` (what makes deleting an in-use ingredient or meal a `409`), and Postgres fires a table's own cascade trigger in an order this codebase does not control. `Auth.DeleteUser` deletes explicitly, in one transaction, in the only order that works: `shopping_lists` (cascades `shopping_items`; first only to keep the chain explicit, since `shopping_items`' references to `ingredients` and `users` are `ON DELETE SET NULL` and cannot abort anything) → `plan_entries` → `diet_templates` (cascades `template_slots`) → `meals` (cascades `meal_ingredients`) → `users` (cascades `ingredients`, now safe since nothing in `meal_ingredients` still references them). Any future table that both cascades from `users` and is referenced with `NO ACTION` from another user-owned table needs the same treatment, in the right position in this chain.
```

After the last bullet of "Behaviour worth knowing" (the `meal_in_use` one), add:

```
- **A shopping item is a snapshot, so it never blocks deleting its ingredient.** `shopping_items` stores its own `name`, `quantity`, `unit` and `category`; `ingredient_id` is `ON DELETE SET NULL` (not `NO ACTION` like `meal_ingredients`), so deleting a custom ingredient just turns the items that referenced it into free-text items. `origin` is unchanged, so a `generated` one is still replaced on the next regeneration.
- **Shopping item edits are versioned; checks are not.** Every change bumps `version` and the `PATCH` response carries the new one. Changing `name`/`quantity`/`unit`/`category` needs the current `version` (`400 version_required` without one, `409 version_conflict` with the current item in the problem's `current` member when stale). A `checked`-only `PATCH` is last-write-wins and ignores `version`; setting `checked` to its current value is a no-op (no bump, no event), which is what makes offline replays safe. `DELETE` of an item is unversioned. `UpdateItem` locks the item row (`SELECT ... FOR UPDATE`) before checking the version; regenerating and adding items lock the list row (`SetShoppingListSourceForUser`, `TouchShoppingListForUser`), the same concurrent-replace fix as `TouchMealForUser`.
- **Generating a list merges per ingredient, preferring the unit the recipes used.** Each meal line counts `quantity × portion / servings`. An ingredient used in one unit throughout keeps it ("6 piece"); mixed units merge into grams via `gramsFor` (the meals conversion), and a unit that cannot convert keeps its own line. `POST /shopping-lists/generate` with `list_id` regenerates that list: `generated` items are replaced (so they come back unchecked), `manual` items are untouched.
- **Shopping-list events are in memory, per process.** `ListEventHub` fans each committed change out to the open `GET /shopping-lists/{id}/events` streams of this process only: with several API replicas, a client sees only events from the replica it is connected to, and converges by refetching on reconnect and foreground (spec §4.3). A subscriber more than 32 events behind is disconnected rather than silently missing events (versions are per item, so a dropped event is not reliably detectable as a gap). The stream clears the server `WriteTimeout` for its own response, and `serve` closes the hub from `RegisterOnShutdown`, so shutdown ends streams without cancelling ordinary requests. The token goes in the `Authorization` header only, so the web client must use a fetch-based SSE reader, not `EventSource`.
```

In "Not built yet", replace the bullet that begins "The domain beyond diets and plan:" with:

```
- The domain beyond shopping lists: partners and sharing (backend build order item 6, the next plan). **Meals, diet templates and shopping lists have no partner visibility yet**: `shared_with_partner` is stored on all three, but every read and write checks `owner_id` only, because the `partnerships` table does not exist until the partner plan. For shopping lists that plan must also let the active partner edit a shared list (spec §3.6: editable by both): every owner check in `ShoppingLists` (`GetShoppingListForUser`, `TouchShoppingListForUser`, `SetShoppingListSourceForUser`, `GetShoppingItemForUserForUpdate`, `DeleteShoppingItemForUser`, and `Subscribe`) needs the active-partner lookup, `checked_by` starts meaning something (it is always the owner today), and unlinking must end the partner's open event streams. `plan_entries` has no sharing concept in the spec at all — it is always owner-only.
```

In "Carried forward (hardening to schedule)", delete the bullet that begins "Later (shopping-list SSE plan): the 30s server `WriteTimeout` cuts event streams". Tasks 8 and 9 implemented both halves, and the "Shopping-list events are in memory" bullet above now documents the result.

- [x] **Step 2: Run everything CI runs**

Run: `make check`
Expected: PASS. It lints `openapi.yaml`, vets and tests the backend, runs golangci-lint, and fails if generated code is stale.

Run: `make check-generated`
Expected: clean (no diff).

- [x] **Step 3: Commit**

```bash
git add backend/CLAUDE.md
git commit -m "docs(backend): document shopping lists and close out the SSE hardening items"
```

---

## Self-Review

- **Spec coverage:**
  - §3.5, schema: `shopping_lists` (`owner_id, name, shared_with_partner, source_from/source_to`) and `shopping_items` (`list_id, ingredient_id` nullable, `name, quantity, unit, category, checked, checked_by, position, version, origin`) are Task 1. Every column the spec names is there.
  - §3.5, generation: "read plan_entries in the range, convert to grams (or keep ml/piece where units cannot merge), sum per ingredient, group by category, write items with origin = generated" is Task 6's `generateLines`/`mergeUnits`, with the grouping order and the `origin`. "Regenerating a list replaces generated items and keeps manual items" is Task 6's `list_id` path, proven by `TestShoppingListsRegenerateReplacesGeneratedItemsAndKeepsManualOnes`.
  - §3.6, sharing rule. Owner-only reads and `404` for another user's list are Task 5, proven by `TestShoppingListsAreVisibleToTheirOwnerOnly`, including a list with `shared_with_partner: true`. "Editable by both" and "a partner checking an item emits an SSE event" are explicitly deferred to the partner plan, with the exact list of owner checks that plan must extend written into `backend/CLAUDE.md` (Task 10). This is the same scoping the meals and diets plans used, and the `partnerships` table's absence was confirmed by grep.
  - §4.1, endpoints: `GET/POST shopping-lists`, `GET/PATCH/DELETE shopping-lists/{id}`, `POST shopping-lists/generate ({from, to})`, `POST shopping-lists/{id}/items` and `PATCH/DELETE shopping-lists/{id}/items/{item}` are Task 7. `GET shopping-lists/{id}/events` is Task 8. That is all ten operations.
  - §4.2, conventions: cursor pagination on the one growing list is Task 7 (newest first, justified in Global Constraints). RFC 9457 problems with stable codes are in place, and the two new codes are `version_conflict` and `version_required`. The rest are reused: `not_found`, `invalid_ingredient`, `plan_range_too_long`/`plan_range_invalid`, `not_ready`. Shape validation stays with the schema, and business rules live in the service.
  - §4.3, sync: "Item edits carry the item's version. A stale write returns 409 with the current item" is Tasks 5 and 7, with `current` in the problem body. "Checking an item is idempotent" is Task 5's no-op check, proven by the replayed-check assertion. "Checked state is last-write-wins; adds and removes merge" is the check-only path ignoring `version`, plus unversioned `DELETE`. "SSE events carry list id, item id and new version" is Tasks 4, 5 and 8, where `listEventData` carries exactly those. "Refetch when they detect a version gap… refetch on reconnect" is backed by the disconnect-on-overflow policy, whose reasoning is in Global Constraints.
  - §6, testing: the "shopping-list merging" unit test is `TestShoppingListsGenerateSumsMergesAndGroupsByCategory`, a hand-computed golden test covering same-unit, mixed-convertible, mixed-unconvertible and out-of-range entries. Integration tests run against real Postgres, contract tests validate every new endpoint's request and response against `openapi.yaml` including the `text/event-stream` body, and TDD ordering holds in every task. §6's "fixture of two users, a partnership and a shared list" is only partly satisfiable here: there are two users and a shared-flagged list, and the partnership belongs to the partner plan.
  - §9, build order: this plan is item 2's "shopping lists". Its SSE is owner-only, and "partner and sharing with SSE" is left intact as the next plan.
  - The `backend/CLAUDE.md` carry-overs are both closed. The "Carried forward" SSE item is implemented (Task 8: write deadline; Task 9: shutdown) and removed (Task 10). The "Cascades alone are not enough" chain is extended and re-verified (Task 3).
- **Placeholder scan:** none. Every step has complete code, SQL or YAML, and every command has its expected output.
  - Task 3 Step 2 expects PASS before the change. That is not a gap in the TDD cycle. It is the explicit empirical check of the Global Constraints claim that `SET NULL` references cannot abort account deletion, and the step says what to do if it fails (the schema has drifted).
  - Task 8 Step 7 temporarily edits code and then restores it. This is a named verification step with the exact edit and the exact expected failure, both observed during planning.
- **Type consistency:**
  - Every `sqlc.*Params` field used in Tasks 3, 5 and 6 matches the scratch-verified signatures in Task 2's Interfaces block. In particular, `SourceFrom`/`SourceTo` are `pgtype.Date` (bridged by `toPgDate` and the new `fromPgDatePtr`), `CursorCreatedAt` is `time.Time`, `Position`/`Version` are `int32` (widened with `int(...)`, narrowed only with `toInt32`), and `GetIngredientsForUserParams.UserID` is `*uuid.UUID` (passed as `&ownerID`, as `Meals.toMeal` does).
  - `service.ShoppingItem`/`ShoppingList`/`ShoppingListSummary` fields match `toAPIShoppingItem`/`toAPIShoppingList`/`ListShoppingLists`' field-by-field conversions against the scratch-verified `api.*` types: `Position int`/`Version int` on both sides, `nullable.Nullable[...]` for every nullable column, and `api.ShoppingItemUnit` versus `api.UpdateShoppingItemRequestUnit` versus `api.Unit` used where the generator actually produced each.
  - `httpapi.ShoppingListsService` gains `Subscribe` only in Task 8, the same task that first calls it. `stubShoppingLists` embeds the interface, so it keeps compiling when the method set grows.
  - The `ListEvent` fields (`ItemID *uuid.UUID`, `Version *int`) are set by `publishItem` (Task 5) and read by `writeListEvent` (Task 8) with matching pointer types.
  - `uniqueUUIDs` is called with matching signatures at its two new call sites (`sqlc.PlanEntry`→`MealID`, `sqlc.MealIngredient`→`IngredientID`).
  - The test helpers `ptr` (`auth_test.go`) and `almostEqual` (`meals_test.go`) are reused, not redefined. A duplicate `ptr` in the first draft was caught by compiling the scratch copy.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-09-23-backend-shopping-lists.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
