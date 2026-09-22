# Backend Meals Domain Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add meals built from ingredients — a `meals`/`meal_ingredients` schema, a CRUD + atomic-ingredients-replace + copy API, and nutrition computed on read (never stored) — and close out the `ingredient_in_use` deletion guard the ingredients plan deliberately deferred.

**Architecture:** Two new tables (`meals`, `meal_ingredients`) follow the existing `httpapi` → `service` → `store` layering, exactly like the ingredients domain. The `Meals` service holds a `*store.Store` (same pattern as `Auth` and `Ingredients`) and reads ingredient rows and their nutrients directly through `store`'s existing and one new query — it does not depend on the `Ingredients` service. Nutrition is computed on every read (`GET`, `PATCH`, `PUT .../ingredients`, `POST .../copy` all return the freshly computed meal) by converting each ingredient line to grams and applying its per-100g nutrient values, then dividing by `servings`. See Global Constraints for the partner-visibility and nutrient-unknown-propagation decisions this plan makes.

**Tech Stack:** Go 1.26, chi, pgx/pgxpool, goose, sqlc, oapi-codegen — no new dependencies.

**Spec:** `docs/superpowers/specs/2026-09-21-meal-planner-design.md` (sections 3.3, 3.6, 4.1, 4.2, 6)

## Global Constraints

