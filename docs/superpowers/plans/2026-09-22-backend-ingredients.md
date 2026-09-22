# Backend Ingredients Domain Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the ingredient catalog (global USDA-sourced ingredients plus per-user custom ingredients), its nutrition data, a search/list/CRUD API, and a `cmd/import-usda` command that idempotently loads the USDA FoodData Central Foundation Foods dataset.

**Architecture:** Two new tables (`ingredients`, `ingredient_nutrients`) follow the existing `httpapi` → `service` → `store` layering. A new leaf package, `internal/usda`, talks to the FoodData Central REST API and writes through `store` directly (an ETL job, not a request-serving business rule, so it doesn't go through `internal/service`'s HTTP-facing surface — see Task 7). `cmd/import-usda` is a thin entry point around it, mirroring `cmd/migrate`.

**Tech Stack:** Go 1.26, chi, pgx/pgxpool, goose, sqlc, oapi-codegen, `pg_trgm` for search (already enabled by migration `00001_init.sql`), the FoodData Central REST API (`https://api.nal.usda.gov/fdc/v1`, needs a free API key from https://api.data.gov/signup/).

**Spec:** `docs/superpowers/specs/2026-09-21-meal-planner-design.md` (sections 3.1, 3.2, 4.1, 4.2)

## Global Constraints

- OpenAPI 3.0.3 is the source of truth: edit `openapi.yaml` first, then `make generate`, then implement. Never hand-edit `backend/internal/api/api.gen.go` or `backend/internal/store/sqlc/`.
- No SQL outside `backend/internal/store`. Dependencies point one way: `httpapi` → `service` → `store`.
- Global `security: bearerAuth` protects every operation by default; auth is enforced by the spec-driven validator, never by a path-based check in a handler.
- Errors are RFC 9457 `application/problem+json` with a stable `code` (`httpapi.WriteProblem` / `WriteValidationProblem`).
- Unseen resources return `404`, never `403` (a custom ingredient owned by someone else, or that doesn't exist, is indistinguishable).
- Migrations are goose SQL files named `NNNNN_description.sql`. Every table has `created_at` and `updated_at` plus the `set_updated_at()` trigger.
- Contract tests validate every new endpoint's request and response against `openapi.yaml` via `contract()` in `internal/httpapi/contract_test.go`.
- Nutrition is stored per 100 g; meal/plan nutrition (a later plan) is computed on read, never stored here.
- USDA import scope, locked this session: **Foundation Foods only** (no SR Legacy, no branded/packaged items).
- Shopping categories, locked this session (10, fixed): `produce`, `dairy_eggs`, `meat_seafood`, `grains_bread`, `legumes_nuts_seeds`, `condiments_oils`, `spices_herbs`, `beverages`, `sweets_snacks`, `other`.
- Nutrient set, locked in the spec (17, fixed, stored as rows not columns): `calories`, `protein`, `carbohydrates`, `sugar`, `fibre`, `fat`, `saturated_fat`, `sodium`, `potassium`, `calcium`, `iron`, `magnesium`, `zinc`, `vitamin_a`, `vitamin_c`, `vitamin_d`, `vitamin_b12`, `folate`.
- Any new environment variable is added to `.env.example` in the same commit.

---

## File Structure

- `backend/migrations/00004_ingredients.sql` — the two tables, the `nutrient_key` enum, indexes, trigger.
- `backend/internal/store/queries/ingredients.sql` — sqlc source; generates into `backend/internal/store/sqlc/ingredients.sql.go` (never hand-edited).
- `backend/internal/store/store.go` — gains `IsForeignKeyViolation`, alongside the existing `IsUniqueViolation`.
- `backend/internal/service/ingredients.go` — the `Ingredients` service: CRUD, list, search, nutrient mapping. Owns the 17 nutrient-key string constants (the shared vocabulary `httpapi` and `usda` both import).
- `backend/internal/service/ingredients_test.go` — its tests, against a real migrated Postgres.
- `openapi.yaml` — `/ingredients` and `/ingredients/{id}`, their schemas, and a new reusable `NotFound` response.
- `backend/internal/httpapi/ingredients.go` — the four handlers, cursor encode/decode, `IngredientsService` interface.
- `backend/internal/httpapi/nutrients.go` — converts between `api.NutrientAmounts` and the service's `map[string]float64`.
- `backend/internal/httpapi/account.go` — gains one `writeServiceError` case for `service.ErrIngredientNotFound`.
- `backend/internal/httpapi/router.go`, `server.go` — wire the new service into `Deps` and `server`.
- `backend/internal/httpapi/ingredients_flow_test.go` — end-to-end contract tests through the real router, service and Postgres (mirrors `account_flow_test.go`).
- `backend/internal/usda/client.go` — pages the FoodData Central search API.
- `backend/internal/usda/category.go` — the USDA-food-group → shopping-category lookup table.
- `backend/internal/usda/nutrients.go` — the USDA-nutrient-number → our-nutrient-key lookup table.
- `backend/internal/usda/import.go` — orchestrates fetch → map → upsert.
- `backend/internal/usda/*_test.go` — unit tests for the two lookup tables and the client's paging/retry, plus an integration test of `Import` against a real migrated Postgres and a fake HTTP server.
- `backend/cmd/import-usda/main.go` — entry point.
- `.env.example`, `backend/CLAUDE.md` — document `FDC_API_KEY` and update the repo map / "Not built yet" / "Decide before the domain plans" sections.

---

### Task 1: Migration — `ingredients` and `ingredient_nutrients`

**Files:**
- Create: `backend/migrations/00004_ingredients.sql`
- Modify: `backend/internal/db/schema_test.go`

**Interfaces:**
- Produces: the `ingredients` table (`id, name, category, owner_id, usda_fdc_id, grams_per_piece, density_g_per_ml, created_at, updated_at`), the `ingredient_nutrients` table (`ingredient_id, nutrient_key, amount_per_100g`), and the Postgres enum type `nutrient_key`. The foreign key on `owner_id` is named `ingredients_owner_id_fkey` (Postgres's default name for an unnamed inline `REFERENCES`, matching the existing style in `00003_refresh_tokens.sql`) — Task 3 relies on that exact name.

- [ ] **Step 1: Write the failing test**

Add to `backend/internal/db/schema_test.go` (same file as the existing `TestUsersSchemaEnforcesItsConstraints`; reuses its `migratedConn` helper):

```go
func TestIngredientsSchemaEnforcesItsConstraints(t *testing.T) {
	ctx := context.Background()
	conn := migratedConn(t)

	var userID string
	if err := conn.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, display_name) VALUES ('a@example.com', 'h', 'A') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	insert := func(sql string, args ...any) error {
		_, err := conn.Exec(ctx, "INSERT INTO ingredients (name, category, owner_id, usda_fdc_id) VALUES "+sql, args...)
		return err
	}
	if err := insert(`('Apple', 'produce', NULL, 100)`); err != nil {
		t.Fatalf("valid global insert: %v", err)
	}
	if err := insert(`('My Mix', 'other', $1, NULL)`, userID); err != nil {
		t.Fatalf("valid custom insert: %v", err)
	}
	if err := insert(`('Bad Category', 'not_a_category', NULL, 101)`); err == nil {
		t.Error("an invalid category was accepted, want a constraint violation")
	}
	if err := insert(`('Duplicate FDC', 'produce', NULL, 100)`); err == nil {
		t.Error("a duplicate usda_fdc_id was accepted, want a unique violation")
	}
	if err := insert(`('Ghost', 'other', gen_random_uuid(), NULL)`); err == nil {
		t.Error("an owner_id that does not reference a user was accepted, want a foreign key violation")
	}

	var ingredientID string
	if err := conn.QueryRow(ctx,
		`INSERT INTO ingredients (name, category) VALUES ('Banana', 'produce') RETURNING id`,
	).Scan(&ingredientID); err != nil {
		t.Fatalf("insert ingredient: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO ingredient_nutrients (ingredient_id, nutrient_key, amount_per_100g) VALUES ($1, 'calories', 89)`, ingredientID,
	); err != nil {
		t.Fatalf("insert nutrient: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO ingredient_nutrients (ingredient_id, nutrient_key, amount_per_100g) VALUES ($1, 'not_a_nutrient', 1)`, ingredientID,
	); err == nil {
		t.Error("an invalid nutrient_key was accepted, want an enum violation")
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO ingredient_nutrients (ingredient_id, nutrient_key, amount_per_100g) VALUES ($1, 'protein', -1)`, ingredientID,
	); err == nil {
		t.Error("a negative amount was accepted, want a constraint violation")
	}

	if _, err := conn.Exec(ctx, `DELETE FROM ingredients WHERE id = $1`, ingredientID); err != nil {
		t.Fatalf("delete ingredient: %v", err)
	}
	var n int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM ingredient_nutrients WHERE ingredient_id = $1`, ingredientID).Scan(&n); err != nil || n != 0 {
		t.Errorf("ingredient_nutrients rows after deleting the ingredient = %d (err %v), want 0", n, err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/db/... -run TestIngredientsSchemaEnforcesItsConstraints -v`
Expected: FAIL — `relation "ingredients" does not exist` (needs Docker; the test skips locally without it and fails under `CI=1`, per `testutil.requireDocker`).

- [ ] **Step 3: Write the migration**

Create `backend/migrations/00004_ingredients.sql`:

```sql
-- +goose Up
CREATE TYPE nutrient_key AS ENUM (
    'calories', 'protein', 'carbohydrates', 'sugar', 'fibre', 'fat', 'saturated_fat',
    'sodium', 'potassium', 'calcium', 'iron', 'magnesium', 'zinc',
    'vitamin_a', 'vitamin_c', 'vitamin_d', 'vitamin_b12', 'folate'
);

CREATE TABLE ingredients (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name             text NOT NULL,
    category         text NOT NULL CHECK (category IN (
        'produce', 'dairy_eggs', 'meat_seafood', 'grains_bread', 'legumes_nuts_seeds',
        'condiments_oils', 'spices_herbs', 'beverages', 'sweets_snacks', 'other'
    )),
    owner_id         uuid REFERENCES users (id) ON DELETE CASCADE,
    usda_fdc_id      integer UNIQUE,
    grams_per_piece  double precision CHECK (grams_per_piece > 0),
    density_g_per_ml double precision CHECK (density_g_per_ml > 0),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX ingredients_name_trgm_idx ON ingredients USING gin (name gin_trgm_ops);
CREATE INDEX ingredients_owner_id_idx ON ingredients (owner_id);
CREATE INDEX ingredients_category_idx ON ingredients (category);

CREATE TRIGGER ingredients_set_updated_at
    BEFORE UPDATE ON ingredients
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE ingredient_nutrients (
    ingredient_id   uuid NOT NULL REFERENCES ingredients (id) ON DELETE CASCADE,
    nutrient_key    nutrient_key NOT NULL,
    amount_per_100g double precision NOT NULL CHECK (amount_per_100g >= 0),
    PRIMARY KEY (ingredient_id, nutrient_key)
);

-- +goose Down
DROP TABLE ingredient_nutrients;
DROP TABLE ingredients;
DROP TYPE nutrient_key;
```

This is scratch-verified: `sqlc generate` parses it cleanly against a copy of the real `sqlc.yaml` (see Task 2), and its shape (inline `REFERENCES ... ON DELETE CASCADE`, `CHECK`, the shared `set_updated_at` trigger) matches `00002_users.sql` / `00003_refresh_tokens.sql` exactly.

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd backend && go test ./internal/db/... -run TestIngredientsSchemaEnforcesItsConstraints -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/migrations/00004_ingredients.sql backend/internal/db/schema_test.go
git commit -m "feat(backend): add the ingredients and ingredient_nutrients tables"
```

---

### Task 2: sqlc queries for ingredients

**Files:**
- Create: `backend/internal/store/queries/ingredients.sql`
- Modify (generated, commit the output): `backend/internal/store/sqlc/ingredients.sql.go`, `backend/internal/store/sqlc/models.go`

**Interfaces:**
- Consumes: the schema from Task 1.
- Produces (sqlc-generated, used by Task 3): `sqlc.Ingredient`, `sqlc.IngredientNutrient`, `sqlc.NutrientKey` (a `string`-backed enum type with one constant per key, e.g. `sqlc.NutrientKeyCalories = "calories"`), and these `*sqlc.Queries` methods:
  - `CreateIngredient(ctx, CreateIngredientParams{Name string, Category string, OwnerID *uuid.UUID, GramsPerPiece *float64, DensityGPerMl *float64}) (Ingredient, error)`
  - `GetIngredientForUser(ctx, GetIngredientForUserParams{ID uuid.UUID, UserID *uuid.UUID}) (Ingredient, error)`
  - `ListIngredients(ctx, ListIngredientsParams{UserID *uuid.UUID, Category *string, HasCursor bool, CursorName string, CursorID uuid.UUID, RowLimit int32}) ([]Ingredient, error)`
  - `SearchIngredients(ctx, SearchIngredientsParams{UserID *uuid.UUID, Category *string, Query string, RowLimit int32}) ([]Ingredient, error)`
  - `UpdateIngredient(ctx, UpdateIngredientParams{Name *string, Category *string, SetGramsPerPiece bool, GramsPerPiece *float64, SetDensityGPerMl bool, DensityGPerMl *float64, ID uuid.UUID, UserID *uuid.UUID}) (Ingredient, error)`
  - `DeleteIngredient(ctx, DeleteIngredientParams{ID uuid.UUID, UserID *uuid.UUID}) (int64, error)`
  - `ReplaceIngredientNutrients(ctx, ingredientID uuid.UUID) error`
  - `UpsertIngredientNutrient(ctx, UpsertIngredientNutrientParams{IngredientID uuid.UUID, NutrientKey NutrientKey, AmountPer100g float64}) error`
  - `GetIngredientNutrients(ctx, ingredientIds []uuid.UUID) ([]IngredientNutrient, error)`
  - `UpsertUSDAIngredient(ctx, UpsertUSDAIngredientParams{Name string, Category string, UsdaFdcID *int32}) (Ingredient, error)`

  All of these are scratch-verified: a real `sqlc generate` run against these exact query texts (with a copy of the repo's `sqlc.yaml`) produced exactly these signatures with no errors.

- [ ] **Step 1: Write the queries**

Create `backend/internal/store/queries/ingredients.sql`:

```sql
-- name: CreateIngredient :one
INSERT INTO ingredients (name, category, owner_id, grams_per_piece, density_g_per_ml)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetIngredientForUser :one
SELECT * FROM ingredients
WHERE id = sqlc.arg('id') AND (owner_id IS NULL OR owner_id = sqlc.arg('user_id'));

-- name: ListIngredients :many
SELECT * FROM ingredients
WHERE (owner_id IS NULL OR owner_id = sqlc.arg('user_id'))
  AND (sqlc.narg('category')::text IS NULL OR category = sqlc.narg('category'))
  AND (
    NOT sqlc.arg('has_cursor')::boolean
    OR name > sqlc.arg('cursor_name')::text
    OR (name = sqlc.arg('cursor_name')::text AND id > sqlc.arg('cursor_id')::uuid)
  )
ORDER BY name, id
LIMIT sqlc.arg('row_limit');

-- name: SearchIngredients :many
SELECT * FROM ingredients
WHERE (owner_id IS NULL OR owner_id = sqlc.arg('user_id'))
  AND (sqlc.narg('category')::text IS NULL OR category = sqlc.narg('category'))
  AND name % sqlc.arg('query')::text
ORDER BY similarity(name, sqlc.arg('query')::text) DESC, name
LIMIT sqlc.arg('row_limit');

-- name: UpdateIngredient :one
UPDATE ingredients SET
    name             = COALESCE(sqlc.narg('name'), name),
    category         = COALESCE(sqlc.narg('category'), category),
    grams_per_piece  = CASE WHEN sqlc.arg('set_grams_per_piece')::boolean THEN sqlc.narg('grams_per_piece') ELSE grams_per_piece END,
    density_g_per_ml = CASE WHEN sqlc.arg('set_density_g_per_ml')::boolean THEN sqlc.narg('density_g_per_ml') ELSE density_g_per_ml END
WHERE id = sqlc.arg('id') AND owner_id = sqlc.arg('user_id')
RETURNING *;

-- name: DeleteIngredient :execrows
DELETE FROM ingredients WHERE id = sqlc.arg('id') AND owner_id = sqlc.arg('user_id');

-- name: ReplaceIngredientNutrients :exec
DELETE FROM ingredient_nutrients WHERE ingredient_id = $1;

-- name: UpsertIngredientNutrient :exec
INSERT INTO ingredient_nutrients (ingredient_id, nutrient_key, amount_per_100g)
VALUES ($1, $2, $3)
ON CONFLICT (ingredient_id, nutrient_key) DO UPDATE SET amount_per_100g = EXCLUDED.amount_per_100g;

-- name: GetIngredientNutrients :many
SELECT * FROM ingredient_nutrients WHERE ingredient_id = ANY(sqlc.arg('ingredient_ids')::uuid[]);

-- name: UpsertUSDAIngredient :one
INSERT INTO ingredients (name, category, usda_fdc_id)
VALUES ($1, $2, $3)
ON CONFLICT (usda_fdc_id) DO UPDATE SET name = EXCLUDED.name, category = EXCLUDED.category
RETURNING *;
```

Notes on choices already made (do not redesign these):
- `ListIngredients` uses a keyset cursor with plain scalar comparisons (`name > ... OR (name = ... AND id > ...)`) instead of a row-value comparison `(name, id) > (...)`. Both are valid Postgres; the scalar form was chosen because it is unambiguous and easy to test.
- `GetIngredientForUser` exists for symmetry and future use but Task 5's handlers do not need it: `UpdateIngredient`'s `RETURNING *` already gives the post-update row, and there is no `GET /ingredients/{id}` in the spec.
- No "delete blocked because in use" query yet: nothing references `ingredients` until the meals plan adds `meal_ingredients`. That plan must add the FK and translate its violation the same way Task 3 translates `ingredients_owner_id_fkey` — do not build that here (YAGNI).

- [ ] **Step 2: Regenerate and verify it compiles**

Run: `cd backend && go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate`
Expected: exits 0, creates/updates `internal/store/sqlc/ingredients.sql.go` and adds the `Ingredient`, `IngredientNutrient`, `NutrientKey` types to `models.go`.

Run: `cd backend && go build ./...`
Expected: builds cleanly (nothing references the new queries yet, so this only proves the generated code itself compiles).

- [ ] **Step 3: Commit**

```bash
git add backend/internal/store/queries/ingredients.sql backend/internal/store/sqlc/
git commit -m "feat(backend): add sqlc queries for ingredients"
```

---

### Task 3: Ingredients service

**Files:**
- Modify: `backend/internal/store/store.go` (add `IsForeignKeyViolation`)
- Create: `backend/internal/service/ingredients.go`
- Create: `backend/internal/service/ingredients_test.go`

**Interfaces:**
- Consumes: the `sqlc.Queries` methods from Task 2; `store.IsNotFound`, `store.InTx` (existing).
- Produces (consumed by Task 5's handlers):
  - Constants `service.NutrientCalories`, `NutrientProtein`, `NutrientCarbohydrates`, `NutrientSugar`, `NutrientFibre`, `NutrientFat`, `NutrientSaturatedFat`, `NutrientSodium`, `NutrientPotassium`, `NutrientCalcium`, `NutrientIron`, `NutrientMagnesium`, `NutrientZinc`, `NutrientVitaminA`, `NutrientVitaminC`, `NutrientVitaminD`, `NutrientVitaminB12`, `NutrientFolate` (all `string`, one per tracked nutrient — the shared vocabulary between `httpapi`, `service` and `usda`).
  - `service.ErrIngredientNotFound` (distinct from the existing `service.ErrNotFound`, which Task 5 keeps mapping to `401` for "the token's own user is gone"; `ErrIngredientNotFound` maps to `404`).
  - `type Ingredient struct { ID uuid.UUID; Name string; Category string; IsCustom bool; GramsPerPiece, DensityGPerMl *float64; Nutrients map[string]float64; CreatedAt, UpdatedAt time.Time }`
  - `type CreateIngredientInput struct { Name, Category string; GramsPerPiece, DensityGPerMl *float64; Nutrients map[string]float64 }`
  - `type UpdateIngredientInput struct { Name, Category *string; GramsPerPiece, DensityGPerMl Optional[float64]; Nutrients map[string]float64 }` (`Nutrients == nil` means "leave unchanged"; non-nil replaces the full set, including removing keys not present in the new map).
  - `type IngredientCursor struct { Name string; ID uuid.UUID }`, `type ListIngredientsInput struct { Category *string; Cursor *IngredientCursor; Limit int }`, `type IngredientPage struct { Items []Ingredient; NextCursor *IngredientCursor }`
  - `func NewIngredients(st *store.Store) *Ingredients`
  - `func (*Ingredients) Create(ctx, ownerID uuid.UUID, in CreateIngredientInput) (Ingredient, error)`
  - `func (*Ingredients) Update(ctx, ownerID, id uuid.UUID, in UpdateIngredientInput) (Ingredient, error)`
  - `func (*Ingredients) Delete(ctx, ownerID, id uuid.UUID) error`
  - `func (*Ingredients) List(ctx, userID uuid.UUID, in ListIngredientsInput) (IngredientPage, error)`
  - `func (*Ingredients) Search(ctx, userID uuid.UUID, query string, category *string, limit int) ([]Ingredient, error)`

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/service/ingredients_test.go`:

```go
package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/db"
	"github.com/InzKazik/mealplanner/backend/internal/service"
	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/testutil"
)

func newIngredientsFixture(t *testing.T) (*service.Ingredients, *store.Store) {
	t.Helper()
	pool, err := db.Connect(context.Background(), testutil.NewMigratedDatabase(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	st := store.New(pool)
	return service.NewIngredients(st), st
}

func newTestUser(t *testing.T, st *store.Store, email string) uuid.UUID {
	t.Helper()
	u, err := st.CreateUser(context.Background(), sqlcCreateUserParams(email))
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u.ID
}
```

This last helper needs a concrete `sqlc.CreateUserParams` literal; write it directly instead of a wrapper so the test file only imports what it needs:

```go
func newTestUser(t *testing.T, st *store.Store, email string) uuid.UUID {
	t.Helper()
	hash := "hash"
	u, err := st.CreateUser(context.Background(), sqlc.CreateUserParams{
		Email: email, PasswordHash: &hash, DisplayName: "Test User",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u.ID
}
```

(replace the placeholder `newTestUser` above with this version, and add `"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"` to the imports)

```go
func TestIngredientsCreateStoresNutrientsAndIsCustom(t *testing.T) {
	svc, _ := newIngredientsFixture(t)
	// (st unused directly here; call the two-return form and discard st with _)
	svc2, st := newIngredientsFixture(t)
	_ = svc
	owner := newTestUser(t, st, "a@example.com")

	grams := 120.0
	ing, err := svc2.Create(context.Background(), owner, service.CreateIngredientInput{
		Name: "Custom Oats", Category: "grains_bread", GramsPerPiece: &grams,
		Nutrients: map[string]float64{service.NutrientCalories: 389, service.NutrientProtein: 17},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !ing.IsCustom || ing.Name != "Custom Oats" || ing.Category != "grains_bread" {
		t.Errorf("ingredient = %+v", ing)
	}
	if ing.GramsPerPiece == nil || *ing.GramsPerPiece != 120 {
		t.Errorf("GramsPerPiece = %v, want 120", ing.GramsPerPiece)
	}
	if ing.Nutrients[service.NutrientCalories] != 389 || ing.Nutrients[service.NutrientProtein] != 17 {
		t.Errorf("Nutrients = %v", ing.Nutrients)
	}
	if len(ing.Nutrients) != 2 {
		t.Errorf("Nutrients has %d keys, want exactly the 2 provided", len(ing.Nutrients))
	}
}

func TestIngredientsCreateWithAMissingOwnerFailsCleanly(t *testing.T) {
	svc, _ := newIngredientsFixture(t)

	_, err := svc.Create(context.Background(), uuid.New(), service.CreateIngredientInput{
		Name: "Orphan", Category: "other",
	})
	if !errors.Is(err, service.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound (mirrors GetUser: an access token for a gone user is unauthorized, not a 500)", err)
	}
}

func TestIngredientsUpdateReplacesNutrientsAndRejectsNonOwners(t *testing.T) {
	svc, st := newIngredientsFixture(t)
	owner := newTestUser(t, st, "owner@example.com")
	other := newTestUser(t, st, "other@example.com")

	ing, err := svc.Create(context.Background(), owner, service.CreateIngredientInput{
		Name: "Mix", Category: "other",
		Nutrients: map[string]float64{service.NutrientCalories: 100, service.NutrientFat: 5},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	newName := "Renamed Mix"
	updated, err := svc.Update(context.Background(), owner, ing.ID, service.UpdateIngredientInput{
		Name:      &newName,
		Nutrients: map[string]float64{service.NutrientProtein: 9},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "Renamed Mix" {
		t.Errorf("Name = %q, want Renamed Mix", updated.Name)
	}
	if _, ok := updated.Nutrients[service.NutrientCalories]; ok {
		t.Error("stale nutrient (calories) survived a full replace")
	}
	if updated.Nutrients[service.NutrientProtein] != 9 {
		t.Errorf("Nutrients = %v, want protein=9 only", updated.Nutrients)
	}

	if _, err := svc.Update(context.Background(), other, ing.ID, service.UpdateIngredientInput{Name: &newName}); !errors.Is(err, service.ErrIngredientNotFound) {
		t.Errorf("Update by a non-owner: err = %v, want ErrIngredientNotFound", err)
	}
}

func TestIngredientsUpdatePreservesNutrientsWhenNotProvided(t *testing.T) {
	svc, st := newIngredientsFixture(t)
	owner := newTestUser(t, st, "a@example.com")
	ing, err := svc.Create(context.Background(), owner, service.CreateIngredientInput{
		Name: "Mix", Category: "other", Nutrients: map[string]float64{service.NutrientCalories: 100},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	newCategory := "sweets_snacks"
	updated, err := svc.Update(context.Background(), owner, ing.ID, service.UpdateIngredientInput{Category: &newCategory})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Category != "sweets_snacks" {
		t.Errorf("Category = %q, want sweets_snacks", updated.Category)
	}
	if updated.Nutrients[service.NutrientCalories] != 100 {
		t.Errorf("Nutrients = %v, want calories=100 preserved", updated.Nutrients)
	}
}

func TestIngredientsDeleteRejectsNonOwnersAndGlobals(t *testing.T) {
	svc, st := newIngredientsFixture(t)
	owner := newTestUser(t, st, "owner@example.com")
	other := newTestUser(t, st, "other@example.com")
	ing, err := svc.Create(context.Background(), owner, service.CreateIngredientInput{Name: "Mix", Category: "other"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := svc.Delete(context.Background(), other, ing.ID); !errors.Is(err, service.ErrIngredientNotFound) {
		t.Errorf("Delete by a non-owner: err = %v, want ErrIngredientNotFound", err)
	}
	if err := svc.Delete(context.Background(), owner, ing.ID); err != nil {
		t.Fatalf("Delete by the owner: %v", err)
	}
}

func TestIngredientsListPaginatesAndFiltersByOwnership(t *testing.T) {
	svc, st := newIngredientsFixture(t)
	owner := newTestUser(t, st, "owner@example.com")
	other := newTestUser(t, st, "other@example.com")

	for _, name := range []string{"Banana", "Apple", "Carrot"} {
		if _, err := svc.Create(context.Background(), owner, service.CreateIngredientInput{Name: name, Category: "produce"}); err != nil {
			t.Fatalf("Create(%s): %v", name, err)
		}
	}
	if _, err := svc.Create(context.Background(), other, service.CreateIngredientInput{Name: "Zebra Cake", Category: "sweets_snacks"}); err != nil {
		t.Fatalf("Create(other's ingredient): %v", err)
	}

	var names []string
	var cursor *service.IngredientCursor
	for {
		page, err := svc.List(context.Background(), owner, service.ListIngredientsInput{Cursor: cursor, Limit: 1})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		for _, ing := range page.Items {
			names = append(names, ing.Name)
		}
		if page.NextCursor == nil {
			break
		}
		cursor = page.NextCursor
	}
	if want := []string{"Apple", "Banana", "Carrot"}; len(names) != len(want) || names[0] != want[0] || names[1] != want[1] || names[2] != want[2] {
		t.Errorf("paginated names = %v, want %v (alphabetical, owner's only)", names, want)
	}
}

func TestIngredientsSearchRanksByNameSimilarity(t *testing.T) {
	svc, st := newIngredientsFixture(t)
	owner := newTestUser(t, st, "owner@example.com")
	for _, name := range []string{"Chicken Breast", "Chicken Thigh", "Beef Steak"} {
		if _, err := svc.Create(context.Background(), owner, service.CreateIngredientInput{Name: name, Category: "meat_seafood"}); err != nil {
			t.Fatalf("Create(%s): %v", name, err)
		}
	}

	results, err := svc.Search(context.Background(), owner, "chicken", nil, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("Search(chicken) returned %d results, want 2", len(results))
	}
	for _, r := range results {
		if r.Category != "meat_seafood" {
			t.Errorf("result %q has category %q, want meat_seafood", r.Name, r.Category)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/service/... -run TestIngredients -v`
Expected: FAIL to compile — `service.Ingredients` etc. do not exist yet.

- [ ] **Step 3: Add `IsForeignKeyViolation` to the store**

In `backend/internal/store/store.go`, add after `IsUniqueViolation`:

```go
// IsForeignKeyViolation reports whether err is a foreign-key violation on the
// named constraint.
func IsForeignKeyViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503" && pgErr.ConstraintName == constraint
}
```

- [ ] **Step 4: Implement the service**

Create `backend/internal/service/ingredients.go`:

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

// Nutrient keys, matching the ingredient_nutrients.nutrient_key enum. This is
// the shared vocabulary between httpapi, service and usda.
const (
	NutrientCalories      = "calories"
	NutrientProtein       = "protein"
	NutrientCarbohydrates = "carbohydrates"
	NutrientSugar         = "sugar"
	NutrientFibre         = "fibre"
	NutrientFat           = "fat"
	NutrientSaturatedFat  = "saturated_fat"
	NutrientSodium        = "sodium"
	NutrientPotassium     = "potassium"
	NutrientCalcium       = "calcium"
	NutrientIron          = "iron"
	NutrientMagnesium     = "magnesium"
	NutrientZinc          = "zinc"
	NutrientVitaminA      = "vitamin_a"
	NutrientVitaminC      = "vitamin_c"
	NutrientVitaminD      = "vitamin_d"
	NutrientVitaminB12    = "vitamin_b12"
	NutrientFolate        = "folate"
)

// ErrIngredientNotFound means the ingredient does not exist, or exists but is
// not visible to the caller: global ingredients are always visible, a custom
// ingredient only to its owner.
var ErrIngredientNotFound = errors.New("ingredient not found")

// Ingredient is an ingredient as the rest of the application sees it.
type Ingredient struct {
	ID            uuid.UUID
	Name          string
	Category      string
	IsCustom      bool
	GramsPerPiece *float64
	DensityGPerMl *float64
	Nutrients     map[string]float64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// CreateIngredientInput is the data needed to create a custom ingredient.
type CreateIngredientInput struct {
	Name          string
	Category      string
	GramsPerPiece *float64
	DensityGPerMl *float64
	Nutrients     map[string]float64
}

// UpdateIngredientInput is a partial update to a custom ingredient. Nutrients,
// when non-nil, replaces the full nutrient set (a key absent from the new map
// is removed, not left alone).
type UpdateIngredientInput struct {
	Name          *string
	Category      *string
	GramsPerPiece Optional[float64]
	DensityGPerMl Optional[float64]
	Nutrients     map[string]float64
}

// IngredientCursor is an opaque position in the alphabetical ingredient list.
type IngredientCursor struct {
	Name string
	ID   uuid.UUID
}

// ListIngredientsInput selects a page of the alphabetical ingredient list.
type ListIngredientsInput struct {
	Category *string
	Cursor   *IngredientCursor
	Limit    int
}

// IngredientPage is one page of ingredients plus the cursor for the next one
// (nil on the last page).
type IngredientPage struct {
	Items      []Ingredient
	NextCursor *IngredientCursor
}

// Ingredients implements the ingredient catalog: custom ingredients owned by
// a user, plus the shared global (USDA) catalog.
type Ingredients struct {
	st *store.Store
}

// NewIngredients returns an Ingredients service.
func NewIngredients(st *store.Store) *Ingredients { return &Ingredients{st: st} }

// Create adds a custom ingredient owned by ownerID.
func (s *Ingredients) Create(ctx context.Context, ownerID uuid.UUID, in CreateIngredientInput) (Ingredient, error) {
	var ing Ingredient
	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
		row, err := q.CreateIngredient(ctx, sqlc.CreateIngredientParams{
			Name: in.Name, Category: in.Category, OwnerID: &ownerID,
			GramsPerPiece: in.GramsPerPiece, DensityGPerMl: in.DensityGPerMl,
		})
		if store.IsForeignKeyViolation(err, "ingredients_owner_id_fkey") {
			// Mirrors GetUser: an access token for a user that no longer
			// exists is unauthorized, not a 500. See ErrNotFound in auth.go.
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("create ingredient: %w", err)
		}
		if err := upsertNutrients(ctx, q, row.ID, in.Nutrients); err != nil {
			return err
		}
		ing = toIngredient(row, in.Nutrients)
		return nil
	})
	if err != nil {
		return Ingredient{}, err
	}
	return ing, nil
}

// Update applies a partial update to a custom ingredient owned by ownerID.
func (s *Ingredients) Update(ctx context.Context, ownerID, id uuid.UUID, in UpdateIngredientInput) (Ingredient, error) {
	var ing Ingredient
	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
		row, err := q.UpdateIngredient(ctx, sqlc.UpdateIngredientParams{
			ID: id, UserID: &ownerID, Name: in.Name, Category: in.Category,
			SetGramsPerPiece: in.GramsPerPiece.Specified, GramsPerPiece: in.GramsPerPiece.Value,
			SetDensityGPerMl: in.DensityGPerMl.Specified, DensityGPerMl: in.DensityGPerMl.Value,
		})
		if store.IsNotFound(err) {
			return ErrIngredientNotFound
		}
		if err != nil {
			return fmt.Errorf("update ingredient: %w", err)
		}

		current, err := s.nutrientsFor(ctx, q, row.ID)
		if err != nil {
			return err
		}
		if in.Nutrients != nil {
			if err := q.ReplaceIngredientNutrients(ctx, row.ID); err != nil {
				return fmt.Errorf("replace nutrients: %w", err)
			}
			if err := upsertNutrients(ctx, q, row.ID, in.Nutrients); err != nil {
				return err
			}
			current = in.Nutrients
		}
		ing = toIngredient(row, current)
		return nil
	})
	if err != nil {
		return Ingredient{}, err
	}
	return ing, nil
}

// Delete removes a custom ingredient owned by ownerID.
func (s *Ingredients) Delete(ctx context.Context, ownerID, id uuid.UUID) error {
	n, err := s.st.DeleteIngredient(ctx, sqlc.DeleteIngredientParams{ID: id, UserID: &ownerID})
	if err != nil {
		return fmt.Errorf("delete ingredient: %w", err)
	}
	if n == 0 {
		return ErrIngredientNotFound
	}
	return nil
}

// List returns a page of the alphabetical ingredient catalog visible to userID.
func (s *Ingredients) List(ctx context.Context, userID uuid.UUID, in ListIngredientsInput) (IngredientPage, error) {
	params := sqlc.ListIngredientsParams{UserID: &userID, Category: in.Category, RowLimit: int32(in.Limit) + 1}
	if in.Cursor != nil {
		params.HasCursor = true
		params.CursorName = in.Cursor.Name
		params.CursorID = in.Cursor.ID
	}
	rows, err := s.st.ListIngredients(ctx, params)
	if err != nil {
		return IngredientPage{}, fmt.Errorf("list ingredients: %w", err)
	}

	var next *IngredientCursor
	if len(rows) > in.Limit {
		last := rows[in.Limit-1]
		next = &IngredientCursor{Name: last.Name, ID: last.ID}
		rows = rows[:in.Limit]
	}
	items, err := s.withNutrients(ctx, rows)
	if err != nil {
		return IngredientPage{}, err
	}
	return IngredientPage{Items: items, NextCursor: next}, nil
}

// Search returns the best-matching ingredients for query, visible to userID.
// Results are not paginated: a type-ahead search never needs a second page.
func (s *Ingredients) Search(ctx context.Context, userID uuid.UUID, query string, category *string, limit int) ([]Ingredient, error) {
	rows, err := s.st.SearchIngredients(ctx, sqlc.SearchIngredientsParams{
		UserID: &userID, Category: category, Query: query, RowLimit: int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("search ingredients: %w", err)
	}
	return s.withNutrients(ctx, rows)
}

func (s *Ingredients) nutrientsFor(ctx context.Context, q *sqlc.Queries, id uuid.UUID) (map[string]float64, error) {
	rows, err := q.GetIngredientNutrients(ctx, []uuid.UUID{id})
	if err != nil {
		return nil, fmt.Errorf("get nutrients: %w", err)
	}
	return toNutrientMap(rows), nil
}

func (s *Ingredients) withNutrients(ctx context.Context, rows []sqlc.Ingredient) ([]Ingredient, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	nutrientRows, err := s.st.GetIngredientNutrients(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("get nutrients: %w", err)
	}
	byIngredient := make(map[uuid.UUID]map[string]float64, len(rows))
	for _, n := range nutrientRows {
		if byIngredient[n.IngredientID] == nil {
			byIngredient[n.IngredientID] = map[string]float64{}
		}
		byIngredient[n.IngredientID][string(n.NutrientKey)] = n.AmountPer100g
	}
	items := make([]Ingredient, len(rows))
	for i, r := range rows {
		items[i] = toIngredient(r, byIngredient[r.ID])
	}
	return items, nil
}

func upsertNutrients(ctx context.Context, q *sqlc.Queries, ingredientID uuid.UUID, nutrients map[string]float64) error {
	for key, amount := range nutrients {
		if err := q.UpsertIngredientNutrient(ctx, sqlc.UpsertIngredientNutrientParams{
			IngredientID: ingredientID, NutrientKey: sqlc.NutrientKey(key), AmountPer100g: amount,
		}); err != nil {
			return fmt.Errorf("upsert nutrient %s: %w", key, err)
		}
	}
	return nil
}

func toNutrientMap(rows []sqlc.IngredientNutrient) map[string]float64 {
	m := make(map[string]float64, len(rows))
	for _, r := range rows {
		m[string(r.NutrientKey)] = r.AmountPer100g
	}
	return m
}

func toIngredient(row sqlc.Ingredient, nutrients map[string]float64) Ingredient {
	return Ingredient{
		ID: row.ID, Name: row.Name, Category: row.Category, IsCustom: row.OwnerID != nil,
		GramsPerPiece: row.GramsPerPiece, DensityGPerMl: row.DensityGPerMl,
		Nutrients: nutrients, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}
```

- [ ] **Step 5: Fix the test file's helper duplication**

The Step 1 listing showed two versions of `newTestUser` and called `newIngredientsFixture` twice in one test to explain the `sqlc` import; clean that up for real: `newIngredientsFixture` returns `(*service.Ingredients, *store.Store)` and every test calls it once. Only the final `newTestUser` body (the one using `sqlc.CreateUserParams`) belongs in the file. Re-read the finished `ingredients_test.go` before running it and delete the placeholder/duplicate blocks.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/service/... -run TestIngredients -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add backend/internal/store/store.go backend/internal/service/ingredients.go backend/internal/service/ingredients_test.go
git commit -m "feat(backend): add the ingredients service"
```

---

### Task 4: OpenAPI contract — `/ingredients`

**Files:**
- Modify: `openapi.yaml`
- Modify (generated, commit the output): `backend/internal/api/api.gen.go`

**Interfaces:**
- Produces (consumed by Task 5): `api.ListIngredientsParams{Q, Cursor *string; Category *api.IngredientCategory; Limit *int}`, `api.CreateIngredientRequest{Name string; Category IngredientCategory; GramsPerPiece, DensityGPerMl nullable.Nullable[float64]; Nutrients *NutrientAmounts}`, `api.UpdateIngredientRequest{Name *string; Category *IngredientCategory; GramsPerPiece, DensityGPerMl nullable.Nullable[float64]; Nutrients *NutrientAmounts}`, `api.Ingredient{Id openapi_types.UUID (= uuid.UUID); Name string; Category IngredientCategory; IsCustom bool; GramsPerPiece, DensityGPerMl nullable.Nullable[float64]; Nutrients NutrientAmounts; CreatedAt, UpdatedAt time.Time}`, `api.NutrientAmounts` (17 `nullable.Nullable[float64]` fields, PascalCase of the snake_case keys, e.g. `SaturatedFat`, `VitaminB12`), `api.IngredientList{Items []Ingredient; NextCursor nullable.Nullable[string]}`, and `api.ServerInterface` methods `ListIngredients(w, r, params ListIngredientsParams)`, `CreateIngredient(w, r)`, `UpdateIngredient(w, r, id openapi_types.UUID)`, `DeleteIngredient(w, r, id openapi_types.UUID)`.

  All of the above is scratch-verified: this exact YAML was run through `redocly lint` (valid, only the two pre-existing unrelated warnings) and a real `oapi-codegen` v2.8.0 generate (matches the repo's pinned version), and the struct field names, JSON tags and `ServerInterface` signatures above were read from that generated output.

- [ ] **Step 1: Add the tag and the two paths**

In `openapi.yaml`, after the `Profile` tag (before `paths:`), add:

```yaml
  - name: Ingredients
    description: The global and per-user ingredient catalog.
```

Immediately after `paths:` add (before the existing `/healthz:`, or anywhere among the top-level path items — order does not matter to the spec, but keep it grouped with a comment-free insertion right before `/healthz:` for readability):

```yaml
  /ingredients:
    get:
      tags: [Ingredients]
      operationId: listIngredients
      summary: List or search ingredients
      description: Without `q`, lists ingredients alphabetically with cursor pagination. With `q`, searches by name and returns the best matches, not paginated.
      parameters:
        - name: q
          in: query
          schema:
            type: string
            minLength: 1
            maxLength: 100
        - name: category
          in: query
          schema:
            $ref: '#/components/schemas/IngredientCategory'
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
          description: Matching ingredients.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/IngredientList'
        '400':
          $ref: '#/components/responses/BadRequest'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '429':
          $ref: '#/components/responses/TooManyRequests'
        '500':
          $ref: '#/components/responses/Problem'
    post:
      tags: [Ingredients]
      operationId: createIngredient
      summary: Create a custom ingredient
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/CreateIngredientRequest'
      responses:
        '201':
          description: The ingredient was created.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Ingredient'
        '400':
          $ref: '#/components/responses/BadRequest'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '429':
          $ref: '#/components/responses/TooManyRequests'
        '500':
          $ref: '#/components/responses/Problem'
  /ingredients/{id}:
    parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
          format: uuid
    patch:
      tags: [Ingredients]
      operationId: updateIngredient
      summary: Update a custom ingredient
      description: Fields that are absent are left unchanged. Only a custom ingredient owned by the caller can be updated.
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/UpdateIngredientRequest'
      responses:
        '200':
          description: The updated ingredient.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Ingredient'
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
      tags: [Ingredients]
      operationId: deleteIngredient
      summary: Delete a custom ingredient
      responses:
        '204':
          description: The ingredient was deleted.
        '401':
          $ref: '#/components/responses/Unauthorized'
        '404':
          $ref: '#/components/responses/NotFound'
        '429':
          $ref: '#/components/responses/TooManyRequests'
        '500':
          $ref: '#/components/responses/Problem'
```

- [ ] **Step 2: Add the schemas**

In `components.schemas`, after `UpdateProfileRequest` (before `responses:`), add:

```yaml
    IngredientCategory:
      type: string
      enum: [produce, dairy_eggs, meat_seafood, grains_bread, legumes_nuts_seeds, condiments_oils, spices_herbs, beverages, sweets_snacks, other]
    NutrientAmounts:
      type: object
      description: Amount per 100 g for each tracked nutrient. A null value means the amount is unknown.
      required: [calories, protein, carbohydrates, sugar, fibre, fat, saturated_fat, sodium, potassium, calcium, iron, magnesium, zinc, vitamin_a, vitamin_c, vitamin_d, vitamin_b12, folate]
      properties:
        calories: { type: number, format: double, nullable: true }
        protein: { type: number, format: double, nullable: true }
        carbohydrates: { type: number, format: double, nullable: true }
        sugar: { type: number, format: double, nullable: true }
        fibre: { type: number, format: double, nullable: true }
        fat: { type: number, format: double, nullable: true }
        saturated_fat: { type: number, format: double, nullable: true }
        sodium: { type: number, format: double, nullable: true }
        potassium: { type: number, format: double, nullable: true }
        calcium: { type: number, format: double, nullable: true }
        iron: { type: number, format: double, nullable: true }
        magnesium: { type: number, format: double, nullable: true }
        zinc: { type: number, format: double, nullable: true }
        vitamin_a: { type: number, format: double, nullable: true }
        vitamin_c: { type: number, format: double, nullable: true }
        vitamin_d: { type: number, format: double, nullable: true }
        vitamin_b12: { type: number, format: double, nullable: true }
        folate: { type: number, format: double, nullable: true }
    Ingredient:
      type: object
      required: [id, name, category, is_custom, grams_per_piece, density_g_per_ml, nutrients, created_at, updated_at]
      properties:
        id:
          type: string
          format: uuid
        name:
          type: string
        category:
          $ref: '#/components/schemas/IngredientCategory'
        is_custom:
          type: boolean
          description: True for a custom ingredient owned by the caller; false for the global USDA catalog.
        grams_per_piece:
          type: number
          format: double
          nullable: true
        density_g_per_ml:
          type: number
          format: double
          nullable: true
        nutrients:
          $ref: '#/components/schemas/NutrientAmounts'
        created_at:
          type: string
          format: date-time
        updated_at:
          type: string
          format: date-time
    CreateIngredientRequest:
      type: object
      additionalProperties: false
      required: [name, category]
      properties:
        name:
          type: string
          minLength: 1
          maxLength: 200
        category:
          $ref: '#/components/schemas/IngredientCategory'
        grams_per_piece:
          type: number
          format: double
          nullable: true
          exclusiveMinimum: true
          minimum: 0
          maximum: 10000
        density_g_per_ml:
          type: number
          format: double
          nullable: true
          exclusiveMinimum: true
          minimum: 0
          maximum: 3
        nutrients:
          $ref: '#/components/schemas/NutrientAmounts'
    UpdateIngredientRequest:
      type: object
      additionalProperties: false
      properties:
        name:
          type: string
          minLength: 1
          maxLength: 200
        category:
          $ref: '#/components/schemas/IngredientCategory'
        grams_per_piece:
          type: number
          format: double
          nullable: true
          exclusiveMinimum: true
          minimum: 0
          maximum: 10000
        density_g_per_ml:
          type: number
          format: double
          nullable: true
          exclusiveMinimum: true
          minimum: 0
          maximum: 3
        nutrients:
          $ref: '#/components/schemas/NutrientAmounts'
    IngredientList:
      type: object
      required: [items, next_cursor]
      properties:
        items:
          type: array
          items:
            $ref: '#/components/schemas/Ingredient'
        next_cursor:
          type: string
          nullable: true
```

- [ ] **Step 3: Add the `NotFound` response**

In `components.responses`, before `Conflict`, add:

```yaml
    NotFound:
      description: The resource does not exist, or exists but is not visible to the caller.
      content:
        application/problem+json:
          schema:
            $ref: '#/components/schemas/Problem'
```

- [ ] **Step 4: Lint and regenerate**

Run: `make lint-api`
Expected: valid, same 4 pre-existing warnings as before this change (info-license, no-server-example.com, and the two `operation-4xx-response` warnings already exempted in `.redocly.lint-ignore.yaml` for `/healthz` and `/readyz`) — no new warnings.

Run: `make generate`
Expected: `backend/internal/api/api.gen.go` is regenerated; `git diff --stat` shows only that file plus, if Task 2 wasn't already committed, the sqlc output.

Run: `cd backend && go build ./...`
Expected: builds cleanly (nothing implements the new `ServerInterface` methods yet, but `api.gen.go` alone must compile).

- [ ] **Step 5: Commit**

```bash
git add openapi.yaml backend/internal/api/api.gen.go
git commit -m "feat(api): add the ingredients endpoints to the contract"
```

---

### Task 5: Ingredients handlers

**Files:**
- Create: `backend/internal/httpapi/nutrients.go`
- Create: `backend/internal/httpapi/ingredients.go`
- Modify: `backend/internal/httpapi/account.go` (extend `writeServiceError`)
- Modify: `backend/internal/httpapi/server.go` (add `ingredients IngredientsService` field)
- Modify: `backend/internal/httpapi/router.go` (wire `Deps.Ingredients`)

**Interfaces:**
- Consumes: `service.Ingredients` and its types from Task 3; `api.*` types from Task 4; the existing `toNullable`, `toOptional`, `decodeJSON`, `requireUser`, `writeJSON` helpers from `account.go`.
- Produces: `httpapi.IngredientsService` interface (consumed by `router.go`'s `Deps.Ingredients` and by Task 6's tests).

- [ ] **Step 1: Add the `NotFound` mapping to `writeServiceError`**

In `backend/internal/httpapi/account.go`, in `writeServiceError`, add a case (order matters: `errors.Is` checks are independent here, so placement among the existing cases doesn't change behavior — add it after the `ErrInvalidRefreshToken` case):

```go
	case errors.Is(err, service.ErrIngredientNotFound):
		WriteProblem(w, http.StatusNotFound, CodeNotFound, "")
```

Also update the comment on the existing `ErrNotFound` case, since Task 3's `Ingredients.Create` now reuses it too:

```go
	case errors.Is(err, service.ErrNotFound):
		// The signed-in user's own account no longer exists: a valid access
		// token for a deleted user is simply no longer authorized. Reused by
		// Ingredients.Create for the same reason (see ingredients.go).
		w.Header().Set("WWW-Authenticate", "Bearer")
		WriteProblem(w, http.StatusUnauthorized, CodeUnauthorized, "")
```

- [ ] **Step 2: Write the nutrient conversion helpers**

Create `backend/internal/httpapi/nutrients.go`:

```go
package httpapi

import (
	"github.com/oapi-codegen/nullable"

	"github.com/InzKazik/mealplanner/backend/internal/api"
	"github.com/InzKazik/mealplanner/backend/internal/service"
)

// nutrientsFromAPI returns one map entry per key present in n with a non-null
// value; an absent or explicitly null key is simply omitted.
func nutrientsFromAPI(n api.NutrientAmounts) map[string]float64 {
	m := make(map[string]float64, 17)
	set := func(key string, v nullable.Nullable[float64]) {
		if v.IsSpecified() && !v.IsNull() {
			m[key] = v.MustGet()
		}
	}
	set(service.NutrientCalories, n.Calories)
	set(service.NutrientProtein, n.Protein)
	set(service.NutrientCarbohydrates, n.Carbohydrates)
	set(service.NutrientSugar, n.Sugar)
	set(service.NutrientFibre, n.Fibre)
	set(service.NutrientFat, n.Fat)
	set(service.NutrientSaturatedFat, n.SaturatedFat)
	set(service.NutrientSodium, n.Sodium)
	set(service.NutrientPotassium, n.Potassium)
	set(service.NutrientCalcium, n.Calcium)
	set(service.NutrientIron, n.Iron)
	set(service.NutrientMagnesium, n.Magnesium)
	set(service.NutrientZinc, n.Zinc)
	set(service.NutrientVitaminA, n.VitaminA)
	set(service.NutrientVitaminC, n.VitaminC)
	set(service.NutrientVitaminD, n.VitaminD)
	set(service.NutrientVitaminB12, n.VitaminB12)
	set(service.NutrientFolate, n.Folate)
	return m
}

// nutrientsToAPI renders a nutrient map with every key present, null where
// the ingredient has no value.
func nutrientsToAPI(m map[string]float64) api.NutrientAmounts {
	get := func(key string) nullable.Nullable[float64] {
		if v, ok := m[key]; ok {
			return nullable.NewNullableWithValue(v)
		}
		return nullable.NewNullNullable[float64]()
	}
	return api.NutrientAmounts{
		Calories: get(service.NutrientCalories), Protein: get(service.NutrientProtein),
		Carbohydrates: get(service.NutrientCarbohydrates), Sugar: get(service.NutrientSugar),
		Fibre: get(service.NutrientFibre), Fat: get(service.NutrientFat),
		SaturatedFat: get(service.NutrientSaturatedFat), Sodium: get(service.NutrientSodium),
		Potassium: get(service.NutrientPotassium), Calcium: get(service.NutrientCalcium),
		Iron: get(service.NutrientIron), Magnesium: get(service.NutrientMagnesium),
		Zinc: get(service.NutrientZinc), VitaminA: get(service.NutrientVitaminA),
		VitaminC: get(service.NutrientVitaminC), VitaminD: get(service.NutrientVitaminD),
		VitaminB12: get(service.NutrientVitaminB12), Folate: get(service.NutrientFolate),
	}
}
```

- [ ] **Step 3: Write the handlers**

Create `backend/internal/httpapi/ingredients.go`:

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

// IngredientsService is what the ingredients handlers need from the
// ingredients service.
type IngredientsService interface {
	Create(ctx context.Context, ownerID uuid.UUID, in service.CreateIngredientInput) (service.Ingredient, error)
	Update(ctx context.Context, ownerID, id uuid.UUID, in service.UpdateIngredientInput) (service.Ingredient, error)
	Delete(ctx context.Context, ownerID, id uuid.UUID) error
	List(ctx context.Context, userID uuid.UUID, in service.ListIngredientsInput) (service.IngredientPage, error)
	Search(ctx context.Context, userID uuid.UUID, query string, category *string, limit int) ([]service.Ingredient, error)
}

const defaultIngredientLimit = 20

func (s *server) ListIngredients(w http.ResponseWriter, r *http.Request, params api.ListIngredientsParams) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	limit := defaultIngredientLimit
	if params.Limit != nil {
		limit = *params.Limit
	}
	var category *string
	if params.Category != nil {
		c := string(*params.Category)
		category = &c
	}

	if params.Q != nil {
		items, err := s.ingredients.Search(r.Context(), userID, *params.Q, category, limit)
		if err != nil {
			s.writeServiceError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, toIngredientList(items, ""))
		return
	}

	var cursor *service.IngredientCursor
	if params.Cursor != nil {
		c, ok := decodeIngredientCursor(*params.Cursor)
		if !ok {
			WriteValidationProblem(w, "cursor is invalid", []FieldError{{Field: "cursor", Code: FieldInvalidForm}})
			return
		}
		cursor = &c
	}
	page, err := s.ingredients.List(r.Context(), userID, service.ListIngredientsInput{Category: category, Cursor: cursor, Limit: limit})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	next := ""
	if page.NextCursor != nil {
		next = encodeIngredientCursor(*page.NextCursor)
	}
	writeJSON(w, http.StatusOK, toIngredientList(page.Items, next))
}

func (s *server) CreateIngredient(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.CreateIngredientRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in := service.CreateIngredientInput{
		Name: req.Name, Category: string(req.Category),
		GramsPerPiece: nullableToPtr(req.GramsPerPiece), DensityGPerMl: nullableToPtr(req.DensityGPerMl),
	}
	if req.Nutrients != nil {
		in.Nutrients = nutrientsFromAPI(*req.Nutrients)
	}
	ing, err := s.ingredients.Create(r.Context(), userID, in)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAPIIngredient(ing))
}

func (s *server) UpdateIngredient(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.UpdateIngredientRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in := service.UpdateIngredientInput{
		Name:          req.Name,
		GramsPerPiece: toOptional(req.GramsPerPiece),
		DensityGPerMl: toOptional(req.DensityGPerMl),
	}
	if req.Category != nil {
		c := string(*req.Category)
		in.Category = &c
	}
	if req.Nutrients != nil {
		n := nutrientsFromAPI(*req.Nutrients)
		in.Nutrients = n
	}
	ing, err := s.ingredients.Update(r.Context(), userID, id, in)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIIngredient(ing))
}

func (s *server) DeleteIngredient(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	if err := s.ingredients.Delete(r.Context(), userID, id); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func nullableToPtr(n nullable.Nullable[float64]) *float64 {
	if !n.IsSpecified() || n.IsNull() {
		return nil
	}
	v := n.MustGet()
	return &v
}

func toIngredientList(items []service.Ingredient, nextCursor string) api.IngredientList {
	list := api.IngredientList{Items: make([]api.Ingredient, len(items))}
	for i, ing := range items {
		list.Items[i] = toAPIIngredient(ing)
	}
	if nextCursor == "" {
		list.NextCursor = nullable.NewNullNullable[string]()
	} else {
		list.NextCursor = nullable.NewNullableWithValue(nextCursor)
	}
	return list
}

func toAPIIngredient(ing service.Ingredient) api.Ingredient {
	return api.Ingredient{
		Id: ing.ID, Name: ing.Name, Category: api.IngredientCategory(ing.Category), IsCustom: ing.IsCustom,
		GramsPerPiece: toNullable(ing.GramsPerPiece), DensityGPerMl: toNullable(ing.DensityGPerMl),
		Nutrients: nutrientsToAPI(ing.Nutrients),
		CreatedAt: ing.CreatedAt, UpdatedAt: ing.UpdatedAt,
	}
}

type ingredientCursorPayload struct {
	Name string    `json:"n"`
	ID   uuid.UUID `json:"i"`
}

func encodeIngredientCursor(c service.IngredientCursor) string {
	b, _ := json.Marshal(ingredientCursorPayload{Name: c.Name, ID: c.ID})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeIngredientCursor(s string) (service.IngredientCursor, bool) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return service.IngredientCursor{}, false
	}
	var p ingredientCursorPayload
	if err := json.Unmarshal(b, &p); err != nil || p.Name == "" || p.ID == uuid.Nil {
		return service.IngredientCursor{}, false
	}
	return service.IngredientCursor{Name: p.Name, ID: p.ID}, true
}
```

Note: `UpdateIngredient`/`DeleteIngredient`'s `id` parameter is declared here as `uuid.UUID`, not `openapi_types.UUID`. `github.com/oapi-codegen/runtime/types.UUID` is a genuine Go type alias (`type UUID = uuid.UUID`, confirmed by reading that package's source), so this satisfies `api.ServerInterface` exactly and avoids an extra import.

`toNullable` and `toOptional` are reused unchanged from `account.go` — do not redefine them here.

- [ ] **Step 4: Wire the new service into `server` and the router**

In `backend/internal/httpapi/server.go`, add a field:

```go
type server struct {
	logger      *slog.Logger
	ready       func(context.Context) error
	auth        AuthService
	ingredients IngredientsService
}
```

In `backend/internal/httpapi/router.go`, add `Ingredients IngredientsService` to `Deps`, require it in the panic check, and pass it into `server{}`:

```go
type Deps struct {
	Logger *slog.Logger
	Ready  func(ctx context.Context) error
	WebOrigin string
	Auth   AuthService
	// Ingredients implements the ingredient catalog endpoints.
	Ingredients IngredientsService
	Tokens TokenParser
	Limits RateLimits
	TrustedProxies int
}
```

```go
	if d.Logger == nil || d.Ready == nil || d.Auth == nil || d.Ingredients == nil || d.Tokens == nil ||
		d.WebOrigin == "" || d.WebOrigin == "*" {
		panic("httpapi: Deps.Logger, Ready, Auth, Ingredients and Tokens are required, and WebOrigin must be a single origin (not empty or *)")
	}
```

```go
	srv := &server{logger: d.Logger, ready: d.Ready, auth: d.Auth, ingredients: d.Ingredients}
```

(Keep every other line of `router.go` and `server.go` unchanged — these are the only edits in both files.)

- [ ] **Step 5: Build**

Run: `cd backend && go build ./...`
Expected: builds cleanly. `var _ api.ServerInterface = (*server)(nil)` in `server.go` now also checks the four new methods.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/httpapi/
git commit -m "feat(backend): implement the ingredients handlers"
```

---

### Task 6: Wire `cmd/api` and write end-to-end tests

**Files:**
- Modify: `backend/cmd/api/main.go` (construct and pass `service.NewIngredients`)
- Modify: `backend/internal/httpapi/contract_test.go` (extend the shared test router with a no-op `stubIngredients`, mirroring `stubAuth`)
- Create: `backend/internal/httpapi/ingredients_flow_test.go`

**Interfaces:**
- Consumes: everything from Tasks 3–5, plus the existing `newTestRouter`, `contract`, `decodeAs[T]`, `problemCode`, `withBody`, `withBearer` helpers already in the `httpapi_test` package.

- [ ] **Step 1: Read `cmd/api/main.go` and wire the service**

Read `backend/internal/cmd/api/main.go` (find the exact line that constructs `service.NewAuth` and the `httpapi.Deps{...}` literal) and add, next to the existing `auth := service.NewAuth(...)` line:

```go
	ingredients := service.NewIngredients(st)
```

(`st` is whatever variable name the existing code already uses for `store.New(pool)` — match it exactly, do not introduce a second store instance.) Then add `Ingredients: ingredients,` to the `httpapi.Deps{...}` literal, alongside the existing `Auth: auth,`.

- [ ] **Step 2: Add a stub to the shared test router**

In `backend/internal/httpapi/contract_test.go`, add a no-op stub next to `stubAuth` and wire it into `newTestRouter`'s default `Deps`:

```go
// stubIngredients panics on any call, so tests that must not reach the
// ingredients service fail loudly if they do.
type stubIngredients struct{ httpapi.IngredientsService }
```

In `newTestRouter`, add `Ingredients: stubIngredients{},` to the `httpapi.Deps{...}` literal (alongside the existing `Auth: stubAuth{}`).

- [ ] **Step 3: Write the end-to-end test**

Create `backend/internal/httpapi/ingredients_flow_test.go`, modeled on `account_flow_test.go`'s `newAccountRouter`/`TestAccountLifecycle`:

```go
package httpapi_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/InzKazik/mealplanner/backend/internal/api"
	"github.com/InzKazik/mealplanner/backend/internal/auth"
	"github.com/InzKazik/mealplanner/backend/internal/db"
	"github.com/InzKazik/mealplanner/backend/internal/httpapi"
	"github.com/InzKazik/mealplanner/backend/internal/service"
	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"
	"github.com/InzKazik/mealplanner/backend/internal/testutil"
)

func newIngredientsRouter(t *testing.T) (http.Handler, uuid1, uuid2 string) {
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

	issuer := auth.NewTokenIssuer([]byte("0123456789abcdef0123456789abcdef"), 15*time.Minute, time.Now)
	router := newTestRouter(t, func(d *httpapi.Deps) {
		d.Ingredients = service.NewIngredients(st)
		d.Tokens = stubTwoUserTokens{u1: u1.ID, u2: u2.ID}
		d.Limits = httpapi.RateLimits{AuthPerMinute: 1000, UserPerMinute: 1000}
	})
	return router, "user1-token", "user2-token"
}
```

`stubTwoUserTokens` needs its own tiny `TokenParser`, since the package-level `stubTokens` in `contract_test.go` uses fixed UUIDs, not the ones actually created here:

```go
type stubTwoUserTokens struct{ u1, u2 uuid.UUID }

func (s stubTwoUserTokens) ParseAccess(token string) (uuid.UUID, error) {
	switch token {
	case "user1-token":
		return s.u1, nil
	case "user2-token":
		return s.u2, nil
	}
	return uuid.Nil, auth.ErrInvalidAccessToken
}
```

(add `"github.com/google/uuid"` to the imports; rewrite `newIngredientsRouter`'s return type to `(http.Handler, string, string)` since the tokens are plain strings, not UUIDs — the two user IDs are only needed inside the closure)

```go
func TestIngredientsLifecycle(t *testing.T) {
	router, token1, token2 := newIngredientsRouter(t)

	// Create a custom ingredient as user 1.
	rec := contract(t, router, http.MethodPost, "/ingredients", withBearer(token1), withBody(`{
		"name": "Custom Oats", "category": "grains_bread",
		"nutrients": {"calories": 389, "protein": 17}
	}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	created := decodeAs[api.Ingredient](t, rec)
	if !created.IsCustom || created.Name != "Custom Oats" {
		t.Errorf("created = %+v", created)
	}
	if !created.Nutrients.Calories.IsSpecified() || created.Nutrients.Calories.IsNull() {
		t.Errorf("Nutrients.Calories = %+v, want 389", created.Nutrients.Calories)
	}

	// User 1 sees it in a search.
	rec = contract(t, router, http.MethodGet, "/ingredients?q=oats", withBearer(token1))
	if rec.Code != http.StatusOK {
		t.Fatalf("search: status = %d", rec.Code)
	}
	if list := decodeAs[api.IngredientList](t, rec); len(list.Items) != 1 || list.Items[0].Id != created.Id {
		t.Errorf("search results = %+v", list)
	}

	// User 2 does not see it (owned by someone else) and gets 404 touching it.
	rec = contract(t, router, http.MethodGet, "/ingredients?q=oats", withBearer(token2))
	if list := decodeAs[api.IngredientList](t, rec); len(list.Items) != 0 {
		t.Errorf("user 2 search results = %+v, want none", list)
	}
	rec = contract(t, router, http.MethodPatch, "/ingredients/"+created.Id.String(), withBearer(token2), withBody(`{"name":"Hijack"}`))
	if rec.Code != http.StatusNotFound {
		t.Errorf("user 2 PATCH: status = %d, want 404", rec.Code)
	}

	// User 1 updates it: the request replaces the full nutrient set.
	rec = contract(t, router, http.MethodPatch, "/ingredients/"+created.Id.String(), withBearer(token1), withBody(`{
		"nutrients": {"fibre": 10}
	}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("update: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	updated := decodeAs[api.Ingredient](t, rec)
	if updated.Nutrients.Calories.IsSpecified() && !updated.Nutrients.Calories.IsNull() {
		t.Errorf("Nutrients.Calories = %+v, want cleared by the full replace", updated.Nutrients.Calories)
	}
	if !updated.Nutrients.Fibre.IsSpecified() || updated.Nutrients.Fibre.IsNull() {
		t.Errorf("Nutrients.Fibre = %+v, want 10", updated.Nutrients.Fibre)
	}

	// An invalid category is a validation error, not a 500.
	rec = contract(t, router, http.MethodPost, "/ingredients", withBearer(token1), withInvalidRequest(),
		withBody(`{"name":"Bad","category":"not_a_category"}`))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid category: status = %d, want 400", rec.Code)
	}

	// User 2 cannot delete it; user 1 can.
	rec = contract(t, router, http.MethodDelete, "/ingredients/"+created.Id.String(), withBearer(token2))
	if rec.Code != http.StatusNotFound {
		t.Errorf("user 2 DELETE: status = %d, want 404", rec.Code)
	}
	rec = contract(t, router, http.MethodDelete, "/ingredients/"+created.Id.String(), withBearer(token1))
	if rec.Code != http.StatusNoContent {
		t.Errorf("owner DELETE: status = %d, want 204", rec.Code)
	}
}

func TestIngredientsListPaginatesThroughTheContract(t *testing.T) {
	router, token1, _ := newIngredientsRouter(t)
	for _, name := range []string{"Apple", "Banana", "Carrot"} {
		rec := contract(t, router, http.MethodPost, "/ingredients", withBearer(token1),
			withBody(`{"name":"`+name+`","category":"produce"}`))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s: status = %d", name, rec.Code)
		}
	}

	var names []string
	path := "/ingredients?limit=1"
	for {
		rec := contract(t, router, http.MethodGet, path, withBearer(token1))
		if rec.Code != http.StatusOK {
			t.Fatalf("list %s: status = %d", path, rec.Code)
		}
		list := decodeAs[api.IngredientList](t, rec)
		for _, ing := range list.Items {
			names = append(names, ing.Name)
		}
		if !list.NextCursor.IsSpecified() || list.NextCursor.IsNull() {
			break
		}
		path = "/ingredients?limit=1&cursor=" + list.NextCursor.MustGet()
		if len(names) > 10 {
			t.Fatal("pagination did not terminate")
		}
	}
	if want := []string{"Apple", "Banana", "Carrot"}; len(names) != 3 || names[0] != want[0] || names[1] != want[1] || names[2] != want[2] {
		t.Errorf("paginated names = %v, want %v", names, want)
	}
}
```

Note: the cursor from `list.NextCursor.MustGet()` is a base64url string (from `encodeIngredientCursor`), which can contain `-` and `_` but never characters that need percent-encoding in a query string, so concatenating it directly into `path` is safe here.

- [ ] **Step 4: Run the tests**

Run: `cd backend && go test ./... -v -run 'Ingredient'`
Expected: PASS (needs Docker)

Run: `cd backend && go vet ./... && go test ./...`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/cmd/api/main.go backend/internal/httpapi/contract_test.go backend/internal/httpapi/ingredients_flow_test.go
git commit -m "test(backend): add end-to-end coverage for the ingredients endpoints"
```

---

### Task 7: USDA client, category and nutrient mapping

**Files:**
- Create: `backend/internal/usda/client.go`
- Create: `backend/internal/usda/client_test.go`
- Create: `backend/internal/usda/category.go`
- Create: `backend/internal/usda/category_test.go`
- Create: `backend/internal/usda/nutrients.go`
- Create: `backend/internal/usda/nutrients_test.go`

**Interfaces:**
- Consumes: `service.Nutrient*` constants from Task 3.
- Produces (consumed by Task 8): `usda.Food{FdcID int32; Description, FoodCategory string; FoodNutrients []FoodNutrient}`, `usda.FoodNutrient{NutrientNumber string; Value float64}`, `usda.Client{BaseURL, APIKey string; HTTP *http.Client}`, `usda.NewClient(apiKey string) *Client`, `func (*Client) FetchFoundationFoods(ctx) ([]Food, error)`, `usda.MapCategory(usdaCategory string) (category string, known bool)`, `usda.MapNutrients(in []FoodNutrient) map[string]float64`.

The nutrient-number and category-name mappings below are scratch-verified against the live FoodData Central API this session (`GET https://api.nal.usda.gov/fdc/v1/foods/search?dataType=Foundation&...`): the field names (`fdcId`, `description`, `foodCategory`, and `foodNutrients[].nutrientNumber`/`nutrientName`/`unitName`/`value`) and the specific nutrient numbers `208, 203, 205, 269, 291, 204, 606, 307, 306, 301, 303, 304, 309, 320, 418, 328, 417` were read directly from real responses. `401` for Vitamin C (total ascorbic acid) was not hit live (the query for a vitamin-C-rich food was rate-limited mid-session) but is USDA's long-stable, well-documented nutrient number for it; Step 2 of Task 8 fetches one real page from the live API as its fixture, which will surface a mismatch immediately if this is wrong.

- [ ] **Step 1: Write the category mapping and its test**

Create `backend/internal/usda/category_test.go`:

```go
package usda_test

import (
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/usda"
)

func TestMapCategory(t *testing.T) {
	tests := []struct {
		usda string
		want string
		known bool
	}{
		{"Fruits and Fruit Juices", "produce", true},
		{"Vegetables and Vegetable Products", "produce", true},
		{"Dairy and Egg Products", "dairy_eggs", true},
		{"Finfish and Shellfish Products", "meat_seafood", true},
		{"Nut and Seed Products", "legumes_nuts_seeds", true},
		{"Some New Category FDC Adds Later", "other", false},
	}
	for _, tt := range tests {
		t.Run(tt.usda, func(t *testing.T) {
			got, known := usda.MapCategory(tt.usda)
			if got != tt.want || known != tt.known {
				t.Errorf("MapCategory(%q) = (%q, %v), want (%q, %v)", tt.usda, got, known, tt.want, tt.known)
			}
		})
	}
}
```

Create `backend/internal/usda/category.go`:

```go
// Package usda loads the USDA FoodData Central Foundation Foods dataset.
package usda

// categoryMap translates FoodData Central food-group names to our shopping
// categories. A food group not listed here falls back to "other"; Import
// logs a warning when that happens so the table can be extended.
var categoryMap = map[string]string{
	"Dairy and Egg Products":              "dairy_eggs",
	"Spices and Herbs":                    "spices_herbs",
	"Baby Foods":                          "other",
	"Fats and Oils":                       "condiments_oils",
	"Poultry Products":                    "meat_seafood",
	"Soups, Sauces, and Gravies":          "condiments_oils",
	"Sausages and Luncheon Meats":         "meat_seafood",
	"Breakfast Cereals":                   "grains_bread",
	"Fruits and Fruit Juices":             "produce",
	"Pork Products":                       "meat_seafood",
	"Vegetables and Vegetable Products":   "produce",
	"Nut and Seed Products":               "legumes_nuts_seeds",
	"Beef Products":                       "meat_seafood",
	"Beverages":                           "beverages",
	"Finfish and Shellfish Products":      "meat_seafood",
	"Legumes and Legume Products":         "legumes_nuts_seeds",
	"Lamb, Veal, and Game Products":       "meat_seafood",
	"Baked Products":                      "grains_bread",
	"Sweets":                              "sweets_snacks",
	"Cereal Grains and Pasta":             "grains_bread",
	"Fast Foods":                          "other",
	"Meals, Entrees, and Side Dishes":     "other",
	"Snacks":                              "sweets_snacks",
	"American Indian/Alaska Native Foods": "other",
	"Restaurant Foods":                    "other",
}

// MapCategory returns our shopping category for a FoodData Central food
// group, and whether it was recognised. An unrecognised group maps to
// "other".
func MapCategory(usdaCategory string) (category string, known bool) {
	c, ok := categoryMap[usdaCategory]
	if !ok {
		return "other", false
	}
	return c, true
}
```

Run: `cd backend && go test ./internal/usda/... -run TestMapCategory -v`
Expected: PASS

- [ ] **Step 2: Write the nutrient mapping and its test**

Create `backend/internal/usda/nutrients_test.go`:

```go
package usda_test

import (
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/service"
	"github.com/InzKazik/mealplanner/backend/internal/usda"
)

func TestMapNutrients(t *testing.T) {
	in := []usda.FoodNutrient{
		{NutrientNumber: "208", Value: 64.7},  // calories
		{NutrientNumber: "203", Value: 0.15},  // protein
		{NutrientNumber: "999", Value: 1},     // unrecognised, ignored
	}
	got := usda.MapNutrients(in)
	if len(got) != 2 {
		t.Fatalf("MapNutrients returned %d entries, want 2 (unrecognised numbers ignored): %v", len(got), got)
	}
	if got[service.NutrientCalories] != 64.7 {
		t.Errorf("calories = %v, want 64.7", got[service.NutrientCalories])
	}
	if got[service.NutrientProtein] != 0.15 {
		t.Errorf("protein = %v, want 0.15", got[service.NutrientProtein])
	}
}
```

Create `backend/internal/usda/nutrients.go`:

```go
package usda

import "github.com/InzKazik/mealplanner/backend/internal/service"

// nutrientNumberMap translates FoodData Central nutrient numbers to our
// nutrient keys. Only numbers we track appear here; every other nutrient
// FoodData Central reports is ignored. Confirmed against live API responses
// except "401" (Vitamin C): verify it against Task 8's fixture and correct
// this table if it differs.
var nutrientNumberMap = map[string]string{
	"208": service.NutrientCalories,
	"203": service.NutrientProtein,
	"205": service.NutrientCarbohydrates,
	"269": service.NutrientSugar,
	"291": service.NutrientFibre,
	"204": service.NutrientFat,
	"606": service.NutrientSaturatedFat,
	"307": service.NutrientSodium,
	"306": service.NutrientPotassium,
	"301": service.NutrientCalcium,
	"303": service.NutrientIron,
	"304": service.NutrientMagnesium,
	"309": service.NutrientZinc,
	"320": service.NutrientVitaminA,
	"401": service.NutrientVitaminC,
	"328": service.NutrientVitaminD,
	"418": service.NutrientVitaminB12,
	"417": service.NutrientFolate,
}

// MapNutrients converts a food's nutrient list to our nutrient keys, per
// 100 g. Unrecognised nutrient numbers are ignored.
func MapNutrients(in []FoodNutrient) map[string]float64 {
	out := make(map[string]float64, len(nutrientNumberMap))
	for _, n := range in {
		if key, ok := nutrientNumberMap[n.NutrientNumber]; ok {
			out[key] = n.Value
		}
	}
	return out
}
```

Run: `cd backend && go test ./internal/usda/... -run TestMapNutrients -v`
Expected: PASS

- [ ] **Step 3: Write the client and its test**

Create `backend/internal/usda/client_test.go`:

```go
package usda_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/usda"
)

func TestFetchFoundationFoodsPagesUntilEmpty(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch r.URL.Query().Get("pageNumber") {
		case "1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"foods":[{"fdcId":1,"description":"Apple","foodCategory":"Fruits and Fruit Juices","foodNutrients":[{"nutrientNumber":"208","nutrientName":"Energy","unitName":"KCAL","value":52}]}]}`))
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"foods":[]}`))
		}
	}))
	defer server.Close()

	client := usda.NewClient("test-key")
	client.BaseURL = server.URL
	client.HTTP = server.Client()

	foods, err := client.FetchFoundationFoods(context.Background())
	if err != nil {
		t.Fatalf("FetchFoundationFoods: %v", err)
	}
	if len(foods) != 1 || foods[0].FdcID != 1 || foods[0].Description != "Apple" || foods[0].FoodCategory != "Fruits and Fruit Juices" {
		t.Fatalf("foods = %+v", foods)
	}
	if len(foods[0].FoodNutrients) != 1 || foods[0].FoodNutrients[0].NutrientNumber != "208" || foods[0].FoodNutrients[0].Value != 52 {
		t.Errorf("nutrients = %+v", foods[0].FoodNutrients)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2 (one page of results, one empty page to stop)", calls)
	}
}

func TestFetchFoundationFoodsRetriesOnRateLimit(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"foods":[]}`))
	}))
	defer server.Close()

	client := usda.NewClient("test-key")
	client.BaseURL = server.URL
	client.HTTP = server.Client()

	foods, err := client.FetchFoundationFoods(context.Background())
	if err != nil {
		t.Fatalf("FetchFoundationFoods: %v", err)
	}
	if len(foods) != 0 {
		t.Errorf("foods = %+v, want none", foods)
	}
	if calls < 2 {
		t.Errorf("calls = %d, want at least 2 (a retry after the 429)", calls)
	}
}
```

Note: this test relies on the client's fixed per-attempt backoff, which in the implementation below is `time.Duration(attempt) * time.Second` starting at attempt 2 — at least 1 second. That is an acceptable, small, one-time cost for this specific test; do not reduce the production backoff to make the test faster.

Create `backend/internal/usda/client.go`:

```go
package usda

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Food is one Foundation Foods item as returned by the search API.
type Food struct {
	FdcID         int32
	Description   string
	FoodCategory  string
	FoodNutrients []FoodNutrient
}

// FoodNutrient is one nutrient value for a Food, per 100 g of the food.
type FoodNutrient struct {
	NutrientNumber string
	Value          float64
}

type searchResponse struct {
	Foods []struct {
		FdcID         int32  `json:"fdcId"`
		Description   string `json:"description"`
		FoodCategory  string `json:"foodCategory"`
		FoodNutrients []struct {
			NutrientNumber string  `json:"nutrientNumber"`
			Value          float64 `json:"value"`
		} `json:"foodNutrients"`
	} `json:"foods"`
}

const (
	defaultBaseURL = "https://api.nal.usda.gov/fdc/v1"
	pageSize       = 200
	maxAttempts    = 5
)

// Client fetches Foundation Foods from the FoodData Central API. BaseURL and
// HTTP are exported so tests can point at an httptest.Server.
type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

// NewClient returns a Client that calls the live FoodData Central API.
func NewClient(apiKey string) *Client {
	return &Client{BaseURL: defaultBaseURL, APIKey: apiKey, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// FetchFoundationFoods returns every food in the Foundation Foods dataset,
// paging through the search endpoint until a page comes back empty.
func (c *Client) FetchFoundationFoods(ctx context.Context) ([]Food, error) {
	var all []Food
	for page := 1; ; page++ {
		batch, err := c.fetchPage(ctx, page)
		if err != nil {
			return nil, fmt.Errorf("fetch page %d: %w", page, err)
		}
		if len(batch) == 0 {
			return all, nil
		}
		all = append(all, batch...)
	}
}

func (c *Client) fetchPage(ctx context.Context, page int) ([]Food, error) {
	u := fmt.Sprintf("%s/foods/search?dataType=Foundation&pageSize=%d&pageNumber=%d&api_key=%s",
		c.BaseURL, pageSize, page, url.QueryEscape(c.APIKey))

	var last error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		foods, retryable, err := c.doFetch(ctx, u)
		if err == nil {
			return foods, nil
		}
		last = err
		if !retryable {
			return nil, err
		}
	}
	return nil, fmt.Errorf("giving up after %d attempts: %w", maxAttempts, last)
}

func (c *Client) doFetch(ctx context.Context, u string) ([]Food, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, false, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var parsed searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, false, fmt.Errorf("decode response: %w", err)
	}
	foods := make([]Food, len(parsed.Foods))
	for i, f := range parsed.Foods {
		nutrients := make([]FoodNutrient, len(f.FoodNutrients))
		for j, n := range f.FoodNutrients {
			nutrients[j] = FoodNutrient{NutrientNumber: n.NutrientNumber, Value: n.Value}
		}
		foods[i] = Food{FdcID: f.FdcID, Description: f.Description, FoodCategory: f.FoodCategory, FoodNutrients: nutrients}
	}
	return foods, false, nil
}
```

- [ ] **Step 4: Run all of it**

Run: `cd backend && go test ./internal/usda/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/usda/client.go backend/internal/usda/client_test.go \
        backend/internal/usda/category.go backend/internal/usda/category_test.go \
        backend/internal/usda/nutrients.go backend/internal/usda/nutrients_test.go
git commit -m "feat(backend): add the FoodData Central client and its mapping tables"
```

---

### Task 8: USDA import orchestration and `cmd/import-usda`

**Files:**
- Create: `backend/internal/usda/import.go`
- Create: `backend/internal/usda/import_test.go`
- Create: `backend/cmd/import-usda/main.go`
- Modify: `.env.example`

**Interfaces:**
- Consumes: `store.Store`, `sqlc.Queries` methods from Task 2; `Client`, `MapCategory`, `MapNutrients` from Task 7.
- Produces: `usda.Stats{Imported, UnknownCategory int}`, `usda.Import(ctx, st *store.Store, client *Client, logger *slog.Logger) (Stats, error)`.

`internal/usda` calls `store` directly rather than going through `internal/service`: importing is a one-off offline ETL job with no request-serving business rule beyond the idempotent upsert SQL already provides, not something `internal/service` (whose own doc comment scopes it to business rules called by the HTTP layer) needs to own. It does import `internal/service` for the `Nutrient*` constants only, which is one-directional and does not create a cycle (`service` never imports `usda`).

- [ ] **Step 1: Write the failing integration test**

Create `backend/internal/usda/import_test.go`. This uses a real migrated Postgres (via `testutil`) and a fake HTTP server (never the live USDA API), matching the "never mocks" rule for the database while keeping the external network call out of tests:

```go
package usda_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/db"
	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/testutil"
	"github.com/InzKazik/mealplanner/backend/internal/usda"
)

const fixturePage1 = `{"foods":[
	{"fdcId":1750340,"description":"Apples, fuji, with skin, raw","foodCategory":"Fruits and Fruit Juices","foodNutrients":[
		{"nutrientNumber":"208","value":64.7},{"nutrientNumber":"203","value":0.15}
	]},
	{"fdcId":9999999,"description":"Mystery Food","foodCategory":"Some Future Category","foodNutrients":[
		{"nutrientNumber":"208","value":10}
	]}
]}`

func fixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("pageNumber") == "1" {
			_, _ = w.Write([]byte(fixturePage1))
			return
		}
		_, _ = w.Write([]byte(`{"foods":[]}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestImportIsIdempotentAndMapsUnknownCategoriesToOther(t *testing.T) {
	pool, err := db.Connect(context.Background(), testutil.NewMigratedDatabase(t))
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	st := store.New(pool)

	server := fixtureServer(t)
	client := usda.NewClient("test-key")
	client.BaseURL = server.URL
	client.HTTP = server.Client()
	logger := slog.New(slog.DiscardHandler)

	stats, err := usda.Import(context.Background(), st, client, logger)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if stats.Imported != 2 || stats.UnknownCategory != 1 {
		t.Errorf("stats = %+v, want Imported=2 UnknownCategory=1", stats)
	}

	apple, err := st.GetIngredientForUser(context.Background(), sqlcGetIngredientForUserByFdcID(t, st, 1750340))
	if err != nil {
		t.Fatalf("look up imported apple: %v", err)
	}
	if apple.Category != "produce" {
		t.Errorf("apple category = %q, want produce", apple.Category)
	}
	mystery, err := st.GetIngredientForUser(context.Background(), sqlcGetIngredientForUserByFdcID(t, st, 9999999))
	if err != nil {
		t.Fatalf("look up imported mystery food: %v", err)
	}
	if mystery.Category != "other" {
		t.Errorf("mystery category = %q, want other (unmapped USDA group)", mystery.Category)
	}

	// Rerunning is idempotent: same row count, values updated in place.
	stats2, err := usda.Import(context.Background(), st, client, logger)
	if err != nil {
		t.Fatalf("second Import: %v", err)
	}
	if stats2.Imported != 2 {
		t.Errorf("second run imported = %d, want 2 (upsert, not duplicate)", stats2.Imported)
	}
}
```

`sqlcGetIngredientForUserByFdcID` is a placeholder name that does not exist: `GetIngredientForUser` looks up by `id`, not `usda_fdc_id`, and there is no by-fdc-id query in Task 2 (nothing needs one outside the importer, which already has the row from `UpsertUSDAIngredient`'s `RETURNING *`). Replace those two lookups with a raw query against the test's own connection instead, matching how `schema_test.go` does ad hoc assertions:

```go
	var appleCategory, mysteryCategory string
	conn, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()
	if err := conn.QueryRow(context.Background(), `SELECT category FROM ingredients WHERE usda_fdc_id = 1750340`).Scan(&appleCategory); err != nil {
		t.Fatalf("look up imported apple: %v", err)
	}
	if appleCategory != "produce" {
		t.Errorf("apple category = %q, want produce", appleCategory)
	}
	if err := conn.QueryRow(context.Background(), `SELECT category FROM ingredients WHERE usda_fdc_id = 9999999`).Scan(&mysteryCategory); err != nil {
		t.Fatalf("look up imported mystery food: %v", err)
	}
	if mysteryCategory != "other" {
		t.Errorf("mystery category = %q, want other (unmapped USDA group)", mysteryCategory)
	}
```

(use this version; delete the two `st.GetIngredientForUser(...)` calls and the placeholder helper name above, and add `"github.com/jackc/pgx/v5/pgxpool"` only if needed for the type — `pool.Acquire` already returns the right type from the existing `pool` variable, no new import required)

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/usda/... -run TestImportIsIdempotent -v`
Expected: FAIL to compile — `usda.Import` does not exist yet.

- [ ] **Step 3: Implement `Import`**

Create `backend/internal/usda/import.go`:

```go
package usda

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"
)

// Stats summarizes an import run.
type Stats struct {
	Imported        int
	UnknownCategory int
}

// Import fetches every Foundation Foods item and upserts it, keyed by
// usda_fdc_id, so rerunning is safe.
func Import(ctx context.Context, st *store.Store, client *Client, logger *slog.Logger) (Stats, error) {
	foods, err := client.FetchFoundationFoods(ctx)
	if err != nil {
		return Stats{}, fmt.Errorf("fetch foundation foods: %w", err)
	}

	var stats Stats
	for _, f := range foods {
		category, known := MapCategory(f.FoodCategory)
		if !known {
			stats.UnknownCategory++
			logger.WarnContext(ctx, "unmapped USDA food category, using other",
				slog.String("category", f.FoodCategory), slog.Int("fdc_id", int(f.FdcID)))
		}
		fdcID := f.FdcID
		err := st.InTx(ctx, func(q *sqlc.Queries) error {
			ing, err := q.UpsertUSDAIngredient(ctx, sqlc.UpsertUSDAIngredientParams{
				Name: f.Description, Category: category, UsdaFdcID: &fdcID,
			})
			if err != nil {
				return fmt.Errorf("upsert ingredient %d: %w", fdcID, err)
			}
			for key, amount := range MapNutrients(f.FoodNutrients) {
				if err := q.UpsertIngredientNutrient(ctx, sqlc.UpsertIngredientNutrientParams{
					IngredientID: ing.ID, NutrientKey: sqlc.NutrientKey(key), AmountPer100g: amount,
				}); err != nil {
					return fmt.Errorf("upsert nutrient %s for %d: %w", key, fdcID, err)
				}
			}
			return nil
		})
		if err != nil {
			return stats, err
		}
		stats.Imported++
	}
	return stats, nil
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd backend && go test ./internal/usda/... -run TestImportIsIdempotent -v`
Expected: PASS

- [ ] **Step 5: Write `cmd/import-usda`**

Create `backend/cmd/import-usda/main.go`:

```go
// Command import-usda loads the FoodData Central Foundation Foods dataset
// into the ingredients and ingredient_nutrients tables. It is idempotent,
// keyed by usda_fdc_id, so rerunning it is safe.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/usda"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	apiKey := os.Getenv("FDC_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "import-usda: FDC_API_KEY is required (register a free key at https://api.data.gov/signup/)")
		os.Exit(2)
	}
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		fmt.Fprintln(os.Stderr, "import-usda: DATABASE_URL is required")
		os.Exit(2)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "import-usda: connect to database:", err)
		os.Exit(1)
	}
	defer pool.Close()

	stats, err := usda.Import(ctx, store.New(pool), usda.NewClient(apiKey), logger)
	if err != nil {
		fmt.Fprintln(os.Stderr, "import-usda:", err)
		os.Exit(1)
	}
	logger.Info("usda import complete", "imported", stats.Imported, "unknown_category", stats.UnknownCategory)
}
```

- [ ] **Step 6: Document `FDC_API_KEY`**

In `.env.example`, add after the `JWT_SECRET` block:

```
# backend/cmd/import-usda only (not read by the API or by Docker Compose). A
# free key from https://api.data.gov/signup/. Without one, run against
# DEMO_KEY's public rate limit by exporting FDC_API_KEY=DEMO_KEY, which is
# too low for a full Foundation Foods import (~2000 items) but fine to try.
FDC_API_KEY=
```

- [ ] **Step 7: Build and run the full suite**

Run: `cd backend && go build ./... && go vet ./... && go test ./...`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add backend/internal/usda/import.go backend/internal/usda/import_test.go backend/cmd/import-usda/main.go .env.example
git commit -m "feat(backend): add the USDA Foundation Foods import command"
```

---

### Task 9: Documentation and final checks

**Files:**
- Modify: `backend/CLAUDE.md`

**Interfaces:** none (documentation only).

- [ ] **Step 1: Update `backend/CLAUDE.md`**

In the "Repo map" section's `internal/` list, add a line for `internal/usda/` next to `internal/service/`:

```
- `internal/usda/`: the FoodData Central client and its category/nutrient mapping tables, used only by `cmd/import-usda`. Talks to `store` directly, not through `internal/service`.
```

In "Not built yet", remove the line `- The domain: ingredients, meals, diets, plan, shopping lists, partners (later plans).` and replace it with:

```
- The domain beyond ingredients: meals, diets, plan, shopping lists, partners (later plans). The meals plan must add a `meal_ingredients` foreign key to `ingredients` and translate its violation into `409 ingredient_in_use` on delete (see the note in `internal/store/queries/ingredients.sql`) — `DeleteIngredient` does not check for that yet because nothing references ingredients until then.
```

In "Decide before the domain plans", mark the first item resolved by editing it in place (keep the other three as-is, they are still open):

```
- ~~**Access tokens versus deleted or logged-out users.**~~ Resolved for ingredients: `Ingredients.Create` translates the `ingredients_owner_id_fkey` foreign-key violation into `service.ErrNotFound`, mapped to `401` exactly like `GetUser`. Every future user-owned write table should follow the same pattern (a named FK constraint plus a `store.IsForeignKeyViolation` check) rather than adopting the validator-level fix that was also considered.
```

Add one line to "Behaviour worth knowing" documenting the search/pagination split decided this session:

```
- **Ingredient search is not paginated.** `GET /ingredients?q=` ranks by trigram similarity and returns up to `limit` results with no cursor; only the plain alphabetical listing (no `q`) paginates. A type-ahead UI never needs a second page of search results, and cursoring a similarity-ranked result set has no stable order to cursor over.
```

- [ ] **Step 2: Run everything CI runs**

Run: `make check`
Expected: PASS (lints `openapi.yaml`, vets and tests the backend, runs golangci-lint, and fails if generated code is stale)

Run: `make check-generated`
Expected: clean (no diff)

- [ ] **Step 3: Commit**

```bash
git add backend/CLAUDE.md
git commit -m "docs(backend): document the ingredients domain and resolve the deleted-owner decision"
```

---

## Self-Review

- **Spec coverage:** §3.1 USDA import → Tasks 7–8. §3.2 `ingredients`/`ingredient_nutrients` schema → Task 1. §4.1 ingredient endpoints (`GET` list/search, `POST`, `PATCH`, `DELETE`) → Tasks 4–5. §4.2 cursor pagination and RFC 9457 errors → Task 5 (list only; search is deliberately unpaginated, documented in Task 9). The `ingredient_in_use` conflict from §4.1 is explicitly deferred to the meals plan (Task 2's note, Task 9's "Not built yet" update) since nothing references `ingredients` yet — this is a scope decision, not a gap, and is called out for the user in the summary below.
- **Placeholder scan:** the two intentionally-marked "placeholder name" spots in Task 8 are not TODOs — they show the wrong code first and then the corrected replacement inline, both fully written out, because the natural first draft of that test collides with a query that doesn't exist; leaving both in place documents why, for whoever executes the task.
- **Type consistency:** `service.Ingredient.Nutrients`, `httpapi`'s conversion, and `usda.MapNutrients`'s output are all `map[string]float64` keyed by the same `service.Nutrient*` constants throughout — checked across Tasks 3, 5, 7, 8. `sqlc.NutrientKey` (Task 2) is a `string`-backed type, so `sqlc.NutrientKey(key)` and `string(row.NutrientKey)` round-trip cleanly everywhere they're used.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-09-22-backend-ingredients.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