- OpenAPI 3.0.3 is the source of truth: edit `openapi.yaml` first, then `make generate`, then implement. Never hand-edit `backend/internal/api/api.gen.go` or `backend/internal/store/sqlc/`.
- No SQL outside `backend/internal/store`. Dependencies point one way: `httpapi` → `service` → `store`.
- Global `security: bearerAuth` protects every operation by default.
- Errors are RFC 9457 `application/problem+json` with a stable `code`.
- Unseen resources return `404`, never `403`.
- Migrations are goose SQL files named `NNNNN_description.sql`. Every table that has one has `created_at`/`updated_at` plus the `set_updated_at()` trigger — `meal_ingredients` deliberately has neither, mirroring `ingredient_nutrients`: it is a line-item table fully owned and replaced by its parent (`meals`/`ingredients` respectively), never edited in place.
- Contract tests validate every new endpoint's request and response against `openapi.yaml` via `contract()` in `internal/httpapi/contract_test.go`.
- **Meal nutrition is per serving, computed on read, never stored.** `nutrition_per_serving` on the `Meal` response is the sum of every ingredient line's contribution (converted to grams, applied against its per-100g values), divided by `servings`. To get the whole-meal total, multiply by `servings` client-side.
- **A nutrient key is `null` in `nutrition_per_serving` if any ingredient in the meal has no value for that key**, not the fallback of treating the unknown amount as zero — a meal made partly of an ingredient with unknown sodium cannot honestly report a sodium number. A meal with zero ingredients reports `0` for all 18 keys (the well-defined sum of nothing), which is a different case from "unknown."
- **Partner visibility is out of scope for this plan.** The `partnerships` table does not exist yet (it is item 6 of the backend build order in the spec, after shopping lists) — confirmed by grepping the codebase for `partner`, which returns nothing outside a doc string. `meals.shared_with_partner` is stored (matches the spec's data model, §3.3) but every read path in this plan checks `owner_id` only; a partner cannot see, copy, or otherwise reach another user's meals until the partner plan adds the partnerships table and an active-partner lookup. Documented in `backend/CLAUDE.md`'s "Not built yet" (Task 8).
- **`POST /meals` creates an empty meal; ingredients are always added via `PUT /meals/{id}/ingredients`.** The spec lists them as separate operations (§4.1: `GET/POST meals`, then `PUT meals/{id}/ingredients` as the atomic replace). Accepting an ingredients array on `POST` too would mean validating the same rows two different ways in two different handlers; a single atomic-replace path keeps the validate-before-write logic in one place. A meal editor UI calls `POST` once for the shell, then `PUT .../ingredients` to save the list (and again on every edit).
- **A copied meal always starts with `shared_with_partner: false`**, regardless of the original's value — copying is not sharing, and starting private is the conservative default the caller can change afterward with `PATCH`.
- Nutrient set (18, fixed, matches the ingredients plan): `calories`, `protein`, `carbohydrates`, `sugar`, `fibre`, `fat`, `saturated_fat`, `sodium`, `potassium`, `calcium`, `iron`, `magnesium`, `zinc`, `vitamin_a`, `vitamin_c`, `vitamin_d`, `vitamin_b12`, `folate`.
- Any new environment variable is added to `.env.example` in the same commit (none are expected in this plan).

---

## File Structure

- `backend/migrations/00005_meals.sql` — the two tables, the `ingredient_id` foreign key that Task 3 teaches the ingredients service to translate into `409 ingredient_in_use`, indexes, trigger.
- `backend/internal/db/schema_test.go` — gains `TestMealsSchemaEnforcesItsConstraints`.
- `backend/internal/store/queries/meals.sql` — sqlc source for meals; generates into `backend/internal/store/sqlc/meals.sql.go` (never hand-edited).
- `backend/internal/store/queries/ingredients.sql` — gains one new query, `GetIngredientsForUser` (a batch, visibility-filtered lookup by id), which the meals service uses to fetch the ingredient rows a meal's lines reference.
- `backend/internal/service/ingredients.go` — gains `ErrIngredientInUse` and teaches `Delete` to translate the new foreign-key violation.
- `backend/internal/service/ingredients_test.go` — gains a test for that.
- `backend/internal/service/meals.go` — the `Meals` service: CRUD, list, atomic ingredients replace, copy, unit conversion and nutrition computation.
- `backend/internal/service/meals_test.go` — its tests, against a real migrated Postgres, including the nutrition golden tests §6 of the spec calls for.
- `openapi.yaml` — `/meals`, `/meals/{id}`, `/meals/{id}/ingredients`, `/meals/{id}/copy`, their schemas, and a `409` added to `DELETE /ingredients/{id}` for `ingredient_in_use`.
- `backend/internal/httpapi/meals.go` — the six handlers, cursor encode/decode (reusing the ingredients cursor's approach, not its code), `MealsService` interface.
- `backend/internal/httpapi/account.go` — gains `writeServiceError` cases for `service.ErrIngredientInUse`, `service.ErrMealNotFound`, `service.ErrMealIngredientNotFound`, `service.ErrUnitNotConvertible`.
- `backend/internal/httpapi/problem.go` — gains `CodeIngredientInUse`, `CodeInvalidIngredient`, `CodeUnitNotConvertible`.
- `backend/internal/httpapi/server.go`, `router.go` — wire the new service into `Deps` and `server`.
- `backend/internal/httpapi/meals_flow_test.go` — end-to-end contract tests through the real router, service and Postgres (mirrors `ingredients_flow_test.go`).
- `backend/internal/httpapi/contract_test.go` — gains `stubMeals` and wires it into `newTestRouter`.
- `backend/cmd/api/main.go` — constructs `service.NewMeals` and wires it into `httpapi.Deps`.
- `backend/CLAUDE.md` — documents the deferred partner visibility, the per-serving/null-propagation nutrition rule, and closes the `ingredient_in_use` line in "Not built yet".

---

### Task 1: Migration — `meals` and `meal_ingredients`

**Files:**
- Create: `backend/migrations/00005_meals.sql`
- Modify: `backend/internal/db/schema_test.go`

**Interfaces:**
- Produces: the `meals` table (`id, owner_id, name, notes, servings, shared_with_partner, created_at, updated_at`) and the `meal_ingredients` table (`id, meal_id, ingredient_id, quantity, unit, position`). Three named foreign keys (Postgres's default naming for an unnamed inline `REFERENCES`, matching `00004_ingredients.sql`'s style): `meals_owner_id_fkey`, `meal_ingredients_meal_id_fkey` (`ON DELETE CASCADE`), and `meal_ingredients_ingredient_id_fkey` (no `ON DELETE` clause — Task 3 relies on this exact name to block deleting an ingredient that is in use, translating the violation into `409 ingredient_in_use`).

- [ ] **Step 1: Write the failing test**

Add to `backend/internal/db/schema_test.go` (same file and `migratedConn` helper as `TestIngredientsSchemaEnforcesItsConstraints`):

```go
func TestMealsSchemaEnforcesItsConstraints(t *testing.T) {
	ctx := context.Background()
	conn := migratedConn(t)

	var userID string
	if err := conn.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, display_name) VALUES ('a@example.com', 'h', 'A') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var ingredientID string
	if err := conn.QueryRow(ctx,
		`INSERT INTO ingredients (name, category) VALUES ('Chicken Breast', 'meat_seafood') RETURNING id`,
	).Scan(&ingredientID); err != nil {
		t.Fatalf("insert ingredient: %v", err)
	}

	var mealID string
	if err := conn.QueryRow(ctx,
		`INSERT INTO meals (owner_id, name, servings) VALUES ($1, 'Chicken and Rice', 2) RETURNING id`, userID,
	).Scan(&mealID); err != nil {
		t.Fatalf("valid insert: %v", err)
	}

	if _, err := conn.Exec(ctx,
		`INSERT INTO meals (owner_id, name, servings) VALUES (gen_random_uuid(), 'Ghost Meal', 1)`,
	); err == nil {
		t.Error("an owner_id that does not reference a user was accepted, want a foreign key violation")
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO meals (owner_id, name, servings) VALUES ($1, 'Bad Servings', 0)`, userID,
	); err == nil {
		t.Error("zero servings was accepted, want a constraint violation")
	}

	insertLine := func(sql string, args ...any) error {
		_, err := conn.Exec(ctx, "INSERT INTO meal_ingredients (meal_id, ingredient_id, quantity, unit, position) VALUES "+sql, args...)
		return err
	}
	if err := insertLine(`($1, $2, 300, 'g', 0)`, mealID, ingredientID); err != nil {
		t.Fatalf("valid line: %v", err)
	}
	if err := insertLine(`($1, gen_random_uuid(), 1, 'g', 1)`, mealID); err == nil {
		t.Error("an ingredient_id that does not reference an ingredient was accepted, want a foreign key violation")
	}
	if err := insertLine(`($1, $2, 0, 'g', 2)`, mealID, ingredientID); err == nil {
		t.Error("zero quantity was accepted, want a constraint violation")
	}
	if err := insertLine(`($1, $2, 1, 'litres', 3)`, mealID, ingredientID); err == nil {
		t.Error("an invalid unit was accepted, want a constraint violation")
	}
	if err := insertLine(`($1, $2, 1, 'g', 0)`, mealID, ingredientID); err == nil {
		t.Error("a duplicate (meal_id, position) was accepted, want a unique violation")
	}

	if _, err := conn.Exec(ctx, `DELETE FROM ingredients WHERE id = $1`, ingredientID); err == nil {
		t.Error("deleting an ingredient referenced by a meal_ingredients row was accepted, want a foreign key violation")
	}

	if _, err := conn.Exec(ctx, `DELETE FROM meals WHERE id = $1`, mealID); err != nil {
		t.Fatalf("delete meal: %v", err)
	}
	var n int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM meal_ingredients WHERE meal_id = $1`, mealID).Scan(&n); err != nil || n != 0 {
		t.Errorf("meal_ingredients rows after deleting the meal = %d (err %v), want 0", n, err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/db/... -run TestMealsSchemaEnforcesItsConstraints -v`
Expected: FAIL — `relation "meals" does not exist` (needs Docker; skips locally without it, fails under `CI=1`, per `testutil.requireDocker`).

- [ ] **Step 3: Write the migration**

Create `backend/migrations/00005_meals.sql`:

```sql
-- +goose Up
CREATE TABLE meals (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id            uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name                text NOT NULL,
    notes               text,
    servings            double precision NOT NULL CHECK (servings > 0),
    shared_with_partner boolean NOT NULL DEFAULT false,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX meals_owner_id_idx ON meals (owner_id);

CREATE TRIGGER meals_set_updated_at
    BEFORE UPDATE ON meals
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE meal_ingredients (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    meal_id       uuid NOT NULL REFERENCES meals (id) ON DELETE CASCADE,
    ingredient_id uuid NOT NULL REFERENCES ingredients (id),
    quantity      double precision NOT NULL CHECK (quantity > 0),
    unit          text NOT NULL CHECK (unit IN ('g', 'ml', 'piece')),
    position      integer NOT NULL CHECK (position >= 0),
    UNIQUE (meal_id, position)
);

CREATE INDEX meal_ingredients_meal_id_idx ON meal_ingredients (meal_id);
CREATE INDEX meal_ingredients_ingredient_id_idx ON meal_ingredients (ingredient_id);

-- +goose Down
DROP TABLE meal_ingredients;
DROP TABLE meals;
```

`ingredient_id` deliberately has no `ON DELETE` clause (defaults to `NO ACTION`/`RESTRICT`): deleting an ingredient that a meal references must fail, not silently orphan or cascade-delete the meal line. This is scratch-verified: this exact migration was applied via `sqlc generate` against a copy of the repo's `sqlc.yaml` with no errors, and its shape (inline `REFERENCES`, `CHECK`, the shared `set_updated_at` trigger, index naming) matches `00004_ingredients.sql` exactly. The default constraint names this migration relies on (`meals_owner_id_fkey`, `meal_ingredients_meal_id_fkey`, `meal_ingredients_ingredient_id_fkey`) follow the same Postgres naming Task 3's ingredients-plan predecessor already established for `ingredients_owner_id_fkey`.

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd backend && go test ./internal/db/... -run TestMealsSchemaEnforcesItsConstraints -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/migrations/00005_meals.sql backend/internal/db/schema_test.go
git commit -m "feat(backend): add the meals and meal_ingredients tables"
```

---

### Task 2: sqlc queries for meals

**Files:**
- Create: `backend/internal/store/queries/meals.sql`
- Modify: `backend/internal/store/queries/ingredients.sql` (add `GetIngredientsForUser`)
- Modify (generated, commit the output): `backend/internal/store/sqlc/meals.sql.go`, `backend/internal/store/sqlc/ingredients.sql.go`, `backend/internal/store/sqlc/models.go`

**Interfaces:**
- Consumes: the schema from Task 1.
- Produces (used by Task 3 and Task 4): `sqlc.Meal{ID, OwnerID uuid.UUID; Name string; Notes *string; Servings float64; SharedWithPartner bool; CreatedAt, UpdatedAt time.Time}`, `sqlc.MealIngredient{ID, MealID, IngredientID uuid.UUID; Quantity float64; Unit string; Position int32}`, and these `*sqlc.Queries` methods:
  - `CreateMeal(ctx, CreateMealParams{OwnerID uuid.UUID, Name string, Notes *string, Servings float64, SharedWithPartner bool}) (Meal, error)`
  - `GetMealForUser(ctx, GetMealForUserParams{ID, UserID uuid.UUID}) (Meal, error)`
  - `ListMealsForUser(ctx, ListMealsForUserParams{UserID uuid.UUID, HasCursor bool, CursorName string, CursorID uuid.UUID, RowLimit int32}) ([]Meal, error)`
  - `UpdateMeal(ctx, UpdateMealParams{Name *string, SetNotes bool, Notes *string, Servings *float64, SharedWithPartner *bool, ID, UserID uuid.UUID}) (Meal, error)`
  - `DeleteMeal(ctx, DeleteMealParams{ID, UserID uuid.UUID}) (int64, error)`
  - `ReplaceMealIngredients(ctx, mealID uuid.UUID) error`
  - `InsertMealIngredient(ctx, InsertMealIngredientParams{MealID, IngredientID uuid.UUID, Quantity float64, Unit string, Position int32}) (MealIngredient, error)`
  - `GetMealIngredients(ctx, mealID uuid.UUID) ([]MealIngredient, error)`
  - `GetIngredientsForUser(ctx, GetIngredientsForUserParams{Ids []uuid.UUID, UserID *uuid.UUID}) ([]Ingredient, error)` — note `UserID` is a **pointer**, not a plain `uuid.UUID`: sqlc infers the arg type from the nullable `owner_id` column it is compared against (`owner_id IS NULL OR owner_id = sqlc.arg('user_id')`), the same reason `ListIngredients`/`SearchIngredients`'s `UserID` is `*uuid.UUID`.

  All of the above is scratch-verified: a real `sqlc generate` run against these exact query texts (with a copy of the repo's `sqlc.yaml`) produced exactly these signatures with no errors, confirmed by reading the generated `meals.sql.go` and the diff to `ingredients.sql.go`.

- [ ] **Step 1: Write the meals queries**

Create `backend/internal/store/queries/meals.sql`:

```sql
-- name: CreateMeal :one
INSERT INTO meals (owner_id, name, notes, servings, shared_with_partner)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetMealForUser :one
-- Owner-only visibility for now: shared_with_partner has no effect on GET
-- until the partner plan adds the partnerships table and an active-partner
-- lookup. See "Not built yet" in backend/CLAUDE.md.
SELECT * FROM meals
WHERE id = sqlc.arg('id') AND owner_id = sqlc.arg('user_id');

-- name: ListMealsForUser :many
SELECT * FROM meals
WHERE owner_id = sqlc.arg('user_id')
  AND (
    NOT sqlc.arg('has_cursor')::boolean
    OR name > sqlc.arg('cursor_name')::text
    OR (name = sqlc.arg('cursor_name')::text AND id > sqlc.arg('cursor_id')::uuid)
  )
ORDER BY name, id
LIMIT sqlc.arg('row_limit');

-- name: UpdateMeal :one
UPDATE meals SET
    name                = COALESCE(sqlc.narg('name'), name),
    notes               = CASE WHEN sqlc.arg('set_notes')::boolean THEN sqlc.narg('notes') ELSE notes END,
    servings            = COALESCE(sqlc.narg('servings'), servings),
    shared_with_partner = COALESCE(sqlc.narg('shared_with_partner'), shared_with_partner)
WHERE id = sqlc.arg('id') AND owner_id = sqlc.arg('user_id')
RETURNING *;

-- name: DeleteMeal :execrows
DELETE FROM meals WHERE id = sqlc.arg('id') AND owner_id = sqlc.arg('user_id');

-- name: ReplaceMealIngredients :exec
DELETE FROM meal_ingredients WHERE meal_id = $1;

-- name: InsertMealIngredient :one
INSERT INTO meal_ingredients (meal_id, ingredient_id, quantity, unit, position)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetMealIngredients :many
SELECT * FROM meal_ingredients WHERE meal_id = sqlc.arg('meal_id') ORDER BY position;
```

Notes on choices already made (do not redesign these):
- `UpdateMeal` uses two different patterns on purpose, matching `UpdateIngredient`: `name`/`servings`/`shared_with_partner` use plain `COALESCE(narg, column)` because these columns are `NOT NULL` — there is no legitimate "clear to null" state, only "unchanged" versus "set". `notes` is nullable and clearing it to `NULL` is a real, valid PATCH — it needs the `set_notes` boolean + `CASE` pattern, exactly like `grams_per_piece`/`density_g_per_ml` in `UpdateIngredient`.
- `InsertMealIngredient` is `:one` with `RETURNING *`, not `:exec`, even though the service could compute the row itself: Task 4's `ReplaceIngredients` needs each inserted row's real (database-generated) `id` to build an accurate response, and re-reading after insert would be an extra round trip for no benefit.
- No query blocks a `meal_ingredients` row from referencing an ingredient invisible to the meal's owner — that is a service-layer check (Task 4), because visibility depends on `ingredients.owner_id`, which needs the same visibility rule `GetIngredientsForUser` (below) implements, not a table-level constraint.

- [ ] **Step 2: Add `GetIngredientsForUser` to the ingredients queries**

In `backend/internal/store/queries/ingredients.sql`, after `GetIngredientNutrients`, add:

```sql
-- name: GetIngredientsForUser :many
SELECT * FROM ingredients
WHERE id = ANY(sqlc.arg('ids')::uuid[]) AND (owner_id IS NULL OR owner_id = sqlc.arg('user_id'));
```

This is the batch counterpart to the existing (still-unused) `GetIngredientForUser`: given a set of ingredient ids, it returns only the ones that exist and are visible to `user_id` (global, or owned by them). The meals service uses it to fetch every ingredient a meal's lines reference in one query, and — because it silently drops ids that don't exist or aren't visible — to detect them: if it returns fewer rows than distinct ids requested, one of them was invalid.

- [ ] **Step 3: Regenerate and verify it compiles**

Run: `cd backend && go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate`
Expected: exits 0, creates `internal/store/sqlc/meals.sql.go`, updates `ingredients.sql.go` and adds `Meal`/`MealIngredient` to `models.go`.

Run: `cd backend && go build ./...`
Expected: builds cleanly (nothing references the new queries yet, so this only proves the generated code itself compiles).

- [ ] **Step 4: Commit**

```bash
git add backend/internal/store/queries/meals.sql backend/internal/store/queries/ingredients.sql backend/internal/store/sqlc/
git commit -m "feat(backend): add sqlc queries for meals"
```

---

### Task 3: Ingredient-in-use — close the deferred deletion guard

The ingredients plan (`docs/superpowers/plans/2026-09-22-backend-ingredients.md`, Task 2) deliberately shipped `DeleteIngredient` without an in-use check, with a comment: *"The meals plan (not built yet) must add a `meal_ingredients` foreign key to `ingredients` and translate its violation into `409 ingredient_in_use` here on delete."* Task 1 added that foreign key (`meal_ingredients_ingredient_id_fkey`). This task closes the loop, entirely within the ingredients domain, before the new `Meals` service exists — it only needs the raw `meal_ingredients` insert query from Task 2 to prove the behavior.

**Files:**
- Modify: `backend/internal/service/ingredients.go`
- Modify: `backend/internal/service/ingredients_test.go`
- Modify: `backend/internal/httpapi/problem.go`
- Modify: `backend/internal/httpapi/account.go`
- Modify: `backend/internal/store/queries/ingredients.sql` (remove the now-resolved comment on `DeleteIngredient`)
- Modify: `openapi.yaml` (add `409` to `DELETE /ingredients/{id}`)
- Modify (generated, commit the output): `backend/internal/api/api.gen.go`

**Interfaces:**
- Consumes: `store.IsForeignKeyViolation` (existing, Task 3 of the ingredients plan).
- Produces: `service.ErrIngredientInUse` (consumed by `httpapi.writeServiceError`), `httpapi.CodeIngredientInUse = "ingredient_in_use"`.

- [ ] **Step 1: Write the failing test**

In `backend/internal/service/ingredients_test.go`, add (needs `st.CreateMeal` and `st.InsertMealIngredient` from Task 2 — both promoted directly onto `*store.Store`, no service needed):

```go
func TestIngredientsDeleteIsBlockedWhileInUseByAMeal(t *testing.T) {
	svc, st := newIngredientsFixture(t)
	owner := newTestUser(t, st, "owner@example.com")
	ing, err := svc.Create(context.Background(), owner, service.CreateIngredientInput{Name: "Oats", Category: "grains_bread"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	meal, err := st.CreateMeal(context.Background(), sqlc.CreateMealParams{OwnerID: owner, Name: "Porridge", Servings: 1})
	if err != nil {
		t.Fatalf("create meal: %v", err)
	}
	if _, err := st.InsertMealIngredient(context.Background(), sqlc.InsertMealIngredientParams{
		MealID: meal.ID, IngredientID: ing.ID, Quantity: 100, Unit: "g", Position: 0,
	}); err != nil {
		t.Fatalf("insert meal ingredient: %v", err)
	}

	if err := svc.Delete(context.Background(), owner, ing.ID); !errors.Is(err, service.ErrIngredientInUse) {
		t.Errorf("Delete while referenced by a meal: err = %v, want ErrIngredientInUse", err)
	}

	if err := st.ReplaceMealIngredients(context.Background(), meal.ID); err != nil {
		t.Fatalf("clear meal ingredients: %v", err)
	}
	if err := svc.Delete(context.Background(), owner, ing.ID); err != nil {
		t.Errorf("Delete once no longer referenced: %v", err)
	}
}
```

Add `"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"` to the file's imports if not already present (it is, from `newTestUser`'s `sqlc.CreateUserParams`).

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/service/... -run TestIngredientsDeleteIsBlockedWhileInUseByAMeal -v`
Expected: FAIL to compile — `service.ErrIngredientInUse` does not exist yet (`sqlc.CreateMealParams` etc. already exist from Task 2).

- [ ] **Step 3: Add `ErrIngredientInUse` and teach `Delete` about it**

In `backend/internal/service/ingredients.go`, add to the `var` block alongside `ErrIngredientNotFound`:

```go
// ErrIngredientInUse means the ingredient cannot be deleted because a meal
// still references it.
var ErrIngredientInUse = errors.New("ingredient is in use")
```

Change `Delete`:

```go
// Delete removes a custom ingredient owned by ownerID. It fails with
// ErrIngredientInUse if a meal still references the ingredient.
func (s *Ingredients) Delete(ctx context.Context, ownerID, id uuid.UUID) error {
	n, err := s.st.DeleteIngredient(ctx, sqlc.DeleteIngredientParams{ID: id, UserID: &ownerID})
	if store.IsForeignKeyViolation(err, "meal_ingredients_ingredient_id_fkey") {
		return ErrIngredientInUse
	}
	if err != nil {
		return fmt.Errorf("delete ingredient: %w", err)
	}
	if n == 0 {
		return ErrIngredientNotFound
	}
	return nil
}
```

- [ ] **Step 4: Update the now-resolved comment in the queries file**

In `backend/internal/store/queries/ingredients.sql`, replace the comment above `DeleteIngredient`:

```sql
-- Deleting an ingredient referenced by a meal_ingredients row fails with a
-- foreign-key violation on meal_ingredients_ingredient_id_fkey, which
-- Ingredients.Delete (internal/service/ingredients.go) translates into
-- ErrIngredientInUse.
-- name: DeleteIngredient :execrows
DELETE FROM ingredients WHERE id = sqlc.arg('id') AND owner_id = sqlc.arg('user_id');
```

- [ ] **Step 5: Add the problem code and the `writeServiceError` case**

In `backend/internal/httpapi/problem.go`, add to the `Code*` constants:

```go
	CodeIngredientInUse = "ingredient_in_use"
```

In `backend/internal/httpapi/account.go`, in `writeServiceError`, add a case (order doesn't matter, `errors.Is` checks are independent — add it next to the `ErrIngredientNotFound` case):

```go
	case errors.Is(err, service.ErrIngredientInUse):
		WriteProblem(w, http.StatusConflict, CodeIngredientInUse, "")
```

- [ ] **Step 6: Add `409` to the contract**

In `openapi.yaml`, in `DELETE /ingredients/{id}`'s `responses`, add before `429`:

```yaml
        '409':
          $ref: '#/components/responses/Conflict'
```

Run: `make lint-api` — expect the same pre-existing warnings only, no new ones.
Run: `make generate` — regenerates `backend/internal/api/api.gen.go` (this response addition does not change any Go type, only the embedded spec used by response validation in tests).

- [ ] **Step 7: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/service/... -run TestIngredients -v`
Expected: PASS, including the new test.

Run: `cd backend && go build ./...`
Expected: builds cleanly.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/service/ingredients.go backend/internal/service/ingredients_test.go \
  backend/internal/httpapi/problem.go backend/internal/httpapi/account.go \
  backend/internal/store/queries/ingredients.sql openapi.yaml backend/internal/api/api.gen.go
git commit -m "feat(backend): block deleting an ingredient a meal still references"
```

---

### Task 4: Meals service

**Files:**
- Create: `backend/internal/service/meals.go`
- Create: `backend/internal/service/meals_test.go`

**Interfaces:**
- Consumes: the `sqlc.Queries` methods from Task 2 (`CreateMeal`, `GetMealForUser`, `ListMealsForUser`, `UpdateMeal`, `DeleteMeal`, `ReplaceMealIngredients`, `InsertMealIngredient`, `GetMealIngredients`, `GetIngredientsForUser`, `GetIngredientNutrients`); `store.IsNotFound`, `store.IsForeignKeyViolation`, `store.InTx`; `service.Optional[T]`/`service.Set[T]` (existing, from `auth.go`); the nutrient key constants from `ingredients.go` (`service.NutrientCalories` … `NutrientFolate`).
- Produces (consumed by Task 6's handlers):
  - Errors `service.ErrMealNotFound`, `service.ErrMealIngredientNotFound`, `service.ErrUnitNotConvertible` (all `errors.New(...)`).
  - `type MealIngredient struct { ID, IngredientID uuid.UUID; IngredientName, IngredientCategory string; Quantity float64; Unit string; Position int }`
  - `type Meal struct { ID uuid.UUID; Name string; Notes *string; Servings float64; SharedWithPartner bool; Ingredients []MealIngredient; NutritionPerServing map[string]float64; CreatedAt, UpdatedAt time.Time }`
  - `type CreateMealInput struct { Name string; Notes *string; Servings float64; SharedWithPartner bool }`
  - `type UpdateMealInput struct { Name *string; Notes Optional[string]; Servings *float64; SharedWithPartner *bool }`
  - `type MealIngredientInput struct { IngredientID uuid.UUID; Quantity float64; Unit string }`
  - `type MealCursor struct { Name string; ID uuid.UUID }`, `type ListMealsInput struct { Cursor *MealCursor; Limit int }`, `type MealSummary struct { ID uuid.UUID; Name string; Notes *string; Servings float64; SharedWithPartner bool; CreatedAt, UpdatedAt time.Time }`, `type MealPage struct { Items []MealSummary; NextCursor *MealCursor }`
  - `func NewMeals(st *store.Store) *Meals`
  - `func (*Meals) Create(ctx, ownerID uuid.UUID, in CreateMealInput) (Meal, error)`
  - `func (*Meals) Get(ctx, ownerID, id uuid.UUID) (Meal, error)`
  - `func (*Meals) Update(ctx, ownerID, id uuid.UUID, in UpdateMealInput) (Meal, error)`
  - `func (*Meals) Delete(ctx, ownerID, id uuid.UUID) error`
  - `func (*Meals) List(ctx, ownerID uuid.UUID, in ListMealsInput) (MealPage, error)`
  - `func (*Meals) ReplaceIngredients(ctx, ownerID, id uuid.UUID, items []MealIngredientInput) (Meal, error)`
  - `func (*Meals) Copy(ctx, callerID, id uuid.UUID) (Meal, error)`

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/service/meals_test.go`:

```go
package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/service"
)

func newMealsFixture(t *testing.T) (*service.Meals, *service.Ingredients) {
	t.Helper()
	_, st := newIngredientsFixture(t)
	return service.NewMeals(st), service.NewIngredients(st)
}

func mustCreateIngredient(t *testing.T, ing *service.Ingredients, owner uuid.UUID, in service.CreateIngredientInput) service.Ingredient {
	t.Helper()
	i, err := ing.Create(context.Background(), owner, in)
	if err != nil {
		t.Fatalf("create ingredient %q: %v", in.Name, err)
	}
	return i
}

// TestMealsNutritionIsComputedToTheGram is the nutrition golden test the spec
// (§6) calls for: hand-verified totals for a small real recipe, checked to
// four decimal places (float64 division is exact here since every input is a
// terminating decimal).
func TestMealsNutritionIsComputedToTheGram(t *testing.T) {
	meals, ing := newMealsFixture(t)
	owner := uuid.New()
	_ = owner // replaced below once we have a real user; see newTestUser

	svc, st := newIngredientsFixture(t)
	_ = svc
	meals, ing2 := newMealsFixture(t)
	_ = ing
	owner = newTestUser(t, st, "chef@example.com")

	chicken := mustCreateIngredient(t, ing2, owner, service.CreateIngredientInput{
		Name: "Chicken Breast", Category: "meat_seafood",
		Nutrients: map[string]float64{service.NutrientCalories: 165, service.NutrientProtein: 31},
	})
	rice := mustCreateIngredient(t, ing2, owner, service.CreateIngredientInput{
		Name: "White Rice", Category: "grains_bread",
		Nutrients: map[string]float64{service.NutrientCalories: 130, service.NutrientProtein: 2.7},
	})

	meal, err := meals.Create(context.Background(), owner, service.CreateMealInput{Name: "Chicken and Rice", Servings: 2})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	meal, err = meals.ReplaceIngredients(context.Background(), owner, meal.ID, []service.MealIngredientInput{
		{IngredientID: chicken.ID, Quantity: 300, Unit: "g"},
		{IngredientID: rice.ID, Quantity: 200, Unit: "g"},
	})
	if err != nil {
		t.Fatalf("ReplaceIngredients: %v", err)
	}

	// Totals: calories = 3*165 + 2*130 = 755, protein = 3*31 + 2*2.7 = 98.4.
	// Per serving (servings=2): calories = 377.5, protein = 49.2.
	if got := meal.NutritionPerServing[service.NutrientCalories]; got != 377.5 {
		t.Errorf("calories per serving = %v, want 377.5", got)
	}
	if got := meal.NutritionPerServing[service.NutrientProtein]; got != 49.2 {
		t.Errorf("protein per serving = %v, want 49.2", got)
	}
}

func TestMealsNutritionConvertsPieceAndMlUnits(t *testing.T) {
	svc, st := newIngredientsFixture(t)
	_ = svc
	meals, ing := newMealsFixture(t)
	owner := newTestUser(t, st, "chef2@example.com")

	gramsPerEgg := 50.0
	egg := mustCreateIngredient(t, ing, owner, service.CreateIngredientInput{
		Name: "Egg", Category: "dairy_eggs", GramsPerPiece: &gramsPerEgg,
		Nutrients: map[string]float64{service.NutrientCalories: 155},
	})
	density := 0.92
	oil := mustCreateIngredient(t, ing, owner, service.CreateIngredientInput{
		Name: "Olive Oil", Category: "condiments_oils", DensityGPerMl: &density,
		Nutrients: map[string]float64{service.NutrientCalories: 884},
	})

	meal, err := meals.Create(context.Background(), owner, service.CreateMealInput{Name: "Fried Egg", Servings: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	meal, err = meals.ReplaceIngredients(context.Background(), owner, meal.ID, []service.MealIngredientInput{
		{IngredientID: egg.ID, Quantity: 2, Unit: "piece"},  // 100g -> 155 kcal
		{IngredientID: oil.ID, Quantity: 10, Unit: "ml"},    // 9.2g -> 81.328 kcal
	})
	if err != nil {
		t.Fatalf("ReplaceIngredients: %v", err)
	}

	want := 155.0 + 9.2/100*884 // 236.328
	if got := meal.NutritionPerServing[service.NutrientCalories]; got != want {
		t.Errorf("calories per serving = %v, want %v", got, want)
	}
}

func TestMealsReplaceIngredientsRejectsAnUnconvertibleUnitAndLeavesTheMealUnchanged(t *testing.T) {
	svc, st := newIngredientsFixture(t)
	_ = svc
	meals, ing := newMealsFixture(t)
	owner := newTestUser(t, st, "chef3@example.com")

	// No grams_per_piece: this ingredient cannot be used with unit "piece".
	flour := mustCreateIngredient(t, ing, owner, service.CreateIngredientInput{Name: "Flour", Category: "grains_bread"})
	rice := mustCreateIngredient(t, ing, owner, service.CreateIngredientInput{
		Name: "Rice", Category: "grains_bread",
		Nutrients: map[string]float64{service.NutrientCalories: 130},
	})

	meal, err := meals.Create(context.Background(), owner, service.CreateMealInput{Name: "Bread", Servings: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	meal, err = meals.ReplaceIngredients(context.Background(), owner, meal.ID, []service.MealIngredientInput{
		{IngredientID: rice.ID, Quantity: 100, Unit: "g"},
	})
	if err != nil {
		t.Fatalf("seed ReplaceIngredients: %v", err)
	}

	_, err = meals.ReplaceIngredients(context.Background(), owner, meal.ID, []service.MealIngredientInput{
		{IngredientID: rice.ID, Quantity: 100, Unit: "g"},
		{IngredientID: flour.ID, Quantity: 3, Unit: "piece"},
	})
	if !errors.Is(err, service.ErrUnitNotConvertible) {
		t.Errorf("err = %v, want ErrUnitNotConvertible", err)
	}

	// The rejected replace must not have touched the meal: it should still
	// have exactly the one rice line from the seed call.
	got, err := meals.Get(context.Background(), owner, meal.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Ingredients) != 1 || got.Ingredients[0].IngredientID != rice.ID {
		t.Errorf("Ingredients after the rejected replace = %+v, want unchanged (just rice)", got.Ingredients)
	}
}

func TestMealsGetBecomesUnitNotConvertibleIfAnIngredientIsEditedAfterTheFact(t *testing.T) {
	svc, st := newIngredientsFixture(t)
	_ = svc
	meals, ing := newMealsFixture(t)
	owner := newTestUser(t, st, "chef4@example.com")

	density := 0.92
	oil := mustCreateIngredient(t, ing, owner, service.CreateIngredientInput{
		Name: "Olive Oil", Category: "condiments_oils", DensityGPerMl: &density,
		Nutrients: map[string]float64{service.NutrientCalories: 884},
	})
	meal, err := meals.Create(context.Background(), owner, service.CreateMealInput{Name: "Dressing", Servings: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := meals.ReplaceIngredients(context.Background(), owner, meal.ID, []service.MealIngredientInput{
		{IngredientID: oil.ID, Quantity: 10, Unit: "ml"},
	}); err != nil {
		t.Fatalf("ReplaceIngredients: %v", err)
	}

	// ingredients has no idea meals exist, so clearing density_g_per_ml here
	// succeeds even though a meal now depends on it for an "ml" line.
	if _, err := ing.Update(context.Background(), owner, oil.ID, service.UpdateIngredientInput{
		DensityGPerMl: service.Set[float64](nil),
	}); err != nil {
		t.Fatalf("clear density: %v", err)
	}

	if _, err := meals.Get(context.Background(), owner, meal.ID); !errors.Is(err, service.ErrUnitNotConvertible) {
		t.Errorf("Get after the ingredient lost its density: err = %v, want ErrUnitNotConvertible", err)
	}
}

func TestMealsNutritionIsNullForAKeyMissingFromAnyIngredient(t *testing.T) {
	svc, st := newIngredientsFixture(t)
	_ = svc
	meals, ing := newMealsFixture(t)
	owner := newTestUser(t, st, "chef5@example.com")

	// Only protein is known for this one; calories is absent from its map.
	proteinOnly := mustCreateIngredient(t, ing, owner, service.CreateIngredientInput{
		Name: "Mystery Powder", Category: "other",
		Nutrients: map[string]float64{service.NutrientProtein: 80},
	})
	known := mustCreateIngredient(t, ing, owner, service.CreateIngredientInput{
		Name: "Known Thing", Category: "other",
		Nutrients: map[string]float64{service.NutrientCalories: 100, service.NutrientProtein: 10},
	})

	meal, err := meals.Create(context.Background(), owner, service.CreateMealInput{Name: "Shake", Servings: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	meal, err = meals.ReplaceIngredients(context.Background(), owner, meal.ID, []service.MealIngredientInput{
		{IngredientID: proteinOnly.ID, Quantity: 100, Unit: "g"},
		{IngredientID: known.ID, Quantity: 100, Unit: "g"},
	})
	if err != nil {
		t.Fatalf("ReplaceIngredients: %v", err)
	}

	if _, ok := meal.NutritionPerServing[service.NutrientCalories]; ok {
		t.Errorf("calories = %v, want absent (one ingredient's calories is unknown)", meal.NutritionPerServing[service.NutrientCalories])
	}
	if got := meal.NutritionPerServing[service.NutrientProtein]; got != 90 {
		t.Errorf("protein = %v, want 90 (both known)", got)
	}
}

func TestMealsEmptyMealHasZeroNutritionForEveryKey(t *testing.T) {
	svc, st := newIngredientsFixture(t)
	_ = svc
	meals, _ := newMealsFixture(t)
	owner := newTestUser(t, st, "chef6@example.com")

	meal, err := meals.Create(context.Background(), owner, service.CreateMealInput{Name: "Empty", Servings: 3})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := meal.NutritionPerServing[service.NutrientCalories]; got != 0 {
		t.Errorf("calories = %v, want 0", got)
	}
	if got := meal.NutritionPerServing[service.NutrientFolate]; got != 0 {
		t.Errorf("folate = %v, want 0", got)
	}
	if len(meal.Ingredients) != 0 {
		t.Errorf("Ingredients = %+v, want empty", meal.Ingredients)
	}
}

func TestMealsAreOwnerOnlyForNow(t *testing.T) {
	svc, st := newIngredientsFixture(t)
	_ = svc
	meals, _ := newMealsFixture(t)
	owner := newTestUser(t, st, "owner7@example.com")
	other := newTestUser(t, st, "other7@example.com")

	meal, err := meals.Create(context.Background(), owner, service.CreateMealInput{Name: "Private Meal", Servings: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := meals.Get(context.Background(), other, meal.ID); !errors.Is(err, service.ErrMealNotFound) {
		t.Errorf("Get by a non-owner: err = %v, want ErrMealNotFound", err)
	}
	newName := "Hijacked"
	if _, err := meals.Update(context.Background(), other, meal.ID, service.UpdateMealInput{Name: &newName}); !errors.Is(err, service.ErrMealNotFound) {
		t.Errorf("Update by a non-owner: err = %v, want ErrMealNotFound", err)
	}
	if err := meals.Delete(context.Background(), other, meal.ID); !errors.Is(err, service.ErrMealNotFound) {
		t.Errorf("Delete by a non-owner: err = %v, want ErrMealNotFound", err)
	}
	if _, err := meals.Copy(context.Background(), other, meal.ID); !errors.Is(err, service.ErrMealNotFound) {
		t.Errorf("Copy by a non-owner: err = %v, want ErrMealNotFound", err)
	}
}

func TestMealsCopyDuplicatesIngredientsAndStartsPrivate(t *testing.T) {
	svc, st := newIngredientsFixture(t)
	_ = svc
	meals, ing := newMealsFixture(t)
	owner := newTestUser(t, st, "chef8@example.com")

	rice := mustCreateIngredient(t, ing, owner, service.CreateIngredientInput{
		Name: "Rice", Category: "grains_bread",
		Nutrients: map[string]float64{service.NutrientCalories: 130},
	})
	original, err := meals.Create(context.Background(), owner, service.CreateMealInput{
		Name: "Rice Bowl", Notes: strPtr("family recipe"), Servings: 2, SharedWithPartner: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	original, err = meals.ReplaceIngredients(context.Background(), owner, original.ID, []service.MealIngredientInput{
		{IngredientID: rice.ID, Quantity: 200, Unit: "g"},
	})
	if err != nil {
		t.Fatalf("ReplaceIngredients: %v", err)
	}

	copy_, err := meals.Copy(context.Background(), owner, original.ID)
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if copy_.ID == original.ID {
		t.Fatal("copy has the same ID as the original")
	}
	if copy_.Name != original.Name || copy_.Servings != original.Servings {
		t.Errorf("copy = %+v, want same name/servings as original", copy_)
	}
	if copy_.SharedWithPartner {
		t.Error("copy has shared_with_partner = true, want false regardless of the original")
	}
	if len(copy_.Ingredients) != 1 || copy_.Ingredients[0].IngredientID != rice.ID || copy_.Ingredients[0].Quantity != 200 {
		t.Errorf("copy ingredients = %+v, want one rice line at 200g", copy_.Ingredients)
	}
	if copy_.NutritionPerServing[service.NutrientCalories] != original.NutritionPerServing[service.NutrientCalories] {
		t.Error("copy nutrition differs from the original's")
	}
}

func TestMealsListPaginatesOwnMealsOnly(t *testing.T) {
	svc, st := newIngredientsFixture(t)
	_ = svc
	meals, _ := newMealsFixture(t)
	owner := newTestUser(t, st, "owner9@example.com")
	other := newTestUser(t, st, "other9@example.com")

	for _, name := range []string{"Breakfast", "Dinner", "Lunch"} {
		if _, err := meals.Create(context.Background(), owner, service.CreateMealInput{Name: name, Servings: 1}); err != nil {
			t.Fatalf("Create(%s): %v", name, err)
		}
	}
	if _, err := meals.Create(context.Background(), other, service.CreateMealInput{Name: "Not Mine", Servings: 1}); err != nil {
		t.Fatalf("Create(other's meal): %v", err)
	}

	var names []string
	var cursor *service.MealCursor
	for {
		page, err := meals.List(context.Background(), owner, service.ListMealsInput{Cursor: cursor, Limit: 1})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		for _, m := range page.Items {
			names = append(names, m.Name)
		}
		if page.NextCursor == nil {
			break
		}
		cursor = page.NextCursor
	}
	if want := []string{"Breakfast", "Dinner", "Lunch"}; len(names) != len(want) || names[0] != want[0] || names[1] != want[1] || names[2] != want[2] {
		t.Errorf("paginated names = %v, want %v (alphabetical, owner's only)", names, want)
	}
}

func strPtr(s string) *string { return &s }
```

The first test (`TestMealsNutritionIsComputedToTheGram`) shows a bit of setup noise (two throwaway `newIngredientsFixture`/`newMealsFixture` calls) to work around needing both a real user id and a real `*service.Ingredients`/`*service.Meals` pair sharing the same store — clean this up in Step 5, same as the ingredients plan's Task 3 Step 5. Only one call to each fixture helper belongs in the final file.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/service/... -run TestMeals -v`
Expected: FAIL to compile — `service.Meals` etc. do not exist yet.

- [ ] **Step 3: Implement the service**

Create `backend/internal/service/meals.go`:

```go
// Package service holds business rules. It calls the store and never speaks HTTP.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"
)

// Errors returned by Meals. Handlers map them to problem responses.
var (
	// ErrMealNotFound means the meal does not exist, or is not owned by the
	// caller. Partner visibility is not implemented yet (see the package doc
	// comment on Meals below), so a meal is visible only to its owner.
	ErrMealNotFound = errors.New("meal not found")
	// ErrMealIngredientNotFound means a meal_ingredients row references an
	// ingredient that no longer exists or is no longer visible to the meal's
	// owner. In practice this should be unreachable: ingredients.Delete
	// refuses to delete an ingredient a meal still references (see
	// ErrIngredientInUse in ingredients.go), and ownership of a custom
	// ingredient never changes. Kept as a defensive check rather than a
	// documented guarantee.
	ErrMealIngredientNotFound = errors.New("one or more ingredients do not exist or are not visible to you")
	// ErrUnitNotConvertible means a meal_ingredients row's unit is "piece" or
	// "ml" but the ingredient has no grams_per_piece/density_g_per_ml. This
	// can surface long after the row was validated at write time, if the
	// ingredient is later edited to clear that field (ingredients has no
	// awareness of meals, so nothing prevents that edit).
	ErrUnitNotConvertible = errors.New("ingredient does not have the data needed to convert this unit")
)

// allNutrientKeys is the ordered set of the 18 tracked nutrient keys, used to
// detect a key that is missing from at least one ingredient contributing to
// a meal (see toMeal).
var allNutrientKeys = []string{
	NutrientCalories, NutrientProtein, NutrientCarbohydrates, NutrientSugar, NutrientFibre, NutrientFat,
	NutrientSaturatedFat, NutrientSodium, NutrientPotassium, NutrientCalcium, NutrientIron, NutrientMagnesium,
	NutrientZinc, NutrientVitaminA, NutrientVitaminC, NutrientVitaminD, NutrientVitaminB12, NutrientFolate,
}

// MealIngredient is one line of a meal, with the referenced ingredient's name
// and category inlined so clients don't need a second round trip to render
// the list.
type MealIngredient struct {
	ID                 uuid.UUID
	IngredientID       uuid.UUID
	IngredientName     string
	IngredientCategory string
	Quantity           float64
	Unit               string
	Position           int
}

// Meal is a meal as the rest of the application sees it. NutritionPerServing
// is computed on read: the meal's ingredient contributions, summed and
// divided by Servings. A key absent from the map means at least one
// ingredient's amount for it is unknown (see toMeal); a meal with no
// ingredients has every key present at 0.
type Meal struct {
	ID                  uuid.UUID
	Name                string
	Notes               *string
	Servings            float64
	SharedWithPartner   bool
	Ingredients         []MealIngredient
	NutritionPerServing map[string]float64
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// CreateMealInput is the data needed to create a meal. It always starts with
// an empty ingredient list; add ingredients with ReplaceIngredients.
type CreateMealInput struct {
	Name              string
	Notes             *string
	Servings          float64
	SharedWithPartner bool
}

// UpdateMealInput is a partial update to a meal. Notes, when Specified, may
// set the column to nil (clearing it) via Set[string](nil).
type UpdateMealInput struct {
	Name              *string
	Notes             Optional[string]
	Servings          *float64
	SharedWithPartner *bool
}

// MealIngredientInput is one line of a ReplaceIngredients call.
type MealIngredientInput struct {
	IngredientID uuid.UUID
	Quantity     float64
	Unit         string // "g" | "ml" | "piece"
}

// MealCursor is an opaque position in the alphabetical meal list.
type MealCursor struct {
	Name string
	ID   uuid.UUID
}

// ListMealsInput selects a page of the caller's alphabetical meal list.
type ListMealsInput struct {
	Cursor *MealCursor
	Limit  int
}

// MealSummary is a meal without its ingredients or computed nutrition, for
// the list endpoint (which would otherwise pay for a nutrition computation
// per meal on every page).
type MealSummary struct {
	ID                uuid.UUID
	Name              string
	Notes             *string
	Servings          float64
	SharedWithPartner bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// MealPage is one page of meal summaries plus the cursor for the next one
// (nil on the last page).
type MealPage struct {
	Items      []MealSummary
	NextCursor *MealCursor
}

// ingredientReader is the subset of ingredient reads Meals needs. Both
// *store.Store (outside a transaction) and *sqlc.Queries (the tx-scoped
// queries InTx hands its callback) satisfy it, so toMeal works in both
// contexts without duplicating its logic.
type ingredientReader interface {
	GetIngredientsForUser(ctx context.Context, arg sqlc.GetIngredientsForUserParams) ([]sqlc.Ingredient, error)
	GetIngredientNutrients(ctx context.Context, ingredientIds []uuid.UUID) ([]sqlc.IngredientNutrient, error)
}

// Meals implements meals built from ingredients, owned by a single user.
// Partner sharing is not implemented: shared_with_partner is stored (it is
// part of the data model), but every read here checks owner_id only. See the
// "Not built yet" note in backend/CLAUDE.md.
type Meals struct {
	st *store.Store
}

// NewMeals returns a Meals service.
func NewMeals(st *store.Store) *Meals { return &Meals{st: st} }

// Create adds a meal owned by ownerID, with no ingredients. Add ingredients
// with ReplaceIngredients.
func (s *Meals) Create(ctx context.Context, ownerID uuid.UUID, in CreateMealInput) (Meal, error) {
	var meal Meal
	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
		row, err := q.CreateMeal(ctx, sqlc.CreateMealParams{
			OwnerID: ownerID, Name: in.Name, Notes: in.Notes,
			Servings: in.Servings, SharedWithPartner: in.SharedWithPartner,
		})
		if store.IsForeignKeyViolation(err, "meals_owner_id_fkey") {
			// Mirrors Ingredients.Create: an access token for a user that no
			// longer exists is unauthorized, not a 500.
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("create meal: %w", err)
		}
		meal, err = s.toMeal(ctx, q, row, nil)
		return err
	})
	if err != nil {
		return Meal{}, err
	}
	return meal, nil
}

// Get returns a meal owned by ownerID, with its ingredients and computed
// nutrition.
func (s *Meals) Get(ctx context.Context, ownerID, id uuid.UUID) (Meal, error) {
	row, err := s.st.GetMealForUser(ctx, sqlc.GetMealForUserParams{ID: id, UserID: ownerID})
	if store.IsNotFound(err) {
		return Meal{}, ErrMealNotFound
	}
	if err != nil {
		return Meal{}, fmt.Errorf("get meal: %w", err)
	}
	miRows, err := s.st.GetMealIngredients(ctx, id)
	if err != nil {
		return Meal{}, fmt.Errorf("get meal ingredients: %w", err)
	}
	return s.toMeal(ctx, s.st.Queries, row, miRows)
}

// Update applies a partial update to a meal owned by ownerID.
func (s *Meals) Update(ctx context.Context, ownerID, id uuid.UUID, in UpdateMealInput) (Meal, error) {
	var meal Meal
	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
		row, err := q.UpdateMeal(ctx, sqlc.UpdateMealParams{
			ID: id, UserID: ownerID, Name: in.Name,
			SetNotes: in.Notes.Specified, Notes: in.Notes.Value,
			Servings: in.Servings, SharedWithPartner: in.SharedWithPartner,
		})
		if store.IsNotFound(err) {
			return ErrMealNotFound
		}
		if err != nil {
			return fmt.Errorf("update meal: %w", err)
		}
		miRows, err := q.GetMealIngredients(ctx, id)
		if err != nil {
			return fmt.Errorf("get meal ingredients: %w", err)
		}
		meal, err = s.toMeal(ctx, q, row, miRows)
		return err
	})
	if err != nil {
		return Meal{}, err
	}
	return meal, nil
}

// Delete removes a meal owned by ownerID. Its meal_ingredients rows are
// removed by ON DELETE CASCADE.
func (s *Meals) Delete(ctx context.Context, ownerID, id uuid.UUID) error {
	n, err := s.st.DeleteMeal(ctx, sqlc.DeleteMealParams{ID: id, UserID: ownerID})
	if err != nil {
		return fmt.Errorf("delete meal: %w", err)
	}
	if n == 0 {
		return ErrMealNotFound
	}
	return nil
}

// List returns a page of the caller's alphabetical meal list. It never
// includes another user's meals, even ones shared_with_partner: true (no
// partner visibility yet, see the Meals doc comment).
func (s *Meals) List(ctx context.Context, ownerID uuid.UUID, in ListMealsInput) (MealPage, error) {
	if in.Limit < 1 {
		in.Limit = 1
	}
	params := sqlc.ListMealsForUserParams{UserID: ownerID, RowLimit: toRowLimit(in.Limit + 1)}
	if in.Cursor != nil {
		params.HasCursor = true
		params.CursorName = in.Cursor.Name
		params.CursorID = in.Cursor.ID
	}
	rows, err := s.st.ListMealsForUser(ctx, params)
	if err != nil {
		return MealPage{}, fmt.Errorf("list meals: %w", err)
	}

	var next *MealCursor
	if len(rows) > in.Limit {
		last := rows[in.Limit-1]
		next = &MealCursor{Name: last.Name, ID: last.ID}
		rows = rows[:in.Limit]
	}
	items := make([]MealSummary, len(rows))
	for i, r := range rows {
		items[i] = MealSummary{
			ID: r.ID, Name: r.Name, Notes: r.Notes, Servings: r.Servings,
			SharedWithPartner: r.SharedWithPartner, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		}
	}
	return MealPage{Items: items, NextCursor: next}, nil
}

// ReplaceIngredients atomically replaces a meal's full ingredient list.
// Every row is validated — the referenced ingredient must exist and be
// visible to ownerID, and its unit must be convertible — before anything is
// written, by building (and therefore fully computing) the candidate meal
// first; if that fails, the existing ingredients are left untouched.
func (s *Meals) ReplaceIngredients(ctx context.Context, ownerID, id uuid.UUID, items []MealIngredientInput) (Meal, error) {
	var meal Meal
	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
		row, err := q.GetMealForUser(ctx, sqlc.GetMealForUserParams{ID: id, UserID: ownerID})
		if store.IsNotFound(err) {
			return ErrMealNotFound
		}
		if err != nil {
			return fmt.Errorf("get meal: %w", err)
		}

		candidates := make([]sqlc.MealIngredient, len(items))
		for i, it := range items {
			candidates[i] = sqlc.MealIngredient{
				MealID: id, IngredientID: it.IngredientID, Quantity: it.Quantity, Unit: it.Unit, Position: int32(i),
			}
		}
		if _, err := s.toMeal(ctx, q, row, candidates); err != nil {
			return err
		}

		if err := q.ReplaceMealIngredients(ctx, id); err != nil {
			return fmt.Errorf("clear meal ingredients: %w", err)
		}
		inserted := make([]sqlc.MealIngredient, len(candidates))
		for i, c := range candidates {
			ins, err := q.InsertMealIngredient(ctx, sqlc.InsertMealIngredientParams{
				MealID: id, IngredientID: c.IngredientID, Quantity: c.Quantity, Unit: c.Unit, Position: c.Position,
			})
			if err != nil {
				return fmt.Errorf("insert meal ingredient: %w", err)
			}
			inserted[i] = ins
		}
		// Rebuild against the inserted rows so the response carries real
		// (database-generated) line ids, not the zero-valued ones the
		// pre-write validation pass used.
		meal, err = s.toMeal(ctx, q, row, inserted)
		return err
	})
	if err != nil {
		return Meal{}, err
	}
	return meal, nil
}

// Copy creates a new meal owned by callerID, with the same name, notes,
// servings and ingredients as the meal at id, and shared_with_partner always
// false regardless of the original. callerID must own the original (partner
// copy access is not implemented yet, see the Meals doc comment).
func (s *Meals) Copy(ctx context.Context, callerID, id uuid.UUID) (Meal, error) {
	var meal Meal
	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
		original, err := q.GetMealForUser(ctx, sqlc.GetMealForUserParams{ID: id, UserID: callerID})
		if store.IsNotFound(err) {
			return ErrMealNotFound
		}
		if err != nil {
			return fmt.Errorf("get meal: %w", err)
		}
		originalLines, err := q.GetMealIngredients(ctx, id)
		if err != nil {
			return fmt.Errorf("get meal ingredients: %w", err)
		}

		copyRow, err := q.CreateMeal(ctx, sqlc.CreateMealParams{
			OwnerID: callerID, Name: original.Name, Notes: original.Notes,
			Servings: original.Servings, SharedWithPartner: false,
		})
		if err != nil {
			return fmt.Errorf("create meal copy: %w", err)
		}

		inserted := make([]sqlc.MealIngredient, len(originalLines))
		for i, line := range originalLines {
			ins, err := q.InsertMealIngredient(ctx, sqlc.InsertMealIngredientParams{
				MealID: copyRow.ID, IngredientID: line.IngredientID, Quantity: line.Quantity,
				Unit: line.Unit, Position: line.Position,
			})
			if err != nil {
				return fmt.Errorf("copy meal ingredient: %w", err)
			}
			inserted[i] = ins
		}
		meal, err = s.toMeal(ctx, q, copyRow, inserted)
		return err
	})
	if err != nil {
		return Meal{}, err
	}
	return meal, nil
}

// toMeal builds a Meal from a meals row and its (not-yet-necessarily-saved)
// meal_ingredients rows: it fetches every referenced ingredient once (via r,
// which is either the plain store or a transaction's *sqlc.Queries),
// validates each line's unit convertibility, and computes nutrition per
// serving in the same pass. Called both to build a real response and, from
// ReplaceIngredients, to validate a candidate list before writing it.
func (s *Meals) toMeal(ctx context.Context, r ingredientReader, row sqlc.Meal, miRows []sqlc.MealIngredient) (Meal, error) {
	items := make([]MealIngredient, len(miRows))
	totals := make(map[string]float64, len(allNutrientKeys))
	for _, k := range allNutrientKeys {
		totals[k] = 0
	}
	unknown := make(map[string]bool, len(allNutrientKeys))

	if len(miRows) > 0 {
		ids := uniqueIngredientIDs(miRows)
		ingredientRows, err := r.GetIngredientsForUser(ctx, sqlc.GetIngredientsForUserParams{Ids: ids, UserID: &row.OwnerID})
		if err != nil {
			return Meal{}, fmt.Errorf("get ingredients: %w", err)
		}
		if len(ingredientRows) != len(ids) {
			return Meal{}, ErrMealIngredientNotFound
		}
		byID := make(map[uuid.UUID]sqlc.Ingredient, len(ingredientRows))
		for _, ing := range ingredientRows {
			byID[ing.ID] = ing
		}

		nutrientRows, err := r.GetIngredientNutrients(ctx, ids)
		if err != nil {
			return Meal{}, fmt.Errorf("get ingredient nutrients: %w", err)
		}
		nutrientsByID := make(map[uuid.UUID]map[string]float64, len(ids))
		for _, n := range nutrientRows {
			if nutrientsByID[n.IngredientID] == nil {
				nutrientsByID[n.IngredientID] = map[string]float64{}
			}
			nutrientsByID[n.IngredientID][string(n.NutrientKey)] = n.AmountPer100g
		}

		for i, mi := range miRows {
			ing, ok := byID[mi.IngredientID]
			if !ok {
				return Meal{}, ErrMealIngredientNotFound
			}
			grams, err := gramsFor(mi.Quantity, mi.Unit, ing.GramsPerPiece, ing.DensityGPerMl)
			if err != nil {
				return Meal{}, err
			}
			lineNutrients := nutrientsByID[mi.IngredientID]
			for _, k := range allNutrientKeys {
				amount, ok := lineNutrients[k]
				if !ok {
					unknown[k] = true
					continue
				}
				totals[k] += grams / 100 * amount
			}
			items[i] = MealIngredient{
				ID: mi.ID, IngredientID: mi.IngredientID, IngredientName: ing.Name, IngredientCategory: ing.Category,
				Quantity: mi.Quantity, Unit: mi.Unit, Position: int(mi.Position),
			}
		}
	}

	perServing := make(map[string]float64, len(allNutrientKeys))
	for _, k := range allNutrientKeys {
		if unknown[k] {
			continue
		}
		perServing[k] = totals[k] / row.Servings
	}

	return Meal{
		ID: row.ID, Name: row.Name, Notes: row.Notes, Servings: row.Servings, SharedWithPartner: row.SharedWithPartner,
		Ingredients: items, NutritionPerServing: perServing, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}, nil
}

// gramsFor converts quantity in unit to grams, using the ingredient's
// conversion factors. unit is one of "g", "ml", "piece" — enforced by the
// meal_ingredients.unit CHECK constraint and, on the write path, the
// OpenAPI enum, so the default case is unreachable in practice.
func gramsFor(quantity float64, unit string, gramsPerPiece, densityGPerMl *float64) (float64, error) {
	switch unit {
	case "g":
		return quantity, nil
	case "ml":
		if densityGPerMl == nil {
			return 0, ErrUnitNotConvertible
		}
		return quantity * *densityGPerMl, nil
	case "piece":
		if gramsPerPiece == nil {
			return 0, ErrUnitNotConvertible
		}
		return quantity * *gramsPerPiece, nil
	default:
		return 0, fmt.Errorf("meals: unknown unit %q", unit)
	}
}

// uniqueIngredientIDs returns the distinct ingredient ids referenced by
// rows, in first-seen order. A meal may reference the same ingredient more
// than once (two lines of the same thing at different quantities/units).
func uniqueIngredientIDs(rows []sqlc.MealIngredient) []uuid.UUID {
	seen := make(map[uuid.UUID]bool, len(rows))
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		if !seen[r.IngredientID] {
			seen[r.IngredientID] = true
			ids = append(ids, r.IngredientID)
		}
	}
	return ids
}
```

`toRowLimit` is reused unchanged from `ingredients.go` (same package, already unexported there).

- [ ] **Step 4: Clean up the test file's setup noise**

As flagged in Step 1: in the final `meals_test.go`, each test calls `newIngredientsFixture`/`newMealsFixture` exactly once and uses the returned values directly. Remove the throwaway double-calls and unused `_ = svc`/`_ = ing` lines from `TestMealsNutritionIsComputedToTheGram` and every other test that has them; re-read the finished file before running it.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/service/... -run TestMeals -v`
Expected: PASS

Run: `cd backend && go build ./...`
Expected: builds cleanly.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/service/meals.go backend/internal/service/meals_test.go
git commit -m "feat(backend): add the meals service"
```

---

### Task 5: OpenAPI contract — `/meals`

**Files:**
- Modify: `openapi.yaml`
- Modify (generated, commit the output): `backend/internal/api/api.gen.go`

**Interfaces:**
- Produces (consumed by Task 6): `api.ListMealsParams{Cursor, *string; Limit *int}`, `api.CreateMealRequest{Name string; Notes nullable.Nullable[string]; Servings float64; SharedWithPartner *bool}`, `api.UpdateMealRequest{Name *string; Notes nullable.Nullable[string]; Servings *float64; SharedWithPartner *bool}`, `api.MealIngredientInput{IngredientId openapi_types.UUID; Quantity float64; Unit Unit}`, `api.ReplaceMealIngredientsRequest{Items []MealIngredientInput}`, `api.Unit` (a `string` type with generated constants for `g`/`ml`/`piece`), `api.MealIngredient{Id, IngredientId openapi_types.UUID; IngredientName string; IngredientCategory IngredientCategory; Quantity float64; Unit Unit; Position int}`, `api.Meal{Id openapi_types.UUID; Name string; Notes nullable.Nullable[string]; Servings float64; SharedWithPartner bool; Ingredients []MealIngredient; NutritionPerServing NutrientAmounts; CreatedAt, UpdatedAt time.Time}`, `api.MealSummary` (same shape as `Meal` minus `Ingredients`/`NutritionPerServing`), `api.MealList{Items []MealSummary; NextCursor nullable.Nullable[string]}`, and `api.ServerInterface` methods `ListMeals(w, r, params ListMealsParams)`, `CreateMeal(w, r)`, `GetMeal(w, r, id openapi_types.UUID)`, `UpdateMeal(w, r, id openapi_types.UUID)`, `DeleteMeal(w, r, id openapi_types.UUID)`, `ReplaceMealIngredients(w, r, id openapi_types.UUID)`, `CopyMeal(w, r, id openapi_types.UUID)`.

  All of the above is scratch-verified: this exact YAML addition was run through `redocly lint` (valid, the same pre-existing warnings only) and a real `oapi-codegen` v2.8.0 generate (matches the repo's pinned version) against a copy of the repo, and every struct field name, JSON tag and `ServerInterface` signature above was read from that generated output. `Meal.Notes`/`MealSummary.Notes` are `nullable.Nullable[string]` (the `notes` column is nullable, so both schemas mark it `nullable: true`); `UpdateMealRequest.Servings` is a plain `*float64`, **not** `nullable.Nullable[float64]` — `servings` is `NOT NULL` in the database and was deliberately not marked `nullable: true` in the schema, matching `UpdateIngredientRequest`'s treatment of `name`/`category` versus `grams_per_piece`/`density_g_per_ml`.

- [ ] **Step 1: Add the tag**

In `openapi.yaml`, after the `Ingredients` tag, add:

```yaml
  - name: Meals
    description: Meals built from ingredients, with nutrition computed on read.
```

- [ ] **Step 2: Add the five paths**

Immediately before `/healthz:`, add:

```yaml
  /meals:
    get:
      tags: [Meals]
      operationId: listMeals
      summary: List the caller's meals
      description: Cursor-paginated, alphabetical by name. Does not include ingredients or computed nutrition; fetch a single meal for those.
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
          description: The caller's meals.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/MealList'
        '400':
          $ref: '#/components/responses/BadRequest'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '429':
          $ref: '#/components/responses/TooManyRequests'
        '500':
          $ref: '#/components/responses/Problem'
    post:
      tags: [Meals]
      operationId: createMeal
      summary: Create a meal
      description: Creates the meal with an empty ingredient list. Add ingredients with `PUT /meals/{id}/ingredients`.
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/CreateMealRequest'
      responses:
        '201':
          description: The meal was created.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Meal'
        '400':
          $ref: '#/components/responses/BadRequest'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '429':
          $ref: '#/components/responses/TooManyRequests'
        '500':
          $ref: '#/components/responses/Problem'
  /meals/{id}:
    parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
          format: uuid
    get:
      tags: [Meals]
      operationId: getMeal
      summary: Get a meal
      description: Includes the ingredient list and nutrition per serving, computed on read.
      responses:
        '200':
          description: The meal.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Meal'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '404':
          $ref: '#/components/responses/NotFound'
        '409':
          $ref: '#/components/responses/Conflict'
        '429':
          $ref: '#/components/responses/TooManyRequests'
        '500':
          $ref: '#/components/responses/Problem'
    patch:
      tags: [Meals]
      operationId: updateMeal
      summary: Update a meal
      description: Fields that are absent are left unchanged. Only the owner can update a meal.
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/UpdateMealRequest'
      responses:
        '200':
          description: The updated meal.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Meal'
        '400':
          $ref: '#/components/responses/BadRequest'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '404':
          $ref: '#/components/responses/NotFound'
        '409':
          $ref: '#/components/responses/Conflict'
        '429':
          $ref: '#/components/responses/TooManyRequests'
        '500':
          $ref: '#/components/responses/Problem'
    delete:
      tags: [Meals]
      operationId: deleteMeal
      summary: Delete a meal
      responses:
        '204':
          description: The meal was deleted.
        '401':
          $ref: '#/components/responses/Unauthorized'
        '404':
          $ref: '#/components/responses/NotFound'
        '429':
          $ref: '#/components/responses/TooManyRequests'
        '500':
          $ref: '#/components/responses/Problem'
  /meals/{id}/ingredients:
    parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
          format: uuid
    put:
      tags: [Meals]
      operationId: replaceMealIngredients
      summary: Replace a meal's ingredient list
      description: Atomically replaces the full ingredient list. Every row is validated (the ingredient must exist and be visible to the caller, and its unit must be convertible) before anything is written; an empty `items` array clears the list.
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/ReplaceMealIngredientsRequest'
      responses:
        '200':
          description: The updated meal.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Meal'
        '400':
          $ref: '#/components/responses/BadRequest'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '404':
          $ref: '#/components/responses/NotFound'
        '409':
          $ref: '#/components/responses/Conflict'
        '429':
          $ref: '#/components/responses/TooManyRequests'
        '500':
          $ref: '#/components/responses/Problem'
  /meals/{id}/copy:
    parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
          format: uuid
    post:
      tags: [Meals]
      operationId: copyMeal
      summary: Copy a meal
      description: Creates a new meal, owned by the caller, with the same name, notes, servings and ingredients. The copy always starts with `shared_with_partner` false.
      responses:
        '201':
          description: The copy.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Meal'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '404':
          $ref: '#/components/responses/NotFound'
        '409':
          $ref: '#/components/responses/Conflict'
        '429':
          $ref: '#/components/responses/TooManyRequests'
        '500':
          $ref: '#/components/responses/Problem'
```

- [ ] **Step 3: Add the schemas**

In `components.schemas`, after `IngredientList` (before `responses:`), add:

```yaml
    Unit:
      type: string
      enum: [g, ml, piece]
    MealIngredient:
      type: object
      required: [id, ingredient_id, ingredient_name, ingredient_category, quantity, unit, position]
      properties:
        id:
          type: string
          format: uuid
        ingredient_id:
          type: string
          format: uuid
        ingredient_name:
          type: string
        ingredient_category:
          $ref: '#/components/schemas/IngredientCategory'
        quantity:
          type: number
          format: double
        unit:
          $ref: '#/components/schemas/Unit'
        position:
          type: integer
    Meal:
      type: object
      required: [id, name, notes, servings, shared_with_partner, ingredients, nutrition_per_serving, created_at, updated_at]
      properties:
        id:
          type: string
          format: uuid
        name:
          type: string
        notes:
          type: string
          nullable: true
        servings:
          type: number
          format: double
        shared_with_partner:
          type: boolean
        ingredients:
          type: array
          items:
            $ref: '#/components/schemas/MealIngredient'
        nutrition_per_serving:
          $ref: '#/components/schemas/NutrientAmounts'
        created_at:
          type: string
          format: date-time
        updated_at:
          type: string
          format: date-time
    MealSummary:
      type: object
      required: [id, name, notes, servings, shared_with_partner, created_at, updated_at]
      properties:
        id:
          type: string
          format: uuid
        name:
          type: string
        notes:
          type: string
          nullable: true
        servings:
          type: number
          format: double
        shared_with_partner:
          type: boolean
        created_at:
          type: string
          format: date-time
        updated_at:
          type: string
          format: date-time
    CreateMealRequest:
      type: object
      additionalProperties: false
      required: [name, servings]
      properties:
        name:
          type: string
          minLength: 1
          maxLength: 200
        notes:
          type: string
          nullable: true
          maxLength: 2000
        servings:
          type: number
          format: double
          exclusiveMinimum: true
          minimum: 0
          maximum: 1000
        shared_with_partner:
          type: boolean
    UpdateMealRequest:
      type: object
      additionalProperties: false
      properties:
        name:
          type: string
          minLength: 1
          maxLength: 200
        notes:
          type: string
          nullable: true
          maxLength: 2000
        servings:
          type: number
          format: double
          exclusiveMinimum: true
          minimum: 0
          maximum: 1000
        shared_with_partner:
          type: boolean
    MealIngredientInput:
      type: object
      additionalProperties: false
      required: [ingredient_id, quantity, unit]
      properties:
        ingredient_id:
          type: string
          format: uuid
        quantity:
          type: number
          format: double
          exclusiveMinimum: true
          minimum: 0
          maximum: 100000
        unit:
          $ref: '#/components/schemas/Unit'
    ReplaceMealIngredientsRequest:
      type: object
      additionalProperties: false
      required: [items]
      properties:
        items:
          type: array
          maxItems: 200
          items:
            $ref: '#/components/schemas/MealIngredientInput'
    MealList:
      type: object
      required: [items, next_cursor]
      properties:
        items:
          type: array
          items:
            $ref: '#/components/schemas/MealSummary'
        next_cursor:
          type: string
          nullable: true
```

- [ ] **Step 4: Lint and regenerate**

Run: `make lint-api`
Expected: valid, same pre-existing warnings only, no new ones.

Run: `make generate`
Expected: `backend/internal/api/api.gen.go` is regenerated; `git diff --stat` shows only that file (Tasks 1–4 are already committed).

Run: `cd backend && go build ./...`
Expected: builds cleanly (nothing implements the new `ServerInterface` methods yet, but `api.gen.go` alone must compile).

- [ ] **Step 5: Commit**

```bash
git add openapi.yaml backend/internal/api/api.gen.go
git commit -m "feat(api): add the meals endpoints to the contract"
```

---

### Task 6: Meals handlers

**Files:**
- Create: `backend/internal/httpapi/meals.go`
- Modify: `backend/internal/httpapi/problem.go`
- Modify: `backend/internal/httpapi/account.go` (extend `writeServiceError`)
- Modify: `backend/internal/httpapi/server.go` (add `meals MealsService` field)
- Modify: `backend/internal/httpapi/router.go` (wire `Deps.Meals`)

**Interfaces:**
- Consumes: `service.Meals` and its types from Task 4; `api.*` types from Task 5; the existing `toNullable`, `decodeJSON`, `requireUser`, `writeJSON` helpers from `account.go`; `nutrientsToAPI` from `nutrients.go` (Meal's `nutrition_per_serving` reuses the exact same `NutrientAmounts` schema as an ingredient's `nutrients`).
- Produces: `httpapi.MealsService` interface (consumed by `router.go`'s `Deps.Meals` and by Task 7's tests).

- [ ] **Step 1: Add the new problem codes**

In `backend/internal/httpapi/problem.go`, add to the `Code*` constants (alongside `CodeIngredientInUse` from Task 3):

```go
	CodeInvalidIngredient  = "invalid_ingredient"
	CodeUnitNotConvertible = "unit_not_convertible"
```

- [ ] **Step 2: Add the `writeServiceError` cases**

In `backend/internal/httpapi/account.go`, in `writeServiceError`, add (next to the existing `ErrIngredientNotFound`/`ErrIngredientInUse` cases):

```go
	case errors.Is(err, service.ErrMealNotFound):
		WriteProblem(w, http.StatusNotFound, CodeNotFound, "")
	case errors.Is(err, service.ErrMealIngredientNotFound):
		WriteProblem(w, http.StatusBadRequest, CodeInvalidIngredient, "")
	case errors.Is(err, service.ErrUnitNotConvertible):
		WriteProblem(w, http.StatusConflict, CodeUnitNotConvertible, "")
```

- [ ] **Step 3: Write the handlers**

Create `backend/internal/httpapi/meals.go`:

```go
package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/oapi-codegen/nullable"

	"github.com/InzKazik/mealplanner/backend/internal/api"
	"github.com/InzKazik/mealplanner/backend/internal/service"
)

// MealsService is what the meals handlers need from the meals service.
type MealsService interface {
	Create(ctx context.Context, ownerID uuid.UUID, in service.CreateMealInput) (service.Meal, error)
	Get(ctx context.Context, ownerID, id uuid.UUID) (service.Meal, error)
	Update(ctx context.Context, ownerID, id uuid.UUID, in service.UpdateMealInput) (service.Meal, error)
	Delete(ctx context.Context, ownerID, id uuid.UUID) error
	List(ctx context.Context, ownerID uuid.UUID, in service.ListMealsInput) (service.MealPage, error)
	ReplaceIngredients(ctx context.Context, ownerID, id uuid.UUID, items []service.MealIngredientInput) (service.Meal, error)
	Copy(ctx context.Context, callerID, id uuid.UUID) (service.Meal, error)
}

const defaultMealLimit = 20

func (s *server) ListMeals(w http.ResponseWriter, r *http.Request, params api.ListMealsParams) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	limit := defaultMealLimit
	if params.Limit != nil {
		limit = *params.Limit
	}
	var cursor *service.MealCursor
	if params.Cursor != nil {
		c, ok := decodeMealCursor(*params.Cursor)
		if !ok {
			WriteValidationProblem(w, "cursor is invalid", []FieldError{{Field: "cursor", Code: FieldInvalidForm}})
			return
		}
		cursor = &c
	}
	page, err := s.meals.List(r.Context(), userID, service.ListMealsInput{Cursor: cursor, Limit: limit})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	next := ""
	if page.NextCursor != nil {
		next = encodeMealCursor(*page.NextCursor)
	}
	writeJSON(w, http.StatusOK, toMealList(page.Items, next))
}

func (s *server) CreateMeal(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.CreateMealRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in := service.CreateMealInput{
		Name: req.Name, Notes: nullableStringToPtr(req.Notes), Servings: req.Servings,
	}
	if req.SharedWithPartner != nil {
		in.SharedWithPartner = *req.SharedWithPartner
	}
	meal, err := s.meals.Create(r.Context(), userID, in)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAPIMeal(meal))
}

func (s *server) GetMeal(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	meal, err := s.meals.Get(r.Context(), userID, id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIMeal(meal))
}

func (s *server) UpdateMeal(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.UpdateMealRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in := service.UpdateMealInput{
		Name: req.Name, Notes: toOptionalString(req.Notes), Servings: req.Servings, SharedWithPartner: req.SharedWithPartner,
	}
	meal, err := s.meals.Update(r.Context(), userID, id, in)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIMeal(meal))
}

func (s *server) DeleteMeal(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	if err := s.meals.Delete(r.Context(), userID, id); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) ReplaceMealIngredients(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.ReplaceMealIngredientsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	items := make([]service.MealIngredientInput, len(req.Items))
	for i, it := range req.Items {
		items[i] = service.MealIngredientInput{IngredientID: it.IngredientId, Quantity: it.Quantity, Unit: string(it.Unit)}
	}
	meal, err := s.meals.ReplaceIngredients(r.Context(), userID, id, items)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIMeal(meal))
}

func (s *server) CopyMeal(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	meal, err := s.meals.Copy(r.Context(), userID, id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAPIMeal(meal))
}

func nullableStringToPtr(n nullable.Nullable[string]) *string {
	if !n.IsSpecified() || n.IsNull() {
		return nil
	}
	v := n.MustGet()
	return &v
}

func toOptionalString(n nullable.Nullable[string]) service.Optional[string] {
	switch {
	case !n.IsSpecified():
		return service.Optional[string]{}
	case n.IsNull():
		return service.Set[string](nil)
	default:
		v := n.MustGet()
		return service.Set(&v)
	}
}

func toNullableString(v *string) nullable.Nullable[string] {
	if v == nil {
		return nullable.NewNullNullable[string]()
	}
	return nullable.NewNullableWithValue(*v)
}

func toMealList(items []service.MealSummary, nextCursor string) api.MealList {
	list := api.MealList{Items: make([]api.MealSummary, len(items))}
	for i, m := range items {
		list.Items[i] = toAPIMealSummary(m)
	}
	if nextCursor == "" {
		list.NextCursor = nullable.NewNullNullable[string]()
	} else {
		list.NextCursor = nullable.NewNullableWithValue(nextCursor)
	}
	return list
}

func toAPIMealSummary(m service.MealSummary) api.MealSummary {
	return api.MealSummary{
		Id: m.ID, Name: m.Name, Notes: toNullableString(m.Notes), Servings: m.Servings,
		SharedWithPartner: m.SharedWithPartner, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

func toAPIMeal(m service.Meal) api.Meal {
	ingredients := make([]api.MealIngredient, len(m.Ingredients))
	for i, mi := range m.Ingredients {
		ingredients[i] = api.MealIngredient{
			Id: mi.ID, IngredientId: mi.IngredientID, IngredientName: mi.IngredientName,
			IngredientCategory: api.IngredientCategory(mi.IngredientCategory),
			Quantity:           mi.Quantity, Unit: api.Unit(mi.Unit), Position: mi.Position,
		}
	}
	return api.Meal{
		Id: m.ID, Name: m.Name, Notes: toNullableString(m.Notes), Servings: m.Servings,
		SharedWithPartner: m.SharedWithPartner, Ingredients: ingredients,
		NutritionPerServing: nutrientsToAPI(m.NutritionPerServing),
		CreatedAt:           m.CreatedAt, UpdatedAt: m.UpdatedAt,
	}
}

type mealCursorPayload struct {
	Name string    `json:"n"`
	ID   uuid.UUID `json:"i"`
}

func encodeMealCursor(c service.MealCursor) string {
	b, _ := json.Marshal(mealCursorPayload{Name: c.Name, ID: c.ID})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeMealCursor(s string) (service.MealCursor, bool) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return service.MealCursor{}, false
	}
	var p mealCursorPayload
	if err := json.Unmarshal(b, &p); err != nil || p.Name == "" || p.ID == uuid.Nil {
		return service.MealCursor{}, false
	}
	return service.MealCursor{Name: p.Name, ID: p.ID}, true
}
```

Note: `GetMeal`/`UpdateMeal`/`DeleteMeal`/`ReplaceMealIngredients`/`CopyMeal`'s `id` parameter is declared here as `uuid.UUID`, matching `UpdateIngredient`/`DeleteIngredient`'s existing precedent (`openapi_types.UUID` is a genuine alias for `uuid.UUID`).

`nutrientsToAPI` is reused unchanged from `nutrients.go` — `Meal.NutritionPerServing` is a `map[string]float64` keyed by the same `service.Nutrient*` constants as `Ingredient.Nutrients`, so the exact same converter applies; a key missing from the map (the "unknown, propagated" case from Task 4) renders as an explicit JSON `null`, exactly like an ingredient's missing nutrient does today.

- [ ] **Step 4: Wire the new service into `server` and the router**

In `backend/internal/httpapi/server.go`, add a field:

```go
type server struct {
	logger      *slog.Logger
	ready       func(context.Context) error
	auth        AuthService
	ingredients IngredientsService
	meals       MealsService
}
```

In `backend/internal/httpapi/router.go`, add `Meals MealsService` to `Deps`, require it in the panic check, and pass it into `server{}`:

```go
	// Meals implements the meals endpoints.
	Meals MealsService
```

```go
	if d.Logger == nil || d.Ready == nil || d.Auth == nil || d.Ingredients == nil || d.Meals == nil || d.Tokens == nil ||
		d.WebOrigin == "" || d.WebOrigin == "*" {
		panic("httpapi: Deps.Logger, Ready, Auth, Ingredients, Meals and Tokens are required, and WebOrigin must be a single origin (not empty or *)")
	}
```

```go
	srv := &server{logger: d.Logger, ready: d.Ready, auth: d.Auth, ingredients: d.Ingredients, meals: d.Meals}
```

(Keep every other line of `router.go` and `server.go` unchanged.)

- [ ] **Step 5: Build**

Run: `cd backend && go build ./...`
Expected: builds cleanly. `var _ api.ServerInterface = (*server)(nil)` in `server.go` now also checks the seven new methods.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/httpapi/
git commit -m "feat(backend): implement the meals handlers"
```

---

### Task 7: Wire `cmd/api` and write end-to-end tests

**Files:**
- Modify: `backend/cmd/api/main.go` (construct and pass `service.NewMeals`)
- Modify: `backend/internal/httpapi/contract_test.go` (extend the shared test router with a no-op `stubMeals`, mirroring `stubIngredients`)
- Create: `backend/internal/httpapi/meals_flow_test.go`

**Interfaces:**
- Consumes: everything from Tasks 4–6, plus the existing `newTestRouter`, `contract`, `decodeAs[T]`, `withBody`, `withBearer`, `withInvalidRequest` helpers already in the `httpapi_test` package.

- [ ] **Step 1: Wire the service in `cmd/api/main.go`**

Next to `ingredients := service.NewIngredients(st)`, add:

```go
	meals := service.NewMeals(st)
```

Add `Meals: meals,` to the `httpapi.Deps{...}` literal, alongside `Ingredients: ingredients,`.

- [ ] **Step 2: Add a stub to the shared test router**

In `backend/internal/httpapi/contract_test.go`, add a no-op stub next to `stubIngredients` and wire it into `newTestRouter`'s default `Deps`:

```go
// stubMeals panics on any call, so tests that must not reach the meals
// service fail loudly if they do.
type stubMeals struct{ httpapi.MealsService }
```

In `newTestRouter`, add `Meals: stubMeals{},` to the `httpapi.Deps{...}` literal (alongside `Ingredients: stubIngredients{}`). Also add `d.Meals == nil` and a `"no meals"` case to `TestNewRouterPanicsWithoutRequiredDependencies`'s `tests` map and to its `full` literal, mirroring the existing `"no ingredients"` case exactly.

- [ ] **Step 3: Write the end-to-end test**

Create `backend/internal/httpapi/meals_flow_test.go`, modeled on `ingredients_flow_test.go`'s `newIngredientsRouter`/`TestIngredientsLifecycle`:

```go
package httpapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/api"
	"github.com/InzKazik/mealplanner/backend/internal/db"
	"github.com/InzKazik/mealplanner/backend/internal/httpapi"
	"github.com/InzKazik/mealplanner/backend/internal/service"
	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"
	"github.com/InzKazik/mealplanner/backend/internal/testutil"
)

func newMealsRouter(t *testing.T) (router http.Handler, token1, token2 string) {
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

	router = newTestRouter(t, func(d *httpapi.Deps) {
		d.Ingredients = service.NewIngredients(st)
		d.Meals = service.NewMeals(st)
		d.Tokens = stubTwoUserTokens{u1: u1.ID, u2: u2.ID}
		d.Limits = httpapi.RateLimits{AuthPerMinute: 1000, UserPerMinute: 1000}
	})
	return router, "user1-token", "user2-token"
}

func TestMealsLifecycle(t *testing.T) {
	router, token1, token2 := newMealsRouter(t)

	rec := contract(t, router, http.MethodPost, "/ingredients", withBearer(token1),
		withBody(`{"name":"Rice","category":"grains_bread","nutrients":{"calories":130}}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create ingredient: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rice := decodeAs[api.Ingredient](t, rec)

	rec = contract(t, router, http.MethodPost, "/meals", withBearer(token1),
		withBody(`{"name":"Rice Bowl","servings":2}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create meal: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	meal := decodeAs[api.Meal](t, rec)
	if len(meal.Ingredients) != 0 {
		t.Errorf("a freshly created meal has ingredients = %+v, want none", meal.Ingredients)
	}
	if !meal.NutritionPerServing.Calories.IsSpecified() || meal.NutritionPerServing.Calories.MustGet() != 0 {
		t.Errorf("a freshly created meal's calories = %+v, want 0", meal.NutritionPerServing.Calories)
	}

	rec = contract(t, router, http.MethodPut, "/meals/"+meal.Id.String()+"/ingredients", withBearer(token1),
		withBody(`{"items":[{"ingredient_id":"`+rice.Id.String()+`","quantity":200,"unit":"g"}]}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("replace ingredients: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	meal = decodeAs[api.Meal](t, rec)
	if len(meal.Ingredients) != 1 || meal.Ingredients[0].IngredientId != rice.Id {
		t.Errorf("meal ingredients = %+v", meal.Ingredients)
	}
	// 200g rice at 130 kcal/100g = 260 kcal total, /2 servings = 130/serving.
	if got := meal.NutritionPerServing.Calories.MustGet(); got != 130 {
		t.Errorf("calories per serving = %v, want 130", got)
	}

	// User 2 cannot see, update or delete user 1's meal.
	rec = contract(t, router, http.MethodGet, "/meals/"+meal.Id.String(), withBearer(token2))
	if rec.Code != http.StatusNotFound {
		t.Errorf("user 2 GET: status = %d, want 404", rec.Code)
	}
	rec = contract(t, router, http.MethodPatch, "/meals/"+meal.Id.String(), withBearer(token2), withBody(`{"name":"Hijack"}`))
	if rec.Code != http.StatusNotFound {
		t.Errorf("user 2 PATCH: status = %d, want 404", rec.Code)
	}

	// Copying gives user 1 a second, independent meal.
	rec = contract(t, router, http.MethodPost, "/meals/"+meal.Id.String()+"/copy", withBearer(token1))
	if rec.Code != http.StatusCreated {
		t.Fatalf("copy: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	copyMeal := decodeAs[api.Meal](t, rec)
	if copyMeal.Id == meal.Id || len(copyMeal.Ingredients) != 1 {
		t.Errorf("copy = %+v", copyMeal)
	}
	if copyMeal.SharedWithPartner {
		t.Error("copy has shared_with_partner = true, want false")
	}

	// Deleting an ingredient in use by a meal is blocked.
	rec = contract(t, router, http.MethodDelete, "/ingredients/"+rice.Id.String(), withBearer(token1))
	if rec.Code != http.StatusConflict {
		t.Errorf("delete ingredient in use: status = %d, want 409", rec.Code)
	}
	if got := problemCode(t, rec); got != "ingredient_in_use" {
		t.Errorf("problem code = %q, want ingredient_in_use", got)
	}

	rec = contract(t, router, http.MethodDelete, "/meals/"+meal.Id.String(), withBearer(token1))
	if rec.Code != http.StatusNoContent {
		t.Errorf("delete meal: status = %d, want 204", rec.Code)
	}
	rec = contract(t, router, http.MethodDelete, "/meals/"+copyMeal.Id.String(), withBearer(token1))
	if rec.Code != http.StatusNoContent {
		t.Errorf("delete meal copy: status = %d, want 204", rec.Code)
	}

	// Now that no meal references it, deleting the ingredient succeeds.
	rec = contract(t, router, http.MethodDelete, "/ingredients/"+rice.Id.String(), withBearer(token1))
	if rec.Code != http.StatusNoContent {
		t.Errorf("delete ingredient once unused: status = %d, want 204", rec.Code)
	}
}

func TestMealsReplaceIngredientsWithAnUnknownIngredientIsRejected(t *testing.T) {
	router, token1, _ := newMealsRouter(t)

	rec := contract(t, router, http.MethodPost, "/meals", withBearer(token1), withBody(`{"name":"Meal","servings":1}`))
	meal := decodeAs[api.Meal](t, rec)

	rec = contract(t, router, http.MethodPut, "/meals/"+meal.Id.String()+"/ingredients", withBearer(token1),
		withBody(`{"items":[{"ingredient_id":"00000000-0000-0000-0000-000000000001","quantity":100,"unit":"g"}]}`))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if got := problemCode(t, rec); got != "invalid_ingredient" {
		t.Errorf("problem code = %q, want invalid_ingredient", got)
	}
}

func TestMealsListPaginatesThroughTheContract(t *testing.T) {
	router, token1, _ := newMealsRouter(t)
	for _, name := range []string{"Breakfast", "Dinner", "Lunch"} {
		rec := contract(t, router, http.MethodPost, "/meals", withBearer(token1),
			withBody(`{"name":"`+name+`","servings":1}`))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s: status = %d", name, rec.Code)
		}
	}

	var names []string
	path := "/meals?limit=1"
	for {
		rec := contract(t, router, http.MethodGet, path, withBearer(token1))
		if rec.Code != http.StatusOK {
			t.Fatalf("list %s: status = %d", path, rec.Code)
		}
		list := decodeAs[api.MealList](t, rec)
		for _, m := range list.Items {
			names = append(names, m.Name)
		}
		if !list.NextCursor.IsSpecified() || list.NextCursor.IsNull() {
			break
		}
		path = "/meals?limit=1&cursor=" + list.NextCursor.MustGet()
		if len(names) > 10 {
			t.Fatal("pagination did not terminate")
		}
	}
	if want := []string{"Breakfast", "Dinner", "Lunch"}; len(names) != 3 || names[0] != want[0] || names[1] != want[1] || names[2] != want[2] {
		t.Errorf("paginated names = %v, want %v", names, want)
	}
}
```

`stubTwoUserTokens` is reused unchanged from `ingredients_flow_test.go` (same `httpapi_test` package).

- [ ] **Step 4: Run the tests**

Run: `cd backend && go test ./... -v 2>&1 | tail -100`
Expected: all PASS, including `TestMealsLifecycle`, `TestMealsReplaceIngredientsWithAnUnknownIngredientIsRejected`, `TestMealsListPaginatesThroughTheContract`, and every pre-existing test.

- [ ] **Step 5: Commit**

```bash
git add backend/cmd/api/main.go backend/internal/httpapi/contract_test.go backend/internal/httpapi/meals_flow_test.go
git commit -m "feat(backend): wire the meals service into cmd/api and add end-to-end coverage"
```

---

### Task 8: Documentation and final checks

**Files:**
- Modify: `backend/CLAUDE.md`

**Interfaces:** none (documentation only).

- [ ] **Step 1: Update `backend/CLAUDE.md`**

In "Behaviour worth knowing", add two entries after the existing "Nutrients use two schemas on purpose" line:

```
- **Meal nutrition is per serving, and a missing nutrient propagates, not zeros out.** `Meal.NutritionPerServing` is the sum of every ingredient line's contribution divided by `servings`; multiply by `servings` client-side for the meal total. A nutrient key is `null` in the response if *any* ingredient in the meal has no value for it — treating an unknown amount as zero would understate the true total, which a meal-planning app cannot do quietly. A meal with zero ingredients reports `0` for every key (a well-defined empty sum, a different case from "unknown").
- **Editing an ingredient can retroactively break a meal that references it.** `Ingredients.Update` has no awareness of meals, so clearing a custom ingredient's `grams_per_piece` or `density_g_per_ml` after a meal has referenced it via `piece`/`ml` succeeds — and the meal's `GET`/`PATCH`/`PUT .../ingredients`/`POST .../copy` then return `409 unit_not_convertible` until that meal's ingredient list is fixed via `PUT /meals/{id}/ingredients`. Deleting the ingredient outright is blocked instead (`409 ingredient_in_use`, via the `meal_ingredients_ingredient_id_fkey` foreign key) — only edits that leave the ingredient row in place can cause this.
```

In "Decide before the domain plans", the existing bullet about `DELETE /me` re-authentication, registration revealing emails, and argon2id memory stay as-is (still open, unrelated to this plan).

In "Not built yet", replace the line `- The domain beyond ingredients: meals, diets, plan, shopping lists, partners (later plans). The meals plan must add a `meal_ingredients` foreign key to `ingredients` and translate its violation into `409 ingredient_in_use` on delete (see the note in `internal/store/queries/ingredients.sql`) — `DeleteIngredient` does not check for that yet because nothing references ingredients until then.` with:

```
- The domain beyond meals: diets, plan, shopping lists, partners (later plans). **Meals have no partner visibility yet**: `meals.shared_with_partner` is stored, but every meals read checks `owner_id` only, because the `partnerships` table does not exist until the partner plan (backend build order item 6, after shopping lists). When that plan lands, `Meals.Get`/`List`/`Copy` (and their `GetMealForUser`/`ListMealsForUser` queries) need an active-partner lookup added alongside the owner check, and `Copy` needs to decide whether a partner may copy a shared meal (the spec's sharing rule, §3.6, says partners get read-only + copy access to meals — this plan only implements that for the owner).
```

- [ ] **Step 2: Run everything CI runs**

Run: `make check`
Expected: PASS (lints `openapi.yaml`, vets and tests the backend, runs golangci-lint, fails if generated code is stale)

Run: `make check-generated`
Expected: clean (no diff)

- [ ] **Step 3: Commit**

```bash
git add backend/CLAUDE.md
git commit -m "docs(backend): document the meals domain and its deferred partner visibility"
```

---

## Self-Review

- **Spec coverage:** §3.3 (meals/meal_ingredients schema, nutrition computed on read, unit conversion, `unit_not_convertible`) → Tasks 1, 4. §3.6 (sharing rule) → Task 4's owner-only `Meals`, explicitly scoped down from full partner sharing because `partnerships` doesn't exist yet (documented in Global Constraints and Task 8). §4.1 (`GET/POST meals`, `GET/PATCH/DELETE meals/{id}` with computed nutrition, `PUT meals/{id}/ingredients` atomic replace, `POST meals/{id}/copy`) → Tasks 5–6. §4.2 (cursor pagination, RFC 9457 errors) → Task 6 (list pagination mirrors ingredients; `unit_not_convertible`/`ingredient_in_use` are the two new stable codes the spec names by example in §4.2, both implemented). §6 (nutrition golden test to the gram, sharing/ownership tests, TDD) → Task 4's test file. The ingredients plan's explicitly deferred `ingredient_in_use` check is closed by Task 3, entirely within the ingredients domain as that plan anticipated.
- **Placeholder scan:** none — every step has real, complete code or SQL. The two "clean up the test file's setup noise" steps (Task 4 Step 4, mirroring the ingredients plan's Task 3 Step 5) are not placeholders: they show the actual redundant code the earlier step introduces and say exactly what to delete, the same pattern the ingredients plan used for the same reason (the first draft of a test needing a fixture's return values collides with needing to explain the fixture itself).
- **Type consistency:** `service.Meal.NutritionPerServing`, `service.MealIngredient`, and `httpapi`'s `toAPIMeal`/`toAPIMealSummary` conversions were checked against the exact struct fields Task 4 defines and the exact generated types Task 5 scratch-verified — in particular, `GetIngredientsForUserParams.UserID` being `*uuid.UUID` (not a plain `uuid.UUID`) was caught by actually running `sqlc generate` during planning, not assumed from the ingredients plan's narrower single-arg queries, and Task 4's `toMeal` passes `&row.OwnerID` accordingly. `sqlc.MealIngredient.Position` is `int32`; `api.MealIngredient.Position` is a plain `int` (oapi-codegen's default for an untyped `integer` schema) — Task 6's `toAPIMeal` converts with `int(mi.Position)` via the intermediate `service.MealIngredient.Position int`, and `ReplaceIngredients` converts back with `int32(i)` when building candidate rows from the request array's index.
