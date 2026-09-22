# Backend Diets and Plan Domain Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add diet templates (reusable day-count meal schedules) and the calendar plan they apply into — `diet_templates`/`template_slots`/`plan_entries` schema, a CRUD + atomic-slots-replace + apply + copy API for templates, and a date-range plan API with daily nutrition totals reused from the meals domain — and close the `meal_in_use` deletion guard the meals plan deliberately deferred, extending the account-deletion ordering fix it already established.

**Architecture:** Three new tables follow the existing `httpapi` → `service` → `store` layering. Two new services: `DietTemplates` (CRUD templates, atomic slot replace, copy, and `Apply`, which writes into `plan_entries`) and `Plan` (the calendar: date-range read with computed totals, set/swap/delete one entry). Both hold a `*store.Store` directly for existence/ownership checks on `meals`, mirroring how `Meals` reads `ingredients` rows directly rather than depending on the `Ingredients` service. The one deliberate exception: `Plan` takes a `*service.Meals` dependency specifically to reuse `Meals.Get`'s nutrition computation for each entry's contribution — duplicating that unit-conversion/null-propagation logic a third time was rejected in favor of one clean, one-directional dependency. See Global Constraints for the snack-multiplicity, day_count-immutability, apply-conflict, and account-deletion-ordering decisions this plan makes.

**Tech Stack:** Go 1.26, chi, pgx/pgxpool, goose, sqlc, oapi-codegen — no new dependencies.

**Spec:** `docs/superpowers/specs/2026-09-21-meal-planner-design.md` (sections 3.4, 3.6, 4.1, 4.2, 6)

## Global Constraints

- OpenAPI 3.0.3 is the source of truth: edit `openapi.yaml` first, then `make generate`, then implement. Never hand-edit `backend/internal/api/api.gen.go` or `backend/internal/store/sqlc/`.
- No SQL outside `backend/internal/store`. Dependencies point one way: `httpapi` → `service` → `store`.
- Global `security: bearerAuth` protects every operation by default.
- Errors are RFC 9457 `application/problem+json` with a stable `code`.
- Unseen resources return `404`, never `403`.
- Migrations are goose SQL files named `NNNNN_description.sql`. `diet_templates` and `plan_entries` get `created_at`/`updated_at` plus the `set_updated_at()` trigger; `template_slots` deliberately has neither — it is a line-item table fully owned and replaced by its parent (`diet_templates`), never edited in place, exactly like `meal_ingredients`. `plan_entries` DOES get timestamps despite also being written in bulk by `Apply`, because unlike `template_slots` it is also a first-class resource edited one row at a time via `PUT /plan/{date}/{slot}` — knowing when a specific day's plan was last touched is useful on its own.
- **OpenAPI contract and handler implementation are combined into a single task per resource group (Tasks 7 and 8), not split across two tasks like the meals plan's Tasks 5/6.** `backend/internal/httpapi/server.go` has `var _ api.ServerInterface = (*server)(nil)` — adding new `ServerInterface` methods breaks that assertion until something implements them. The meals plan split contract and handlers into separate tasks and discovered this the hard way: its Task 5 had to add a temporary stub file just to keep `go build` green, which Task 6 then fully replaced. This plan avoids the problem entirely by keeping each resource group's contract change and its handlers in the same task, so `go build` never sees a broken interim state.
- **`plan_entries` allows multiple `snack` rows per day, exactly like `template_slots`, but `PUT/DELETE /plan/{date}/{slot}` can only address a slot by its enum value, not by row id.** The spec states "Multiple `snack` rows per day are allowed" for `template_slots` and doesn't repeat it for `plan_entries`, but applying a template with two snacks on the same day must still work, and a template's slots and a day's plan entries should behave the same way — so this plan extends the same partial-unique-index pattern (`UNIQUE (..., slot) WHERE slot != 'snack'`) to `plan_entries`. The consequence, since the URL shape can't disambiguate among several snacks: `PUT /plan/{date}/snack` always **creates a new** snack entry (there is nothing existing to select and overwrite), and `DELETE /plan/{date}/snack` deletes **all** snack entries for that date (the only unambiguous meaning available from this URL shape). `breakfast`/`lunch`/`dinner` are true single-row upserts.
- **`day_count` is immutable after a template is created.** Changing it would require re-validating every existing slot's `day_index` against the new value — the same validate-before-write problem `ReplaceSlots` already solves once. A template with the wrong day count can simply be recreated or copied. `UpdateDietTemplateInput` covers `name` and `shared_with_partner` only.
- **`POST /diet-templates` creates an empty template; slots are always added via `PUT /diet-templates/{id}/slots`.** Same reasoning as the meals plan's `POST /meals`: one atomic-replace path keeps the day_index/day_count and meal-visibility validation in one place instead of duplicating it across create and replace.
- **`portion` defaults to `1` and is optional on write**, matching the spec's "portion (numeric, default 1)" for both `template_slots` and `plan_entries`. The OpenAPI request schemas mark it optional with `default: 1`; the Go request types carry it as `*float64` and the handlers substitute `1.0` when absent.
- **A manual `PUT /plan/{date}/{slot}` always clears `from_template_id` to `NULL`, even when it overwrites an entry that came from a template.** `from_template_id` tracks provenance — "this row was written by applying template X" — and a manually swapped meal is no longer that; the spec's "entries are independent" language after applying already establishes that an applied entry has no special protection from being edited normally.
- **Applying a template without `overwrite: true` checks only the non-`snack` (date, slot) pairs the template would write** — `snack` entries never conflict (they're always additive, per the rule above), so there is nothing to check for them. With `overwrite: true`, the same non-`snack` target rows are replaced (delete-then-insert on the specific date+slot); `snack` slots from the template are always added, never deduplicated against existing snacks.
- **`meal_id` in `template_slots` and `plan_entries` is a `NO ACTION` foreign key, exactly like `meal_ingredients.ingredient_id`.** Deleting a meal that is still scheduled in a template or on the calendar must fail (`409 meal_in_use`), not silently vanish from someone's plan. Task 4 teaches `Meals.Delete` to translate both constraint names.
- **Account deletion needs a third and fourth table added to `Auth.DeleteUser`'s explicit pre-delete ordering.** The meals plan's final review found and fixed a cascade-ordering bug: `users` cascades to `ingredients` before `meals`, so a `meal_ingredients` NO ACTION violation could abort the delete; the fix was deleting the user's meals first, in the same transaction. The same problem now exists one level up: `template_slots`/`plan_entries` reference `meals` with NO ACTION, and `diet_templates`/`plan_entries` both cascade from `users`. `Auth.DeleteUser`'s transaction (Task 4) must delete in this exact order: `plan_entries` → `diet_templates` (cascades `template_slots`) → `meals` (already there) → `users`.
- **Nutrition totals reuse `Meals.Get`, not a reimplementation.** `Plan.GetRange` calls `Meals.Get` once per distinct meal referenced in the date range (deduplicated) and multiplies each entry's contribution by its `portion`. This is the one place in the codebase where a service depends on another service's public method rather than reading raw rows via `store` — justified because the alternative is a third copy of the unit-conversion and null-propagation logic (`ingredients.go` → `meals.go` → here), which the meals plan's final review specifically flagged as worth consolidating before this domain landed.
- **A missing nutrient key propagates the same way it does for a single meal, one level up.** A day's total for a key is absent if any entry that day contributes a meal with that key absent. A day with zero entries reports `0` for all 18 keys — the same "well-defined empty sum, different from unknown" rule the meals plan established. (The spec's "weekly totals" are left to the client to sum from daily totals — see the `GET /plan` task for why this plan does not also compute a period total server-side.)
- **A generic `uniqueUUIDs` helper replaces three near-identical dedup functions.** The meals plan's final review flagged `meals.go`'s `uniqueIngredientIDs` and `httpapi/meals.go`'s cursor codec as duplicated-but-tolerated patterns, noting consolidation was "worth it once a third instance appears." This plan is about to add two more (deduping a template's slot meal-ids, deduping a date range's entry meal-ids) — Task 5 adds one generic helper and refactors `meals.go` to use it instead of adding two more copies.
- **`GET /plan` is capped at a 92-day range** (roughly a quarter) — the spec doesn't state a limit, and this is not a growing list that needs cursor pagination (§4.2 reserves that for meals/ingredients/diet-templates), but an unbounded range would let a client request years of data in one call. Enforced in the service layer (`ErrPlanRangeTooLong`, `400`), since OpenAPI's JSON Schema can't express a cross-field constraint between `from` and `to`.
- Nutrient set (18, fixed, matches the ingredients/meals plans): `calories`, `protein`, `carbohydrates`, `sugar`, `fibre`, `fat`, `saturated_fat`, `sodium`, `potassium`, `calcium`, `iron`, `magnesium`, `zinc`, `vitamin_a`, `vitamin_c`, `vitamin_d`, `vitamin_b12`, `folate`.
- **Partner visibility is out of scope for this plan**, exactly like the meals plan. The `partnerships` table still does not exist (confirmed by grep) — `diet_templates.shared_with_partner` is stored but every read checks `owner_id` only.
- **`users.target_kcal`/`target_protein_g`/`target_carbs_g`/`target_fat_g` already exist** (added in `00002_users.sql`) — no migration needed for target comparison, just a read via the existing `GetUserByID` query.
- Any new environment variable is added to `.env.example` in the same commit (none are expected in this plan).

---

## File Structure

- `backend/migrations/00006_diets_and_plan.sql` — the three tables, the named FKs (`template_slots_meal_id_fkey`, `plan_entries_meal_id_fkey`) Task 4 relies on, partial unique indexes, triggers.
- `backend/internal/db/schema_test.go` — gains `TestDietsAndPlanSchemaEnforcesItsConstraints`.
- `backend/internal/store/queries/meals.sql` — gains `GetMealsForUser` (batch, owner-scoped lookup used by both new services to validate a slot/entry's `meal_id`).
- `backend/internal/store/queries/diet_templates.sql` — sqlc source for `diet_templates`/`template_slots`, including `DeleteDietTemplatesForUser`.
- `backend/internal/store/queries/plan_entries.sql` — sqlc source for `plan_entries`, including `DeletePlanEntriesForUser`.
- `backend/internal/service/util.go` — new: the generic `uniqueUUIDs` helper.
- `backend/internal/service/meals.go` — gains `ErrMealInUse`, teaches `Delete` to translate the two new foreign-key violations, and is refactored to use `uniqueUUIDs`.
- `backend/internal/service/meals_test.go` — gains tests for the new guard.
- `backend/internal/service/auth.go` — `DeleteUser`'s transaction gains the `plan_entries`/`diet_templates` pre-deletes.
- `backend/internal/service/auth_test.go` — gains a regression test.
- `backend/internal/service/diet_templates.go` — the `DietTemplates` service: CRUD, list, atomic slots replace, copy, apply.
- `backend/internal/service/diet_templates_test.go` — its tests.
- `backend/internal/service/plan.go` — the `Plan` service: date-range read with totals, set/swap entry, delete entry.
- `backend/internal/service/plan_test.go` — its tests.
- `openapi.yaml` — `/diet-templates`, `/diet-templates/{id}`, `/diet-templates/{id}/slots`, `/diet-templates/{id}/apply`, `/diet-templates/{id}/copy`, `/plan`, `/plan/{date}/{slot}`, their schemas, and `409` added to `DELETE /meals/{id}`.
- `backend/internal/httpapi/diet_templates.go` — the eight diet-template handlers, cursor encode/decode, `DietTemplatesService` interface.
- `backend/internal/httpapi/plan.go` — the three plan handlers, `PlanService` interface.
- `backend/internal/httpapi/account.go` — gains `writeServiceError` cases for every new service error.
- `backend/internal/httpapi/problem.go` — gains the new `Code*` constants.
- `backend/internal/httpapi/server.go`, `router.go` — wire the two new services into `Deps` and `server`.
- `backend/internal/httpapi/diet_templates_flow_test.go`, `plan_flow_test.go` — end-to-end contract tests.
- `backend/internal/httpapi/contract_test.go` — gains `stubDietTemplates`/`stubPlan` and wires them into `newTestRouter`.
- `backend/cmd/api/main.go` — constructs `service.NewDietTemplates`/`service.NewPlan` and wires them into `httpapi.Deps`.
- `backend/CLAUDE.md` — documents the deletion-guard extension, the snack-multiplicity/upsert rule, day_count immutability, and closes the "Not built yet" line.

---

### Task 1: Migration — `diet_templates`, `template_slots`, `plan_entries`

**Files:**
- Create: `backend/migrations/00006_diets_and_plan.sql`
- Modify: `backend/internal/db/schema_test.go`

**Interfaces:**
- Produces: the `diet_templates`, `template_slots` and `plan_entries` tables. Named foreign keys (Postgres's default naming for an unnamed inline `REFERENCES`, matching `00005_meals.sql`'s style): `diet_templates_owner_id_fkey`, `template_slots_template_id_fkey` (`ON DELETE CASCADE`), `template_slots_meal_id_fkey` (no `ON DELETE` — Task 4 relies on this exact name), `plan_entries_owner_id_fkey` (`ON DELETE CASCADE`), `plan_entries_meal_id_fkey` (no `ON DELETE` — Task 4 relies on this exact name), `plan_entries_from_template_id_fkey` (`ON DELETE SET NULL`).

- [ ] **Step 1: Write the failing test**

Add to `backend/internal/db/schema_test.go` (same file and `migratedConn` helper as `TestMealsSchemaEnforcesItsConstraints`):

```go
func TestDietsAndPlanSchemaEnforcesItsConstraints(t *testing.T) {
	ctx := context.Background()
	conn := migratedConn(t)

	var userID string
	if err := conn.QueryRow(ctx,
		`INSERT INTO users (email, password_hash, display_name) VALUES ('a@example.com', 'h', 'A') RETURNING id`,
	).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var mealID string
	if err := conn.QueryRow(ctx,
		`INSERT INTO meals (owner_id, name, servings) VALUES ($1, 'Oatmeal', 1) RETURNING id`, userID,
	).Scan(&mealID); err != nil {
		t.Fatalf("insert meal: %v", err)
	}

	var templateID string
	if err := conn.QueryRow(ctx,
		`INSERT INTO diet_templates (owner_id, name, day_count) VALUES ($1, 'Week One', 7) RETURNING id`, userID,
	).Scan(&templateID); err != nil {
		t.Fatalf("valid insert: %v", err)
	}

	if _, err := conn.Exec(ctx,
		`INSERT INTO diet_templates (owner_id, name, day_count) VALUES (gen_random_uuid(), 'Ghost Template', 7)`,
	); err == nil {
		t.Error("an owner_id that does not reference a user was accepted, want a foreign key violation")
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO diet_templates (owner_id, name, day_count) VALUES ($1, 'Bad Day Count', 0)`, userID,
	); err == nil {
		t.Error("day_count 0 was accepted, want a constraint violation")
	}

	insertSlot := func(sql string, args ...any) error {
		_, err := conn.Exec(ctx, "INSERT INTO template_slots (template_id, day_index, slot, meal_id, portion) VALUES "+sql, args...)
		return err
	}
	if err := insertSlot(`($1, 0, 'breakfast', $2, 1)`, templateID, mealID); err != nil {
		t.Fatalf("valid slot: %v", err)
	}
	if err := insertSlot(`($1, -1, 'lunch', $2, 1)`, templateID, mealID); err == nil {
		t.Error("a negative day_index was accepted, want a constraint violation")
	}
	if err := insertSlot(`($1, 0, 'brunch', $2, 1)`, templateID, mealID); err == nil {
		t.Error("an invalid slot was accepted, want a constraint violation")
	}
	if err := insertSlot(`($1, 0, 'lunch', $2, 0)`, templateID, mealID); err == nil {
		t.Error("zero portion was accepted, want a constraint violation")
	}
	if err := insertSlot(`($1, 0, 'breakfast', $2, 1)`, templateID, mealID); err == nil {
		t.Error("a duplicate (template_id, day_index, breakfast) was accepted, want a unique violation")
	}
	if err := insertSlot(`($1, 0, 'snack', $2, 1)`, templateID, mealID); err != nil {
		t.Fatalf("first snack on day 0: %v", err)
	}
	if err := insertSlot(`($1, 0, 'snack', $2, 1)`, templateID, mealID); err != nil {
		t.Errorf("a second snack on the same day was rejected, want it accepted: %v", err)
	}
	if err := insertSlot(`($1, 0, 'breakfast', gen_random_uuid(), 1)`, templateID); err == nil {
		t.Error("a meal_id that does not reference a meal was accepted, want a foreign key violation")
	}

	if _, err := conn.Exec(ctx, `DELETE FROM meals WHERE id = $1`, mealID); err == nil {
		t.Error("deleting a meal referenced by a template_slots row was accepted, want a foreign key violation")
	}

	var planEntryID string
	if err := conn.QueryRow(ctx,
		`INSERT INTO plan_entries (owner_id, date, slot, meal_id, from_template_id) VALUES ($1, '2026-01-01', 'breakfast', $2, $3) RETURNING id`,
		userID, mealID, templateID,
	).Scan(&planEntryID); err != nil {
		t.Fatalf("valid plan entry: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO plan_entries (owner_id, date, slot, meal_id) VALUES ($1, '2026-01-01', 'breakfast', $2)`, userID, mealID,
	); err == nil {
		t.Error("a duplicate (owner_id, date, breakfast) was accepted, want a unique violation")
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO plan_entries (owner_id, date, slot, meal_id) VALUES ($1, '2026-01-01', 'snack', $2)`, userID, mealID,
	); err != nil {
		t.Errorf("a second snack plan entry on the same day was rejected, want it accepted: %v", err)
	}
	if _, err := conn.Exec(ctx,
		`INSERT INTO plan_entries (owner_id, date, slot, meal_id) VALUES ($1, '2026-01-02', 'breakfast', gen_random_uuid())`, userID,
	); err == nil {
		t.Error("a meal_id that does not reference a meal was accepted, want a foreign key violation")
	}
	if _, err := conn.Exec(ctx, `DELETE FROM meals WHERE id = $1`, mealID); err == nil {
		t.Error("deleting a meal referenced by a plan_entries row was accepted, want a foreign key violation")
	}

	if _, err := conn.Exec(ctx, `DELETE FROM diet_templates WHERE id = $1`, templateID); err != nil {
		t.Fatalf("delete template: %v", err)
	}
	var slotCount int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM template_slots WHERE template_id = $1`, templateID).Scan(&slotCount); err != nil || slotCount != 0 {
		t.Errorf("template_slots rows after deleting the template = %d (err %v), want 0", slotCount, err)
	}
	var fromTemplate *string
	if err := conn.QueryRow(ctx, `SELECT from_template_id FROM plan_entries WHERE id = $1`, planEntryID).Scan(&fromTemplate); err != nil {
		t.Fatalf("read plan entry after template delete: %v", err)
	}
	if fromTemplate != nil {
		t.Errorf("from_template_id after deleting the template = %v, want NULL (ON DELETE SET NULL)", *fromTemplate)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/db/... -run TestDietsAndPlanSchemaEnforcesItsConstraints -v`
Expected: FAIL — `relation "diet_templates" does not exist` (needs Docker; skips locally without it, fails under `CI=1`).

- [ ] **Step 3: Write the migration**

Create `backend/migrations/00006_diets_and_plan.sql`:

```sql
-- +goose Up
CREATE TABLE diet_templates (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id            uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name                text NOT NULL,
    day_count           integer NOT NULL CHECK (day_count >= 1),
    shared_with_partner boolean NOT NULL DEFAULT false,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX diet_templates_owner_id_idx ON diet_templates (owner_id);

CREATE TRIGGER diet_templates_set_updated_at
    BEFORE UPDATE ON diet_templates
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE template_slots (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    template_id uuid NOT NULL REFERENCES diet_templates (id) ON DELETE CASCADE,
    day_index   integer NOT NULL CHECK (day_index >= 0),
    slot        text NOT NULL CHECK (slot IN ('breakfast', 'lunch', 'dinner', 'snack')),
    meal_id     uuid NOT NULL REFERENCES meals (id),
    portion     double precision NOT NULL DEFAULT 1 CHECK (portion > 0)
);

CREATE UNIQUE INDEX template_slots_unique_slot_idx ON template_slots (template_id, day_index, slot) WHERE slot != 'snack';
CREATE INDEX template_slots_template_id_idx ON template_slots (template_id);
CREATE INDEX template_slots_meal_id_idx ON template_slots (meal_id);

CREATE TABLE plan_entries (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id         uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    date             date NOT NULL,
    slot             text NOT NULL CHECK (slot IN ('breakfast', 'lunch', 'dinner', 'snack')),
    meal_id          uuid NOT NULL REFERENCES meals (id),
    portion          double precision NOT NULL DEFAULT 1 CHECK (portion > 0),
    from_template_id uuid REFERENCES diet_templates (id) ON DELETE SET NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX plan_entries_unique_slot_idx ON plan_entries (owner_id, date, slot) WHERE slot != 'snack';
CREATE INDEX plan_entries_owner_id_date_idx ON plan_entries (owner_id, date);
CREATE INDEX plan_entries_meal_id_idx ON plan_entries (meal_id);

CREATE TRIGGER plan_entries_set_updated_at
    BEFORE UPDATE ON plan_entries
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE plan_entries;
DROP TABLE template_slots;
DROP TABLE diet_templates;
```

`meal_id` deliberately has no `ON DELETE` clause on both `template_slots` and `plan_entries` (defaults to `NO ACTION`/`RESTRICT`): deleting a meal that a template or the calendar references must fail, not silently orphan the row — exactly the `meal_ingredients.ingredient_id` precedent from `00005_meals.sql`. `plan_entries.from_template_id` is the one nullable, provenance-only reference in this migration, so it alone gets `ON DELETE SET NULL`: deleting a template must not delete or block deleting the plan entries that came from it, only forget where they came from. Both partial unique indexes (`WHERE slot != 'snack'`) allow multiple `snack` rows per (template_id, day_index) / (owner_id, date) while keeping the other three slots singular — see Global Constraints for why `plan_entries` needed the same treatment as `template_slots` even though the spec only states the rule for the latter.

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd backend && go test ./internal/db/... -run TestDietsAndPlanSchemaEnforcesItsConstraints -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/migrations/00006_diets_and_plan.sql backend/internal/db/schema_test.go
git commit -m "feat(backend): add the diet_templates, template_slots and plan_entries tables"
```

---

### Task 2: sqlc queries for diet templates and slots

**Files:**
- Create: `backend/internal/store/queries/diet_templates.sql`
- Modify: `backend/internal/store/queries/meals.sql` (add `GetMealsForUser`)
- Modify (generated, commit the output): `backend/internal/store/sqlc/diet_templates.sql.go`, `backend/internal/store/sqlc/meals.sql.go`, `backend/internal/store/sqlc/models.go`

**Interfaces:**
- Consumes: the schema from Task 1.
- Produces (used by Tasks 4, 5, 7): `sqlc.DietTemplate{ID, OwnerID uuid.UUID; Name string; DayCount int32; SharedWithPartner bool; CreatedAt, UpdatedAt time.Time}`, `sqlc.TemplateSlot{ID, TemplateID, MealID uuid.UUID; DayIndex int32; Slot string; Portion float64}`, and these `*sqlc.Queries` methods:
  - `CreateDietTemplate(ctx, CreateDietTemplateParams{OwnerID uuid.UUID, Name string, DayCount int32, SharedWithPartner bool}) (DietTemplate, error)`
  - `GetDietTemplateForUser(ctx, GetDietTemplateForUserParams{ID, UserID uuid.UUID}) (DietTemplate, error)`
  - `ListDietTemplatesForUser(ctx, ListDietTemplatesForUserParams{UserID uuid.UUID, HasCursor bool, CursorName string, CursorID uuid.UUID, RowLimit int32}) ([]DietTemplate, error)`
  - `UpdateDietTemplate(ctx, UpdateDietTemplateParams{Name *string, SharedWithPartner *bool, ID, UserID uuid.UUID}) (DietTemplate, error)`
  - `DeleteDietTemplate(ctx, DeleteDietTemplateParams{ID, UserID uuid.UUID}) (int64, error)`
  - `DeleteDietTemplatesForUser(ctx, userID uuid.UUID) error`
  - `DeleteTemplateSlots(ctx, templateID uuid.UUID) error`
  - `InsertTemplateSlot(ctx, InsertTemplateSlotParams{TemplateID, MealID uuid.UUID, DayIndex int32, Slot string, Portion float64}) (TemplateSlot, error)`
  - `GetTemplateSlots(ctx, templateID uuid.UUID) ([]TemplateSlot, error)`
  - `GetMealsForUser(ctx, GetMealsForUserParams{Ids []uuid.UUID, UserID uuid.UUID}) ([]Meal, error)` — note `UserID` is a **plain `uuid.UUID`**, not a pointer, unlike `GetIngredientsForUser`'s: `meals.owner_id` is `NOT NULL`, so sqlc has no nullable column to infer a pointer from.

  All of the above is scratch-verified: a real `sqlc generate` run against these exact query texts (with a copy of the repo's `sqlc.yaml`) produced exactly these signatures with no errors.

- [ ] **Step 1: Write the diet template queries**

Create `backend/internal/store/queries/diet_templates.sql`:

```sql
-- name: CreateDietTemplate :one
INSERT INTO diet_templates (owner_id, name, day_count, shared_with_partner)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetDietTemplateForUser :one
-- Owner-only visibility for now: shared_with_partner has no effect on GET
-- until the partner plan adds the partnerships table and an active-partner
-- lookup. See "Not built yet" in backend/CLAUDE.md.
SELECT * FROM diet_templates
WHERE id = sqlc.arg('id') AND owner_id = sqlc.arg('user_id');

-- name: ListDietTemplatesForUser :many
SELECT * FROM diet_templates
WHERE owner_id = sqlc.arg('user_id')
  AND (
    NOT sqlc.arg('has_cursor')::boolean
    OR name > sqlc.arg('cursor_name')::text
    OR (name = sqlc.arg('cursor_name')::text AND id > sqlc.arg('cursor_id')::uuid)
  )
ORDER BY name, id
LIMIT sqlc.arg('row_limit');

-- name: UpdateDietTemplate :one
-- day_count is immutable after creation (see Global Constraints), so it has
-- no place in this UPDATE.
UPDATE diet_templates SET
    name                = COALESCE(sqlc.narg('name'), name),
    shared_with_partner = COALESCE(sqlc.narg('shared_with_partner'), shared_with_partner)
WHERE id = sqlc.arg('id') AND owner_id = sqlc.arg('user_id')
RETURNING *;

-- name: DeleteDietTemplate :execrows
DELETE FROM diet_templates WHERE id = sqlc.arg('id') AND owner_id = sqlc.arg('user_id');

-- name: DeleteDietTemplatesForUser :exec
-- Used by Auth.DeleteUser (Task 4), ahead of deleting the user row.
DELETE FROM diet_templates WHERE owner_id = sqlc.arg('user_id');

-- name: DeleteTemplateSlots :exec
DELETE FROM template_slots WHERE template_id = $1;

-- name: InsertTemplateSlot :one
INSERT INTO template_slots (template_id, day_index, slot, meal_id, portion)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetTemplateSlots :many
SELECT * FROM template_slots WHERE template_id = sqlc.arg('template_id') ORDER BY day_index, slot;
```

Notes on choices already made (do not redesign these):
- `UpdateDietTemplate` uses plain `COALESCE(narg, column)` for both fields because neither `name` nor `shared_with_partner` is nullable — there is no legitimate "clear to null" state for either, unlike `Meal.notes`.
- `InsertTemplateSlot` is `:one` with `RETURNING *`, not `:exec`, for the same reason `InsertMealIngredient` is: `ReplaceSlots` (Task 5) needs each inserted row's real database-generated `id`.
- No query blocks a `template_slots` row from referencing a meal invisible to the template's owner, or from having a `day_index` past the template's `day_count` — both are service-layer checks (Task 5), the same way `meal_ingredients` visibility is a service-layer check in the meals plan.

- [ ] **Step 2: Add `GetMealsForUser` to the meals queries**

In `backend/internal/store/queries/meals.sql`, after `GetMealIngredients`, add:

```sql
-- name: GetMealsForUser :many
-- The batch counterpart to GetMealForUser: given a set of meal ids, returns
-- only the ones that exist and are owned by user_id. Used by DietTemplates
-- and Plan (Tasks 5, 6) to validate a slot's or entry's meal_id, and by
-- DietTemplates.toTemplate to fetch each slot's meal name in one query.
SELECT * FROM meals
WHERE id = ANY(sqlc.arg('ids')::uuid[]) AND owner_id = sqlc.arg('user_id');
```

- [ ] **Step 3: Regenerate and verify it compiles**

Run: `cd backend && go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate`
Expected: exits 0, creates `internal/store/sqlc/diet_templates.sql.go`, updates `meals.sql.go` and adds `DietTemplate`/`TemplateSlot` to `models.go`.

Run: `cd backend && go build ./...`
Expected: builds cleanly (nothing references the new queries yet, so this only proves the generated code itself compiles).

- [ ] **Step 4: Commit**

```bash
git add backend/internal/store/queries/diet_templates.sql backend/internal/store/queries/meals.sql backend/internal/store/sqlc/
git commit -m "feat(backend): add sqlc queries for diet templates and slots"
```

---

### Task 3: sqlc queries for plan entries

**Files:**
- Create: `backend/internal/store/queries/plan_entries.sql`
- Modify (generated, commit the output): `backend/internal/store/sqlc/plan_entries.sql.go`, `backend/internal/store/sqlc/models.go`

**Interfaces:**
- Consumes: the schema from Task 1.
- Produces (used by Tasks 4, 5, 6): `sqlc.PlanEntry{ID, OwnerID, MealID uuid.UUID; Date time.Time; Slot string; Portion float64; FromTemplateID *uuid.UUID; CreatedAt, UpdatedAt time.Time}`, and these `*sqlc.Queries` methods:
  - `InsertPlanEntry(ctx, InsertPlanEntryParams{OwnerID, MealID uuid.UUID; Date time.Time; Slot string; Portion float64; FromTemplateID *uuid.UUID}) (PlanEntry, error)`
  - `UpsertPlanEntry(ctx, UpsertPlanEntryParams{OwnerID, MealID uuid.UUID; Date time.Time; Slot string; Portion float64}) (PlanEntry, error)` — no `FromTemplateID` param: it always writes `NULL` (see Global Constraints).
  - `GetPlanEntriesForUserInRange(ctx, GetPlanEntriesForUserInRangeParams{UserID uuid.UUID; FromDate, ToDate time.Time}) ([]PlanEntry, error)`
  - `GetPlanEntriesForUserOnDates(ctx, GetPlanEntriesForUserOnDatesParams{UserID uuid.UUID; Dates []time.Time}) ([]PlanEntry, error)`
  - `DeletePlanEntryForUserOnDateSlot(ctx, DeletePlanEntryForUserOnDateSlotParams{UserID uuid.UUID; Date time.Time; Slot string}) (int64, error)`
  - `DeleteAllSnackEntriesForUserOnDate(ctx, DeleteAllSnackEntriesForUserOnDateParams{UserID uuid.UUID; Date time.Time}) (int64, error)`
  - `DeletePlanEntriesForUser(ctx, userID uuid.UUID) error`

  All of the above is scratch-verified: a real `sqlc generate` run against these exact query texts produced exactly these signatures with no errors, including `UpsertPlanEntry`'s `ON CONFLICT ... WHERE ...` clause against the Task 1 partial unique index — sqlc accepts a raw SQL upsert as-is and infers params/return the same way it does any other query.

- [ ] **Step 1: Write the plan entry queries**

Create `backend/internal/store/queries/plan_entries.sql`:

```sql
-- name: InsertPlanEntry :one
-- Always adds a new row. Used for snack (which never upserts) and by
-- DietTemplates.Apply (Task 5), which sets from_template_id explicitly.
INSERT INTO plan_entries (owner_id, date, slot, meal_id, portion, from_template_id)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpsertPlanEntry :one
-- Only for slot != 'snack': the partial unique index (owner_id, date, slot)
-- WHERE slot != 'snack' from 00006_diets_and_plan.sql is the ON CONFLICT
-- target, so this natively updates the single existing entry for that
-- date+slot, or inserts a new one. Always writes from_template_id = NULL: a
-- manual upsert is no longer "from" a template, even when it overwrites an
-- entry that was (see Global Constraints). Never call this for slot =
-- 'snack' — the service layer routes to InsertPlanEntry instead.
INSERT INTO plan_entries (owner_id, date, slot, meal_id, portion, from_template_id)
VALUES ($1, $2, $3, $4, $5, NULL)
ON CONFLICT (owner_id, date, slot) WHERE slot != 'snack'
DO UPDATE SET meal_id = EXCLUDED.meal_id, portion = EXCLUDED.portion, from_template_id = NULL
RETURNING *;

-- name: GetPlanEntriesForUserInRange :many
SELECT * FROM plan_entries
WHERE owner_id = sqlc.arg('user_id')
  AND date >= sqlc.arg('from_date')::date AND date <= sqlc.arg('to_date')::date
ORDER BY date, slot;

-- name: GetPlanEntriesForUserOnDates :many
-- Used by DietTemplates.Apply's pre-write conflict check: given the exact
-- target dates a template's slots would land on, return every existing
-- entry on any of those dates in one query.
SELECT * FROM plan_entries
WHERE owner_id = sqlc.arg('user_id') AND date = ANY(sqlc.arg('dates')::date[]);

-- name: DeletePlanEntryForUserOnDateSlot :execrows
-- The "AND slot != 'snack'" is a defensive belt: this query must never
-- delete a snack row even if called with slot = 'snack' by mistake — the
-- service layer routes snack deletes to DeleteAllSnackEntriesForUserOnDate
-- instead, but a wrong call here should do nothing rather than something
-- wrong. Also used by DietTemplates.Apply's overwrite path to clear the one
-- existing non-snack entry at a target date+slot before re-inserting.
DELETE FROM plan_entries
WHERE owner_id = sqlc.arg('user_id') AND date = sqlc.arg('date') AND slot = sqlc.arg('slot') AND slot != 'snack';

-- name: DeleteAllSnackEntriesForUserOnDate :execrows
DELETE FROM plan_entries
WHERE owner_id = sqlc.arg('user_id') AND date = sqlc.arg('date') AND slot = 'snack';

-- name: DeletePlanEntriesForUser :exec
-- Used by Auth.DeleteUser (Task 4), ahead of deleting diet_templates and
-- meals and the user row.
DELETE FROM plan_entries WHERE owner_id = sqlc.arg('user_id');
```

- [ ] **Step 2: Regenerate and verify it compiles**

Run: `cd backend && go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate`
Expected: exits 0, creates `internal/store/sqlc/plan_entries.sql.go` and adds `PlanEntry` to `models.go`.

Run: `cd backend && go build ./...`
Expected: builds cleanly.

- [ ] **Step 3: Commit**

```bash
git add backend/internal/store/queries/plan_entries.sql backend/internal/store/sqlc/
git commit -m "feat(backend): add sqlc queries for plan entries"
```

---

### Task 4: Meal-in-use — close the deletion guard, extend the account-deletion ordering

Two closely related gaps, both about `meal_id`'s new `NO ACTION` foreign keys, closed together because the second is only discoverable once the first is understood: (1) `Meals.Delete` doesn't yet know that `template_slots`/`plan_entries` can also block a delete (only `meal_ingredients` couldn't exist yet when it was written), and (2) `Auth.DeleteUser`'s transaction, which the meals plan's final review taught to delete a user's meals before the user row, now needs to delete `plan_entries` and `diet_templates` before that, for the reason Global Constraints explains.

**Files:**
- Modify: `backend/internal/service/meals.go`
- Modify: `backend/internal/service/meals_test.go`
- Modify: `backend/internal/service/auth.go`
- Modify: `backend/internal/service/auth_test.go`
- Modify: `backend/internal/httpapi/problem.go`
- Modify: `backend/internal/httpapi/account.go`
- Modify: `openapi.yaml` (add `409` to `DELETE /meals/{id}`)
- Modify (generated, commit the output): `backend/internal/api/api.gen.go`

**Interfaces:**
- Consumes: `store.IsForeignKeyViolation` (existing); `DeletePlanEntriesForUser`/`DeleteDietTemplatesForUser` (Tasks 2, 3).
- Produces: `service.ErrMealInUse` (consumed by `httpapi.writeServiceError`), `httpapi.CodeMealInUse = "meal_in_use"`.

- [ ] **Step 1: Write the failing tests**

In `backend/internal/service/meals_test.go`, add (needs `st.CreateDietTemplate`/`st.InsertTemplateSlot` and `st.InsertPlanEntry` from Tasks 2 to 3, promoted directly onto `*store.Store`):

```go
func TestMealsDeleteIsBlockedWhileInUseByATemplateSlot(t *testing.T) {
	svc, st := newIngredientsFixture(t)
	_ = svc
	meals, _ := newMealsFixture(t)
	owner := newTestUser(t, st, "chef10@example.com")

	meal, err := meals.Create(context.Background(), owner, service.CreateMealInput{Name: "Oatmeal", Servings: 1})
	if err != nil {
		t.Fatalf("Create meal: %v", err)
	}
	tpl, err := st.CreateDietTemplate(context.Background(), sqlc.CreateDietTemplateParams{OwnerID: owner, Name: "Week", DayCount: 7})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	if _, err := st.InsertTemplateSlot(context.Background(), sqlc.InsertTemplateSlotParams{
		TemplateID: tpl.ID, DayIndex: 0, Slot: "breakfast", MealID: meal.ID, Portion: 1,
	}); err != nil {
		t.Fatalf("insert template slot: %v", err)
	}

	if err := meals.Delete(context.Background(), owner, meal.ID); !errors.Is(err, service.ErrMealInUse) {
		t.Errorf("Delete while referenced by a template slot: err = %v, want ErrMealInUse", err)
	}

	if err := st.DeleteTemplateSlots(context.Background(), tpl.ID); err != nil {
		t.Fatalf("clear template slots: %v", err)
	}
	if err := meals.Delete(context.Background(), owner, meal.ID); err != nil {
		t.Errorf("Delete once no longer referenced: %v", err)
	}
}

func TestMealsDeleteIsBlockedWhileInUseByAPlanEntry(t *testing.T) {
	svc, st := newIngredientsFixture(t)
	_ = svc
	meals, _ := newMealsFixture(t)
	owner := newTestUser(t, st, "chef11@example.com")

	meal, err := meals.Create(context.Background(), owner, service.CreateMealInput{Name: "Salad", Servings: 1})
	if err != nil {
		t.Fatalf("Create meal: %v", err)
	}
	entry, err := st.InsertPlanEntry(context.Background(), sqlc.InsertPlanEntryParams{
		OwnerID: owner, Date: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Slot: "lunch", MealID: meal.ID, Portion: 1,
	})
	if err != nil {
		t.Fatalf("insert plan entry: %v", err)
	}

	if err := meals.Delete(context.Background(), owner, meal.ID); !errors.Is(err, service.ErrMealInUse) {
		t.Errorf("Delete while referenced by a plan entry: err = %v, want ErrMealInUse", err)
	}

	if _, err := st.DeletePlanEntryForUserOnDateSlot(context.Background(), sqlc.DeletePlanEntryForUserOnDateSlotParams{
		UserID: owner, Date: entry.Date, Slot: entry.Slot,
	}); err != nil {
		t.Fatalf("clear plan entry: %v", err)
	}
	if err := meals.Delete(context.Background(), owner, meal.ID); err != nil {
		t.Errorf("Delete once no longer referenced: %v", err)
	}
}
```

Add `"time"` to the file's imports if not already present.

In `backend/internal/service/auth_test.go`, add (mirrors the regression test the meals plan's final review added for the meals-only case, extended to a plan entry and a template too):

```go
func TestDeleteUserWithAPlanEntryAndTemplateUsingTheirOwnMeal(t *testing.T) {
	auth, st := newAuthFixture(t)
	meals := service.NewMeals(st)
	owner := newTestUser(t, st, "deleteme@example.com")

	meal, err := meals.Create(context.Background(), owner, service.CreateMealInput{Name: "Toast", Servings: 1})
	if err != nil {
		t.Fatalf("create meal: %v", err)
	}
	tpl, err := st.CreateDietTemplate(context.Background(), sqlc.CreateDietTemplateParams{OwnerID: owner, Name: "Week", DayCount: 1})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	if _, err := st.InsertTemplateSlot(context.Background(), sqlc.InsertTemplateSlotParams{
		TemplateID: tpl.ID, DayIndex: 0, Slot: "breakfast", MealID: meal.ID, Portion: 1,
	}); err != nil {
		t.Fatalf("insert template slot: %v", err)
	}
	if _, err := st.InsertPlanEntry(context.Background(), sqlc.InsertPlanEntryParams{
		OwnerID: owner, Date: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Slot: "breakfast", MealID: meal.ID, Portion: 1,
	}); err != nil {
		t.Fatalf("insert plan entry: %v", err)
	}

	if err := auth.DeleteUser(context.Background(), owner); err != nil {
		t.Errorf("DeleteUser with a template and a plan entry using the caller's own meal: %v", err)
	}
	if _, err := auth.GetUser(context.Background(), owner); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("GetUser after DeleteUser: err = %v, want ErrNotFound", err)
	}
}
```

Check `backend/internal/service/auth_test.go` for its existing fixture helper name (likely `newAuthFixture(t) (*service.Auth, *store.Store)`, mirroring `newIngredientsFixture`) and use it verbatim; add `"time"` and `"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"` to imports if not already present.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/service/... -run 'TestMealsDeleteIsBlockedWhileInUseBy|TestDeleteUserWithAPlanEntryAndTemplate' -v`
Expected: FAIL to compile — `service.ErrMealInUse` does not exist yet (the `sqlc.*` calls already exist from Tasks 2 to 3).

- [ ] **Step 3: Add `ErrMealInUse` and teach `Delete` about it**

In `backend/internal/service/meals.go`, add to the `var` block alongside the other `Meals` errors:

```go
	// ErrMealInUse means the meal cannot be deleted because a diet
	// template's slot or a plan entry still references it.
	ErrMealInUse = errors.New("meal is in use")
```

Change `Delete`:

```go
// Delete removes a meal owned by ownerID. Its meal_ingredients rows are
// removed by ON DELETE CASCADE. Fails with ErrMealInUse if a diet template's
// slot or a plan entry still references the meal.
func (s *Meals) Delete(ctx context.Context, ownerID, id uuid.UUID) error {
	n, err := s.st.DeleteMeal(ctx, sqlc.DeleteMealParams{ID: id, UserID: ownerID})
	if store.IsForeignKeyViolation(err, "template_slots_meal_id_fkey") || store.IsForeignKeyViolation(err, "plan_entries_meal_id_fkey") {
		return ErrMealInUse
	}
	if err != nil {
		return fmt.Errorf("delete meal: %w", err)
	}
	if n == 0 {
		return ErrMealNotFound
	}
	return nil
}
```

- [ ] **Step 4: Extend `Auth.DeleteUser`'s ordering**

In `backend/internal/service/auth.go`, change `DeleteUser`:

```go
func (a *Auth) DeleteUser(ctx context.Context, id uuid.UUID) error {
	return a.st.InTx(ctx, func(q *sqlc.Queries) error {
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

The order matters: `plan_entries` goes first (its `from_template_id` would otherwise just be set to `NULL` by the existing `ON DELETE SET NULL` when the template is deleted next, which is harmless either way, but deleting entries first keeps the intent explicit); `diet_templates` goes before `meals` (a template's slots reference meals with `NO ACTION`, and deleting the template cascades its slots away first); `meals` goes before `users` (already true from the meals plan's fix — `meals` cascades from `users`, but `ingredients` also cascades from `users` and would hit the still-referenced-by-`meal_ingredients` check if meals weren't already gone).

- [ ] **Step 5: Add the problem code and the `writeServiceError` case**

In `backend/internal/httpapi/problem.go`, add to the `Code*` constants:

```go
	CodeMealInUse = "meal_in_use"
```

In `backend/internal/httpapi/account.go`, in `writeServiceError`, add a case next to `ErrIngredientInUse`:

```go
	case errors.Is(err, service.ErrMealInUse):
		WriteProblem(w, http.StatusConflict, CodeMealInUse, "")
```

- [ ] **Step 6: Add `409` to the contract**

In `openapi.yaml`, in `DELETE /meals/{id}`'s `responses`, add before `429`:

```yaml
        '409':
          $ref: '#/components/responses/Conflict'
```

Run: `make lint-api` — expect the same pre-existing warnings only, no new ones.
Run: `make generate` — regenerates `backend/internal/api/api.gen.go` (response-only change, no new Go type).

- [ ] **Step 7: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/service/... -run 'TestMeals|TestDeleteUser' -v`
Expected: PASS, including all four new tests.

Run: `cd backend && go build ./...`
Expected: builds cleanly.

- [ ] **Step 8: Commit**

```bash
git add backend/internal/service/meals.go backend/internal/service/meals_test.go \
  backend/internal/service/auth.go backend/internal/service/auth_test.go \
  backend/internal/httpapi/problem.go backend/internal/httpapi/account.go \
  openapi.yaml backend/internal/api/api.gen.go
git commit -m "feat(backend): block deleting a meal a template or plan entry still references"
```

---

### Task 5: `uniqueUUIDs` helper and the `DietTemplates` service

**Files:**
- Create: `backend/internal/service/util.go`
- Modify: `backend/internal/service/meals.go` (refactor `uniqueIngredientIDs` to use `uniqueUUIDs`)
- Create: `backend/internal/service/diet_templates.go`
- Create: `backend/internal/service/diet_templates_test.go`

**Interfaces:**
- Consumes: the `sqlc.Queries` methods from Tasks 2 to 3; `store.IsNotFound`, `store.IsForeignKeyViolation`, `store.IsUniqueViolation`, `store.InTx` (all existing).
- Produces (consumed by Task 7's handlers and Task 6's `Plan` service):
  - `func uniqueUUIDs[T any](items []T, get func(T) uuid.UUID) []uuid.UUID` (used by `meals.go`, `diet_templates.go`, and Task 6's `plan.go`).
  - Errors `service.ErrDietTemplateNotFound`, `service.ErrDayIndexOutOfRange`, `service.ErrTemplateMealNotFound`, `service.ErrDuplicateSlot`, `service.ErrPlanConflict` (all `errors.New(...)`).
  - `type TemplateSlot struct { ID uuid.UUID; DayIndex int; Slot string; MealID uuid.UUID; MealName string; Portion float64 }`
  - `type DietTemplate struct { ID uuid.UUID; Name string; DayCount int; SharedWithPartner bool; Slots []TemplateSlot; CreatedAt, UpdatedAt time.Time }`
  - `type CreateDietTemplateInput struct { Name string; DayCount int; SharedWithPartner bool }`
  - `type UpdateDietTemplateInput struct { Name *string; SharedWithPartner *bool }`
  - `type TemplateSlotInput struct { DayIndex int; Slot string; MealID uuid.UUID; Portion float64 }`
  - `type DietTemplateCursor struct { Name string; ID uuid.UUID }`, `type ListDietTemplatesInput struct { Cursor *DietTemplateCursor; Limit int }`, `type DietTemplateSummary struct { ID uuid.UUID; Name string; DayCount int; SharedWithPartner bool; CreatedAt, UpdatedAt time.Time }`, `type DietTemplatePage struct { Items []DietTemplateSummary; NextCursor *DietTemplateCursor }`
  - `type ApplyTemplateInput struct { StartDate time.Time; Overwrite bool }`
  - `func NewDietTemplates(st *store.Store) *DietTemplates`
  - `func (*DietTemplates) Create(ctx, ownerID uuid.UUID, in CreateDietTemplateInput) (DietTemplate, error)`
  - `func (*DietTemplates) Get(ctx, ownerID, id uuid.UUID) (DietTemplate, error)`
  - `func (*DietTemplates) Update(ctx, ownerID, id uuid.UUID, in UpdateDietTemplateInput) (DietTemplate, error)`
  - `func (*DietTemplates) Delete(ctx, ownerID, id uuid.UUID) error`
  - `func (*DietTemplates) List(ctx, ownerID uuid.UUID, in ListDietTemplatesInput) (DietTemplatePage, error)`
  - `func (*DietTemplates) ReplaceSlots(ctx, ownerID, id uuid.UUID, slots []TemplateSlotInput) (DietTemplate, error)`
  - `func (*DietTemplates) Copy(ctx, callerID, id uuid.UUID) (DietTemplate, error)`
  - `func (*DietTemplates) Apply(ctx, ownerID, id uuid.UUID, in ApplyTemplateInput) (int, error)` — returns the number of plan entries written.

- [ ] **Step 1: Add the `uniqueUUIDs` helper and refactor `meals.go`**

Create `backend/internal/service/util.go`:

```go
package service

import "github.com/google/uuid"

// uniqueUUIDs returns the distinct ids get extracts from items, in
// first-seen order. Shared by every domain that needs to dedupe a batch of
// referenced ids before a single batch lookup: meals' ingredient lines, a
// diet template's slot meal references, and a plan range's entry meal
// references.
func uniqueUUIDs[T any](items []T, get func(T) uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]bool, len(items))
	ids := make([]uuid.UUID, 0, len(items))
	for _, it := range items {
		id := get(it)
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}
```

In `backend/internal/service/meals.go`, delete the `uniqueIngredientIDs` function entirely, and change its one call site in `toMeal`:

```go
		ids := uniqueUUIDs(miRows, func(r sqlc.MealIngredient) uuid.UUID { return r.IngredientID })
```

- [ ] **Step 2: Run the meals tests to verify the refactor didn't break anything**

Run: `cd backend && go test ./internal/service/... -run TestMeals -v`
Expected: PASS, unchanged (this step is a pure refactor with no behavior change).

- [ ] **Step 3: Write the failing tests**

Create `backend/internal/service/diet_templates_test.go`:

```go
package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/service"
	"github.com/InzKazik/mealplanner/backend/internal/store"
	"github.com/InzKazik/mealplanner/backend/internal/store/sqlc"
)

func newDietTemplatesFixture(t *testing.T) (*service.DietTemplates, *service.Meals, *service.Ingredients, *store.Store) {
	t.Helper()
	_, st := newIngredientsFixture(t)
	return service.NewDietTemplates(st), service.NewMeals(st), service.NewIngredients(st), st
}

func mustCreateMeal(t *testing.T, meals *service.Meals, owner uuid.UUID, name string) service.Meal {
	t.Helper()
	m, err := meals.Create(context.Background(), owner, service.CreateMealInput{Name: name, Servings: 1})
	if err != nil {
		t.Fatalf("create meal %q: %v", name, err)
	}
	return m
}

func TestDietTemplatesReplaceSlotsRejectsDayIndexPastDayCount(t *testing.T) {
	tpls, meals, _, st := newDietTemplatesFixture(t)
	owner := newTestUser(t, st, "planner1@example.com")
	meal := mustCreateMeal(t, meals, owner, "Toast")

	tpl, err := tpls.Create(context.Background(), owner, service.CreateDietTemplateInput{Name: "Two Days", DayCount: 2})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = tpls.ReplaceSlots(context.Background(), owner, tpl.ID, []service.TemplateSlotInput{
		{DayIndex: 2, Slot: "breakfast", MealID: meal.ID, Portion: 1},
	})
	if !errors.Is(err, service.ErrDayIndexOutOfRange) {
		t.Errorf("err = %v, want ErrDayIndexOutOfRange (day_count is 2, valid indexes are 0 and 1)", err)
	}
}

func TestDietTemplatesReplaceSlotsRejectsAMealNotOwnedByTheCaller(t *testing.T) {
	tpls, meals, _, st := newDietTemplatesFixture(t)
	owner := newTestUser(t, st, "planner2@example.com")
	other := newTestUser(t, st, "other2@example.com")
	othersMeal := mustCreateMeal(t, meals, other, "Not Mine")

	tpl, err := tpls.Create(context.Background(), owner, service.CreateDietTemplateInput{Name: "Week", DayCount: 7})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, err = tpls.ReplaceSlots(context.Background(), owner, tpl.ID, []service.TemplateSlotInput{
		{DayIndex: 0, Slot: "breakfast", MealID: othersMeal.ID, Portion: 1},
	})
	if !errors.Is(err, service.ErrTemplateMealNotFound) {
		t.Errorf("err = %v, want ErrTemplateMealNotFound", err)
	}
}

func TestDietTemplatesReplaceSlotsRejectsADuplicateNonSnackSlotButAllowsTwoSnacks(t *testing.T) {
	tpls, meals, _, st := newDietTemplatesFixture(t)
	owner := newTestUser(t, st, "planner3@example.com")
	meal := mustCreateMeal(t, meals, owner, "Meal")

	tpl, err := tpls.Create(context.Background(), owner, service.CreateDietTemplateInput{Name: "Week", DayCount: 7})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, err = tpls.ReplaceSlots(context.Background(), owner, tpl.ID, []service.TemplateSlotInput{
		{DayIndex: 0, Slot: "breakfast", MealID: meal.ID, Portion: 1},
		{DayIndex: 0, Slot: "breakfast", MealID: meal.ID, Portion: 1},
	})
	if !errors.Is(err, service.ErrDuplicateSlot) {
		t.Errorf("err = %v, want ErrDuplicateSlot", err)
	}

	got, err := tpls.ReplaceSlots(context.Background(), owner, tpl.ID, []service.TemplateSlotInput{
		{DayIndex: 0, Slot: "snack", MealID: meal.ID, Portion: 1},
		{DayIndex: 0, Slot: "snack", MealID: meal.ID, Portion: 1},
	})
	if err != nil {
		t.Fatalf("two snacks on the same day: %v", err)
	}
	if len(got.Slots) != 2 {
		t.Errorf("Slots = %+v, want 2 snack rows", got.Slots)
	}
}

```go
func TestDietTemplatesCopyDuplicatesSlotsAndStartsPrivate(t *testing.T) {
	tpls, meals, _, st := newDietTemplatesFixture(t)
	owner := newTestUser(t, st, "planner4@example.com")
	meal := mustCreateMeal(t, meals, owner, "Meal")

	original, err := tpls.Create(context.Background(), owner, service.CreateDietTemplateInput{Name: "Week", DayCount: 7, SharedWithPartner: true})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	original, err = tpls.ReplaceSlots(context.Background(), owner, original.ID, []service.TemplateSlotInput{
		{DayIndex: 0, Slot: "breakfast", MealID: meal.ID, Portion: 1},
	})
	if err != nil {
		t.Fatalf("ReplaceSlots: %v", err)
	}

	copy_, err := tpls.Copy(context.Background(), owner, original.ID)
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if copy_.ID == original.ID {
		t.Fatal("copy has the same ID as the original")
	}
	if copy_.SharedWithPartner {
		t.Error("copy has shared_with_partner = true, want false regardless of the original")
	}
	if len(copy_.Slots) != 1 || copy_.Slots[0].MealID != meal.ID {
		t.Errorf("copy slots = %+v, want one breakfast slot", copy_.Slots)
	}
}

func TestDietTemplatesApplyWritesPlanEntriesAtTheRightDates(t *testing.T) {
	tpls, meals, _, st := newDietTemplatesFixture(t)
	owner := newTestUser(t, st, "planner5@example.com")
	meal := mustCreateMeal(t, meals, owner, "Meal")

	tpl, err := tpls.Create(context.Background(), owner, service.CreateDietTemplateInput{Name: "Two Days", DayCount: 2})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := tpls.ReplaceSlots(context.Background(), owner, tpl.ID, []service.TemplateSlotInput{
		{DayIndex: 0, Slot: "breakfast", MealID: meal.ID, Portion: 1},
		{DayIndex: 1, Slot: "breakfast", MealID: meal.ID, Portion: 1},
	}); err != nil {
		t.Fatalf("ReplaceSlots: %v", err)
	}

	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	n, err := tpls.Apply(context.Background(), owner, tpl.ID, service.ApplyTemplateInput{StartDate: start})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if n != 2 {
		t.Errorf("Apply wrote %d entries, want 2", n)
	}

	rows, err := st.GetPlanEntriesForUserInRange(context.Background(), sqlc.GetPlanEntriesForUserInRangeParams{
		UserID: owner, FromDate: start, ToDate: start.AddDate(0, 0, 1),
	})
	if err != nil {
		t.Fatalf("GetPlanEntriesForUserInRange: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("plan entries after apply = %d, want 2", len(rows))
	}
	if !rows[0].Date.Equal(start) || !rows[1].Date.Equal(start.AddDate(0, 0, 1)) {
		t.Errorf("plan entry dates = %v, %v, want %v, %v", rows[0].Date, rows[1].Date, start, start.AddDate(0, 0, 1))
	}
	for _, r := range rows {
		if r.FromTemplateID == nil || *r.FromTemplateID != tpl.ID {
			t.Errorf("plan entry from_template_id = %v, want %v", r.FromTemplateID, tpl.ID)
		}
	}
}

```go
func TestDietTemplatesApplyWithoutOverwriteConflictsOnAnExistingNonSnackEntry(t *testing.T) {
	tpls, meals, _, st := newDietTemplatesFixture(t)
	owner := newTestUser(t, st, "planner6@example.com")
	meal := mustCreateMeal(t, meals, owner, "Meal")

	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	if _, err := st.InsertPlanEntry(context.Background(), sqlc.InsertPlanEntryParams{
		OwnerID: owner, Date: start, Slot: "breakfast", MealID: meal.ID, Portion: 1,
	}); err != nil {
		t.Fatalf("seed existing entry: %v", err)
	}

	tpl, err := tpls.Create(context.Background(), owner, service.CreateDietTemplateInput{Name: "One Day", DayCount: 1})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := tpls.ReplaceSlots(context.Background(), owner, tpl.ID, []service.TemplateSlotInput{
		{DayIndex: 0, Slot: "breakfast", MealID: meal.ID, Portion: 1},
	}); err != nil {
		t.Fatalf("ReplaceSlots: %v", err)
	}

	if _, err := tpls.Apply(context.Background(), owner, tpl.ID, service.ApplyTemplateInput{StartDate: start}); !errors.Is(err, service.ErrPlanConflict) {
		t.Errorf("Apply without overwrite over an existing entry: err = %v, want ErrPlanConflict", err)
	}

	n, err := tpls.Apply(context.Background(), owner, tpl.ID, service.ApplyTemplateInput{StartDate: start, Overwrite: true})
	if err != nil {
		t.Fatalf("Apply with overwrite: %v", err)
	}
	if n != 1 {
		t.Errorf("Apply with overwrite wrote %d entries, want 1", n)
	}
	rows, err := st.GetPlanEntriesForUserInRange(context.Background(), sqlc.GetPlanEntriesForUserInRangeParams{UserID: owner, FromDate: start, ToDate: start})
	if err != nil {
		t.Fatalf("GetPlanEntriesForUserInRange: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("plan entries after overwrite apply = %d, want 1 (the old entry replaced, not duplicated)", len(rows))
	}
}
```

`newDietTemplatesFixture`'s signature intentionally returns four values (`*service.DietTemplates`, `*service.Meals`, `*service.Ingredients`, `*store.Store`) so every test in this file needs exactly one fixture call.

- [ ] **Step 4: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/service/... -run TestDietTemplates -v`
Expected: FAIL to compile — `service.DietTemplates` etc. do not exist yet.

- [ ] **Step 5: Implement the service**

Create `backend/internal/service/diet_templates.go`:

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

// Errors returned by DietTemplates. Handlers map them to problem responses.
var (
	ErrDietTemplateNotFound = errors.New("diet template not found")
	// ErrDayIndexOutOfRange means a slot's day_index is >= the template's
	// day_count. There is no database constraint for this (a CHECK cannot
	// compare against another table's column), so it is enforced here.
	ErrDayIndexOutOfRange = errors.New("day_index is out of range for this template's day_count")
	// ErrTemplateMealNotFound means a slot references a meal that does not
	// exist or is not owned by the caller.
	ErrTemplateMealNotFound = errors.New("one or more meals do not exist or are not visible to you")
	// ErrDuplicateSlot means a non-snack slot was given twice for the same
	// day, translated from the template_slots_unique_slot_idx violation.
	ErrDuplicateSlot = errors.New("a non-snack slot already exists for this day")
	// ErrPlanConflict means applying a template without overwrite would
	// replace an existing, non-snack plan entry.
	ErrPlanConflict = errors.New("applying this template would overwrite existing plan entries")
)

// TemplateSlot is one slot of a diet template, with the referenced meal's
// name inlined so clients don't need a second round trip to render it.
type TemplateSlot struct {
	ID       uuid.UUID
	DayIndex int
	Slot     string
	MealID   uuid.UUID
	MealName string
	Portion  float64
}

// DietTemplate is a reusable meal schedule.
type DietTemplate struct {
	ID                uuid.UUID
	Name              string
	DayCount          int
	SharedWithPartner bool
	Slots             []TemplateSlot
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// CreateDietTemplateInput is the data needed to create a template. It always
// starts with no slots; add them with ReplaceSlots.
type CreateDietTemplateInput struct {
	Name              string
	DayCount          int
	SharedWithPartner bool
}

// UpdateDietTemplateInput is a partial update. day_count is immutable after
// creation (see the plan's Global Constraints), so it has no field here.
type UpdateDietTemplateInput struct {
	Name              *string
	SharedWithPartner *bool
}

// TemplateSlotInput is one slot of a ReplaceSlots call.
type TemplateSlotInput struct {
	DayIndex int
	Slot     string // "breakfast" | "lunch" | "dinner" | "snack"
	MealID   uuid.UUID
	Portion  float64
}

// DietTemplateCursor is an opaque position in the alphabetical template list.
type DietTemplateCursor struct {
	Name string
	ID   uuid.UUID
}

// ListDietTemplatesInput selects a page of the caller's alphabetical
// template list.
type ListDietTemplatesInput struct {
	Cursor *DietTemplateCursor
	Limit  int
}

// DietTemplateSummary is a template without its slots, for the list endpoint.
type DietTemplateSummary struct {
	ID                uuid.UUID
	Name              string
	DayCount          int
	SharedWithPartner bool
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// DietTemplatePage is one page of template summaries plus the cursor for the
// next one (nil on the last page).
type DietTemplatePage struct {
	Items      []DietTemplateSummary
	NextCursor *DietTemplateCursor
}

// ApplyTemplateInput selects where and how a template's slots are copied
// into plan_entries.
type ApplyTemplateInput struct {
	StartDate time.Time
	Overwrite bool
}

// DietTemplates implements reusable meal-schedule templates, owned by a
// single user. Partner sharing is not implemented: shared_with_partner is
// stored, but every read here checks owner_id only. See the "Not built yet"
// note in backend/CLAUDE.md.
type DietTemplates struct {
	st *store.Store
}

// NewDietTemplates returns a DietTemplates service.
func NewDietTemplates(st *store.Store) *DietTemplates { return &DietTemplates{st: st} }

// Create adds a template owned by ownerID, with no slots. Add slots with
// ReplaceSlots.
func (s *DietTemplates) Create(ctx context.Context, ownerID uuid.UUID, in CreateDietTemplateInput) (DietTemplate, error) {
	var tpl DietTemplate
	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
		row, err := q.CreateDietTemplate(ctx, sqlc.CreateDietTemplateParams{
			OwnerID: ownerID, Name: in.Name, DayCount: int32(in.DayCount), SharedWithPartner: in.SharedWithPartner,
		})
		if store.IsForeignKeyViolation(err, "diet_templates_owner_id_fkey") {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("create diet template: %w", err)
		}
		tpl, err = s.toTemplate(ctx, q, row, nil)
		return err
	})
	if err != nil {
		return DietTemplate{}, err
	}
	return tpl, nil
}

// Get returns a template owned by ownerID, with its slots.
func (s *DietTemplates) Get(ctx context.Context, ownerID, id uuid.UUID) (DietTemplate, error) {
	row, err := s.st.GetDietTemplateForUser(ctx, sqlc.GetDietTemplateForUserParams{ID: id, UserID: ownerID})
	if store.IsNotFound(err) {
		return DietTemplate{}, ErrDietTemplateNotFound
	}
	if err != nil {
		return DietTemplate{}, fmt.Errorf("get diet template: %w", err)
	}
	slotRows, err := s.st.GetTemplateSlots(ctx, id)
	if err != nil {
		return DietTemplate{}, fmt.Errorf("get template slots: %w", err)
	}
	return s.toTemplate(ctx, s.st.Queries, row, slotRows)
}

// Update applies a partial update to a template owned by ownerID.
func (s *DietTemplates) Update(ctx context.Context, ownerID, id uuid.UUID, in UpdateDietTemplateInput) (DietTemplate, error) {
	var tpl DietTemplate
	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
		row, err := q.UpdateDietTemplate(ctx, sqlc.UpdateDietTemplateParams{ID: id, UserID: ownerID, Name: in.Name, SharedWithPartner: in.SharedWithPartner})
		if store.IsNotFound(err) {
			return ErrDietTemplateNotFound
		}
		if err != nil {
			return fmt.Errorf("update diet template: %w", err)
		}
		slotRows, err := q.GetTemplateSlots(ctx, id)
		if err != nil {
			return fmt.Errorf("get template slots: %w", err)
		}
		tpl, err = s.toTemplate(ctx, q, row, slotRows)
		return err
	})
	if err != nil {
		return DietTemplate{}, err
	}
	return tpl, nil
}

// Delete removes a template owned by ownerID. Its template_slots rows are
// removed by ON DELETE CASCADE; any plan_entries that came from it keep
// their row and have from_template_id set to NULL.
func (s *DietTemplates) Delete(ctx context.Context, ownerID, id uuid.UUID) error {
	n, err := s.st.DeleteDietTemplate(ctx, sqlc.DeleteDietTemplateParams{ID: id, UserID: ownerID})
	if err != nil {
		return fmt.Errorf("delete diet template: %w", err)
	}
	if n == 0 {
		return ErrDietTemplateNotFound
	}
	return nil
}

// List returns a page of the caller's alphabetical template list.
func (s *DietTemplates) List(ctx context.Context, ownerID uuid.UUID, in ListDietTemplatesInput) (DietTemplatePage, error) {
	if in.Limit < 1 {
		in.Limit = 1
	}
	params := sqlc.ListDietTemplatesForUserParams{UserID: ownerID, RowLimit: toRowLimit(in.Limit + 1)}
	if in.Cursor != nil {
		params.HasCursor = true
		params.CursorName = in.Cursor.Name
		params.CursorID = in.Cursor.ID
	}
	rows, err := s.st.ListDietTemplatesForUser(ctx, params)
	if err != nil {
		return DietTemplatePage{}, fmt.Errorf("list diet templates: %w", err)
	}
	var next *DietTemplateCursor
	if len(rows) > in.Limit {
		last := rows[in.Limit-1]
		next = &DietTemplateCursor{Name: last.Name, ID: last.ID}
		rows = rows[:in.Limit]
	}
	items := make([]DietTemplateSummary, len(rows))
	for i, r := range rows {
		items[i] = DietTemplateSummary{
			ID: r.ID, Name: r.Name, DayCount: int(r.DayCount),
			SharedWithPartner: r.SharedWithPartner, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
		}
	}
	return DietTemplatePage{Items: items, NextCursor: next}, nil
}
```

```go
// ReplaceSlots atomically replaces a template's full slot list. Every
// slot's day_index must be < the template's day_count, and every meal_id
// must exist and be owned by ownerID, before anything is written.
func (s *DietTemplates) ReplaceSlots(ctx context.Context, ownerID, id uuid.UUID, slots []TemplateSlotInput) (DietTemplate, error) {
	var tpl DietTemplate
	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
		row, err := q.GetDietTemplateForUser(ctx, sqlc.GetDietTemplateForUserParams{ID: id, UserID: ownerID})
		if store.IsNotFound(err) {
			return ErrDietTemplateNotFound
		}
		if err != nil {
			return fmt.Errorf("get diet template: %w", err)
		}

		for _, sl := range slots {
			if sl.DayIndex < 0 || sl.DayIndex >= int(row.DayCount) {
				return ErrDayIndexOutOfRange
			}
		}
		mealIDs := uniqueUUIDs(slots, func(sl TemplateSlotInput) uuid.UUID { return sl.MealID })
		if len(mealIDs) > 0 {
			mealRows, err := q.GetMealsForUser(ctx, sqlc.GetMealsForUserParams{Ids: mealIDs, UserID: ownerID})
			if err != nil {
				return fmt.Errorf("get meals: %w", err)
			}
			if len(mealRows) != len(mealIDs) {
				return ErrTemplateMealNotFound
			}
		}

		if err := q.DeleteTemplateSlots(ctx, id); err != nil {
			return fmt.Errorf("clear template slots: %w", err)
		}
		inserted := make([]sqlc.TemplateSlot, len(slots))
		for i, sl := range slots {
			ins, err := q.InsertTemplateSlot(ctx, sqlc.InsertTemplateSlotParams{
				TemplateID: id, DayIndex: int32(sl.DayIndex), Slot: sl.Slot, MealID: sl.MealID, Portion: sl.Portion,
			})
			if store.IsUniqueViolation(err, "template_slots_unique_slot_idx") {
				return ErrDuplicateSlot
			}
			if err != nil {
				return fmt.Errorf("insert template slot: %w", err)
			}
			inserted[i] = ins
		}
		tpl, err = s.toTemplate(ctx, q, row, inserted)
		return err
	})
	if err != nil {
		return DietTemplate{}, err
	}
	return tpl, nil
}

// Copy creates a new template owned by callerID, with the same name,
// day_count and slots as the template at id, and shared_with_partner always
// false regardless of the original. callerID must own the original.
func (s *DietTemplates) Copy(ctx context.Context, callerID, id uuid.UUID) (DietTemplate, error) {
	var tpl DietTemplate
	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
		original, err := q.GetDietTemplateForUser(ctx, sqlc.GetDietTemplateForUserParams{ID: id, UserID: callerID})
		if store.IsNotFound(err) {
			return ErrDietTemplateNotFound
		}
		if err != nil {
			return fmt.Errorf("get diet template: %w", err)
		}
		originalSlots, err := q.GetTemplateSlots(ctx, id)
		if err != nil {
			return fmt.Errorf("get template slots: %w", err)
		}

		copyRow, err := q.CreateDietTemplate(ctx, sqlc.CreateDietTemplateParams{
			OwnerID: callerID, Name: original.Name, DayCount: original.DayCount, SharedWithPartner: false,
		})
		if err != nil {
			return fmt.Errorf("create diet template copy: %w", err)
		}
		inserted := make([]sqlc.TemplateSlot, len(originalSlots))
		for i, sl := range originalSlots {
			ins, err := q.InsertTemplateSlot(ctx, sqlc.InsertTemplateSlotParams{
				TemplateID: copyRow.ID, DayIndex: sl.DayIndex, Slot: sl.Slot, MealID: sl.MealID, Portion: sl.Portion,
			})
			if err != nil {
				return fmt.Errorf("copy template slot: %w", err)
			}
			inserted[i] = ins
		}
		tpl, err = s.toTemplate(ctx, q, copyRow, inserted)
		return err
	})
	if err != nil {
		return DietTemplate{}, err
	}
	return tpl, nil
}
```

```go
// Apply copies the template's slots into plan_entries starting at
// startDate: day_index 0 lands on startDate, day_index 1 on startDate+1,
// and so on. Without overwrite, it fails with ErrPlanConflict (writing
// nothing) if any non-snack target date+slot already has an entry; snack
// slots are always additive and never conflict. With overwrite, the same
// non-snack targets are replaced first. Returns the number of entries
// written.
func (s *DietTemplates) Apply(ctx context.Context, ownerID, id uuid.UUID, in ApplyTemplateInput) (int, error) {
	var written int
	err := s.st.InTx(ctx, func(q *sqlc.Queries) error {
		_, err := q.GetDietTemplateForUser(ctx, sqlc.GetDietTemplateForUserParams{ID: id, UserID: ownerID})
		if store.IsNotFound(err) {
			return ErrDietTemplateNotFound
		}
		if err != nil {
			return fmt.Errorf("get diet template: %w", err)
		}
		slotRows, err := q.GetTemplateSlots(ctx, id)
		if err != nil {
			return fmt.Errorf("get template slots: %w", err)
		}

		targetDates := make([]time.Time, len(slotRows))
		for i, sl := range slotRows {
			targetDates[i] = in.StartDate.AddDate(0, 0, int(sl.DayIndex))
		}

		if !in.Overwrite {
			existing, err := q.GetPlanEntriesForUserOnDates(ctx, sqlc.GetPlanEntriesForUserOnDatesParams{UserID: ownerID, Dates: targetDates})
			if err != nil {
				return fmt.Errorf("check existing plan entries: %w", err)
			}
			existingNonSnack := make(map[string]bool, len(existing))
			for _, e := range existing {
				if e.Slot != "snack" {
					existingNonSnack[e.Date.Format("2006-01-02")+"|"+e.Slot] = true
				}
			}
			for i, sl := range slotRows {
				if sl.Slot == "snack" {
					continue
				}
				key := targetDates[i].Format("2006-01-02") + "|" + sl.Slot
				if existingNonSnack[key] {
					return ErrPlanConflict
				}
			}
		}

		for i, sl := range slotRows {
			if in.Overwrite && sl.Slot != "snack" {
				if _, err := q.DeletePlanEntryForUserOnDateSlot(ctx, sqlc.DeletePlanEntryForUserOnDateSlotParams{
					UserID: ownerID, Date: targetDates[i], Slot: sl.Slot,
				}); err != nil {
					return fmt.Errorf("clear existing plan entry: %w", err)
				}
			}
			fromID := id
			if _, err := q.InsertPlanEntry(ctx, sqlc.InsertPlanEntryParams{
				OwnerID: ownerID, Date: targetDates[i], Slot: sl.Slot, MealID: sl.MealID, Portion: sl.Portion, FromTemplateID: &fromID,
			}); err != nil {
				return fmt.Errorf("insert plan entry: %w", err)
			}
			written++
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return written, nil
}

// toTemplate builds a DietTemplate from a diet_templates row and its slot
// rows, fetching every referenced meal's name in one batch query (via q,
// which is either the plain store or a transaction's *sqlc.Queries).
func (s *DietTemplates) toTemplate(ctx context.Context, q *sqlc.Queries, row sqlc.DietTemplate, slotRows []sqlc.TemplateSlot) (DietTemplate, error) {
	slots := make([]TemplateSlot, len(slotRows))
	if len(slotRows) > 0 {
		mealIDs := uniqueUUIDs(slotRows, func(r sqlc.TemplateSlot) uuid.UUID { return r.MealID })
		mealRows, err := q.GetMealsForUser(ctx, sqlc.GetMealsForUserParams{Ids: mealIDs, UserID: row.OwnerID})
		if err != nil {
			return DietTemplate{}, fmt.Errorf("get meals: %w", err)
		}
		names := make(map[uuid.UUID]string, len(mealRows))
		for _, m := range mealRows {
			names[m.ID] = m.Name
		}
		for i, sl := range slotRows {
			name, ok := names[sl.MealID]
			if !ok {
				return DietTemplate{}, ErrTemplateMealNotFound
			}
			slots[i] = TemplateSlot{ID: sl.ID, DayIndex: int(sl.DayIndex), Slot: sl.Slot, MealID: sl.MealID, MealName: name, Portion: sl.Portion}
		}
	}
	return DietTemplate{
		ID: row.ID, Name: row.Name, DayCount: int(row.DayCount), SharedWithPartner: row.SharedWithPartner,
		Slots: slots, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}, nil
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/service/... -run 'TestDietTemplates|TestMeals' -v`
Expected: PASS, including all six new tests.

Run: `cd backend && go build ./...`
Expected: builds cleanly.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/service/util.go backend/internal/service/meals.go \
  backend/internal/service/diet_templates.go backend/internal/service/diet_templates_test.go
git commit -m "feat(backend): add the diet templates service"
```

---

### Task 6: `Plan` service

**Files:**
- Create: `backend/internal/service/plan.go`
- Create: `backend/internal/service/plan_test.go`

**Interfaces:**
- Consumes: the `sqlc.Queries` methods from Task 3 and `GetMealsForUser`/`GetUserByID` (Task 2, existing); `uniqueUUIDs` (Task 5); `store.IsNotFound`; `*service.Meals` — specifically its existing public `Get` method (unchanged).
- Produces (consumed by Task 8's handlers):
  - Errors `service.ErrPlanEntryNotFound`, `service.ErrPlanMealNotFound`, `service.ErrPlanRangeTooLong` (all `errors.New(...)`).
  - `type PlanEntry struct { ID uuid.UUID; Date time.Time; Slot string; MealID uuid.UUID; MealName string; Portion float64; FromTemplateID *uuid.UUID; CreatedAt, UpdatedAt time.Time }`
  - `type DailyTotal struct { Date time.Time; Entries []PlanEntry; NutritionPerDay map[string]float64 }`
  - `type Targets struct { Kcal, ProteinG, CarbsG, FatG *float64 }`
  - `type PlanRange struct { From, To time.Time; Days []DailyTotal; Targets Targets }`
  - `type SetPlanEntryInput struct { MealID uuid.UUID; Portion float64 }`
  - `func NewPlan(st *store.Store, meals *Meals) *Plan`
  - `func (*Plan) GetRange(ctx, ownerID uuid.UUID, from, to time.Time) (PlanRange, error)`
  - `func (*Plan) SetEntry(ctx, ownerID uuid.UUID, date time.Time, slot string, in SetPlanEntryInput) (PlanEntry, error)`
  - `func (*Plan) DeleteEntry(ctx, ownerID uuid.UUID, date time.Time, slot string) error`

- [ ] **Step 1: Write the failing tests**

Create `backend/internal/service/plan_test.go`:

```go
package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/InzKazik/mealplanner/backend/internal/service"
)

func newPlanFixture(t *testing.T) (*service.Plan, *service.Meals, *service.Ingredients, *store.Store) {
	t.Helper()
	_, st := newIngredientsFixture(t)
	meals := service.NewMeals(st)
	return service.NewPlan(st, meals), meals, service.NewIngredients(st), st
}

func TestPlanSetEntryUpsertsNonSnackSlotsAndClearsProvenance(t *testing.T) {
	plan, meals, _, st := newPlanFixture(t)
	owner := newTestUser(t, st, "planowner1@example.com")
	meal1 := mustCreateMeal(t, meals, owner, "First")
	meal2 := mustCreateMeal(t, meals, owner, "Second")
	date := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	first, err := plan.SetEntry(context.Background(), owner, date, "breakfast", service.SetPlanEntryInput{MealID: meal1.ID, Portion: 1})
	if err != nil {
		t.Fatalf("SetEntry (first): %v", err)
	}

	second, err := plan.SetEntry(context.Background(), owner, date, "breakfast", service.SetPlanEntryInput{MealID: meal2.ID, Portion: 2})
	if err != nil {
		t.Fatalf("SetEntry (swap): %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("swap created a new row (id %v), want the same row (id %v) updated in place", second.ID, first.ID)
	}
	if second.MealID != meal2.ID || second.Portion != 2 {
		t.Errorf("second = %+v, want meal2 at portion 2", second)
	}
	if second.FromTemplateID != nil {
		t.Errorf("FromTemplateID = %v, want nil after a manual set", second.FromTemplateID)
	}

	rng, err := plan.GetRange(context.Background(), owner, date, date)
	if err != nil {
		t.Fatalf("GetRange: %v", err)
	}
	if len(rng.Days) != 1 || len(rng.Days[0].Entries) != 1 {
		t.Fatalf("Days = %+v, want exactly one day with exactly one entry", rng.Days)
	}
}

func TestPlanSetEntryOnSnackAlwaysAddsANewRow(t *testing.T) {
	plan, meals, _, st := newPlanFixture(t)
	owner := newTestUser(t, st, "planowner2@example.com")
	meal := mustCreateMeal(t, meals, owner, "Snack Food")
	date := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	if _, err := plan.SetEntry(context.Background(), owner, date, "snack", service.SetPlanEntryInput{MealID: meal.ID, Portion: 1}); err != nil {
		t.Fatalf("SetEntry (first snack): %v", err)
	}
	if _, err := plan.SetEntry(context.Background(), owner, date, "snack", service.SetPlanEntryInput{MealID: meal.ID, Portion: 1}); err != nil {
		t.Fatalf("SetEntry (second snack): %v", err)
	}

	rng, err := plan.GetRange(context.Background(), owner, date, date)
	if err != nil {
		t.Fatalf("GetRange: %v", err)
	}
	if len(rng.Days) != 1 || len(rng.Days[0].Entries) != 2 {
		t.Fatalf("Days = %+v, want one day with two snack entries", rng.Days)
	}
}

func TestPlanSetEntryRejectsAMealNotOwnedByTheCaller(t *testing.T) {
	plan, meals, _, st := newPlanFixture(t)
	owner := newTestUser(t, st, "planowner3@example.com")
	other := newTestUser(t, st, "planother3@example.com")
	othersMeal := mustCreateMeal(t, meals, other, "Not Mine")

	_, err := plan.SetEntry(context.Background(), owner, time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), "lunch", service.SetPlanEntryInput{MealID: othersMeal.ID, Portion: 1})
	if !errors.Is(err, service.ErrPlanMealNotFound) {
		t.Errorf("err = %v, want ErrPlanMealNotFound", err)
	}
}

func TestPlanDeleteEntryRemovesTheOneNonSnackEntryOrAllSnacks(t *testing.T) {
	plan, meals, _, st := newPlanFixture(t)
	owner := newTestUser(t, st, "planowner4@example.com")
	meal := mustCreateMeal(t, meals, owner, "Meal")
	date := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	if _, err := plan.SetEntry(context.Background(), owner, date, "dinner", service.SetPlanEntryInput{MealID: meal.ID, Portion: 1}); err != nil {
		t.Fatalf("SetEntry: %v", err)
	}
	if err := plan.DeleteEntry(context.Background(), owner, date, "dinner"); err != nil {
		t.Errorf("DeleteEntry: %v", err)
	}
	if err := plan.DeleteEntry(context.Background(), owner, date, "dinner"); !errors.Is(err, service.ErrPlanEntryNotFound) {
		t.Errorf("DeleteEntry again: err = %v, want ErrPlanEntryNotFound", err)
	}

	if _, err := plan.SetEntry(context.Background(), owner, date, "snack", service.SetPlanEntryInput{MealID: meal.ID, Portion: 1}); err != nil {
		t.Fatalf("SetEntry snack 1: %v", err)
	}
	if _, err := plan.SetEntry(context.Background(), owner, date, "snack", service.SetPlanEntryInput{MealID: meal.ID, Portion: 1}); err != nil {
		t.Fatalf("SetEntry snack 2: %v", err)
	}
	if err := plan.DeleteEntry(context.Background(), owner, date, "snack"); err != nil {
		t.Errorf("DeleteEntry (snack, both): %v", err)
	}
	rng, err := plan.GetRange(context.Background(), owner, date, date)
	if err != nil {
		t.Fatalf("GetRange: %v", err)
	}
	if len(rng.Days[0].Entries) != 0 {
		t.Errorf("entries after deleting all snacks = %+v, want none", rng.Days[0].Entries)
	}
}

func TestPlanGetRangeComputesDailyTotalsAndPropagatesUnknownNutrients(t *testing.T) {
	plan, meals, ing, st := newPlanFixture(t)
	owner := newTestUser(t, st, "planowner5@example.com")

	rice, err := ing.Create(context.Background(), owner, service.CreateIngredientInput{
		Name: "Rice", Category: "grains_bread", Nutrients: map[string]float64{service.NutrientCalories: 130},
	})
	if err != nil {
		t.Fatalf("create ingredient: %v", err)
	}
	riceMeal, err := meals.Create(context.Background(), owner, service.CreateMealInput{Name: "Rice Bowl", Servings: 1})
	if err != nil {
		t.Fatalf("create meal: %v", err)
	}
	if _, err := meals.ReplaceIngredients(context.Background(), owner, riceMeal.ID, []service.MealIngredientInput{
		{IngredientID: rice.ID, Quantity: 200, Unit: "g"},
	}); err != nil {
		t.Fatalf("ReplaceIngredients: %v", err)
	}
	// riceMeal's calories per serving: 200/100*130 = 260.

	day1 := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	day2 := day1.AddDate(0, 0, 1)
	if _, err := plan.SetEntry(context.Background(), owner, day1, "breakfast", service.SetPlanEntryInput{MealID: riceMeal.ID, Portion: 2}); err != nil {
		t.Fatalf("SetEntry day1: %v", err)
	}
	// day1 total calories: 260 * 2 = 520.

	protein, err := ing.Create(context.Background(), owner, service.CreateIngredientInput{
		Name: "Mystery Protein", Category: "other", Nutrients: map[string]float64{service.NutrientProtein: 80},
	})
	if err != nil {
		t.Fatalf("create ingredient: %v", err)
	}
	unknownMeal, err := meals.Create(context.Background(), owner, service.CreateMealInput{Name: "Shake", Servings: 1})
	if err != nil {
		t.Fatalf("create meal: %v", err)
	}
	if _, err := meals.ReplaceIngredients(context.Background(), owner, unknownMeal.ID, []service.MealIngredientInput{
		{IngredientID: protein.ID, Quantity: 100, Unit: "g"},
	}); err != nil {
		t.Fatalf("ReplaceIngredients: %v", err)
	}
	if _, err := plan.SetEntry(context.Background(), owner, day2, "lunch", service.SetPlanEntryInput{MealID: unknownMeal.ID, Portion: 1}); err != nil {
		t.Fatalf("SetEntry day2: %v", err)
	}

	rng, err := plan.GetRange(context.Background(), owner, day1, day2)
	if err != nil {
		t.Fatalf("GetRange: %v", err)
	}
	if len(rng.Days) != 2 {
		t.Fatalf("Days = %+v, want 2 (day1 and day2)", rng.Days)
	}
	if got := rng.Days[0].NutritionPerDay[service.NutrientCalories]; got != 520 {
		t.Errorf("day1 calories = %v, want 520", got)
	}
	if _, ok := rng.Days[1].NutritionPerDay[service.NutrientCalories]; ok {
		t.Errorf("day2 calories = %v, want absent (unknownMeal has no calories data)", rng.Days[1].NutritionPerDay[service.NutrientCalories])
	}
}

func TestPlanGetRangeIncludesEmptyDaysAndRejectsATooLongRange(t *testing.T) {
	plan, _, _, st := newPlanFixture(t)
	owner := newTestUser(t, st, "planowner6@example.com")

	from := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 3)
	rng, err := plan.GetRange(context.Background(), owner, from, to)
	if err != nil {
		t.Fatalf("GetRange: %v", err)
	}
	if len(rng.Days) != 4 {
		t.Fatalf("Days = %d, want 4 (from..to inclusive, all empty)", len(rng.Days))
	}
	for _, d := range rng.Days {
		if len(d.Entries) != 0 {
			t.Errorf("day %v entries = %+v, want none", d.Date, d.Entries)
		}
		if got := d.NutritionPerDay[service.NutrientCalories]; got != 0 {
			t.Errorf("day %v calories = %v, want 0", d.Date, got)
		}
	}

	_, err = plan.GetRange(context.Background(), owner, from, from.AddDate(0, 0, 93))
	if !errors.Is(err, service.ErrPlanRangeTooLong) {
		t.Errorf("a 93-day range: err = %v, want ErrPlanRangeTooLong", err)
	}
}
```

Add `"github.com/InzKazik/mealplanner/backend/internal/store"` to the file's imports (for `*store.Store` in `newPlanFixture`'s signature).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/service/... -run TestPlan -v`
Expected: FAIL to compile — `service.Plan` etc. do not exist yet.

- [ ] **Step 3: Implement the service**

Create `backend/internal/service/plan.go`:

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

const maxPlanRangeDays = 92

// Errors returned by Plan. Handlers map them to problem responses.
var (
	ErrPlanEntryNotFound = errors.New("plan entry not found")
	ErrPlanMealNotFound  = errors.New("meal does not exist or is not visible to you")
	// ErrPlanRangeTooLong means [from, to] spans more than maxPlanRangeDays.
	// There is no growing list here to cursor-paginate (§4.2 reserves that
	// for meals/ingredients/diet-templates), so this bounds the request
	// size instead; OpenAPI's JSON Schema can't express a cross-field
	// constraint between from and to, so it is enforced here.
	ErrPlanRangeTooLong = errors.New("date range is too long")
)

// PlanEntry is one scheduled meal on the calendar, with the referenced
// meal's name inlined.
type PlanEntry struct {
	ID             uuid.UUID
	Date           time.Time
	Slot           string
	MealID         uuid.UUID
	MealName       string
	Portion        float64
	FromTemplateID *uuid.UUID
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// DailyTotal is one calendar date's entries plus computed nutrition, using
// the same null-propagation rule as a single meal one level up: a key is
// absent if any contributing entry's meal has it absent. A date with no
// entries reports 0 for every key.
type DailyTotal struct {
	Date            time.Time
	Entries         []PlanEntry
	NutritionPerDay map[string]float64
}

// Targets are the caller's daily nutrition goals, straight from their
// profile (nil where unset). Comparing totals against targets is a client
// concern; this just puts both in the same response.
type Targets struct {
	Kcal     *float64
	ProteinG *float64
	CarbsG   *float64
	FatG     *float64
}

// PlanRange is [From, To] inclusive, one DailyTotal per calendar date, plus
// the caller's targets.
type PlanRange struct {
	From    time.Time
	To      time.Time
	Days    []DailyTotal
	Targets Targets
}

// SetPlanEntryInput is the meal and portion for one date+slot.
type SetPlanEntryInput struct {
	MealID  uuid.UUID
	Portion float64
}

// Plan implements the calendar: which meal is scheduled for which date and
// slot, and the nutrition totals that follow from it. Unlike every other
// service in this package, it depends on Meals rather than reading meal rows
// via store directly — specifically to reuse Meals.Get's nutrition
// computation instead of reimplementing unit conversion and
// null-propagation a third time. See the diets-and-plan plan's Global
// Constraints for the reasoning.
type Plan struct {
	st    *store.Store
	meals *Meals
}

// NewPlan returns a Plan service.
func NewPlan(st *store.Store, meals *Meals) *Plan { return &Plan{st: st, meals: meals} }

// GetRange returns one DailyTotal per calendar date in [from, to]
// inclusive, capped at maxPlanRangeDays.
func (s *Plan) GetRange(ctx context.Context, ownerID uuid.UUID, from, to time.Time) (PlanRange, error) {
	if to.Before(from) || int(to.Sub(from).Hours()/24) > maxPlanRangeDays {
		return PlanRange{}, ErrPlanRangeTooLong
	}

	rows, err := s.st.GetPlanEntriesForUserInRange(ctx, sqlc.GetPlanEntriesForUserInRangeParams{UserID: ownerID, FromDate: from, ToDate: to})
	if err != nil {
		return PlanRange{}, fmt.Errorf("get plan entries: %w", err)
	}

	mealIDs := uniqueUUIDs(rows, func(r sqlc.PlanEntry) uuid.UUID { return r.MealID })
	mealsByID := make(map[uuid.UUID]Meal, len(mealIDs))
	for _, id := range mealIDs {
		m, err := s.meals.Get(ctx, ownerID, id)
		if err != nil {
			return PlanRange{}, fmt.Errorf("get meal %s: %w", id, err)
		}
		mealsByID[id] = m
	}

	byDate := make(map[string][]sqlc.PlanEntry, len(rows))
	for _, r := range rows {
		key := r.Date.Format("2006-01-02")
		byDate[key] = append(byDate[key], r)
	}

	user, err := s.st.GetUserByID(ctx, ownerID)
	if err != nil {
		return PlanRange{}, fmt.Errorf("get user: %w", err)
	}

	var days []DailyTotal
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		dayRows := byDate[d.Format("2006-01-02")]
		entries := make([]PlanEntry, len(dayRows))
		totals := make(map[string]float64, len(allNutrientKeys))
		for _, k := range allNutrientKeys {
			totals[k] = 0
		}
		unknown := make(map[string]bool, len(allNutrientKeys))
		for i, r := range dayRows {
			m := mealsByID[r.MealID]
			entries[i] = PlanEntry{
				ID: r.ID, Date: r.Date, Slot: r.Slot, MealID: r.MealID, MealName: m.Name,
				Portion: r.Portion, FromTemplateID: r.FromTemplateID, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
			}
			for _, k := range allNutrientKeys {
				amount, ok := m.NutritionPerServing[k]
				if !ok {
					unknown[k] = true
					continue
				}
				totals[k] += amount * r.Portion
			}
		}
		perDay := make(map[string]float64, len(allNutrientKeys))
		for _, k := range allNutrientKeys {
			if unknown[k] {
				continue
			}
			perDay[k] = totals[k]
		}
		days = append(days, DailyTotal{Date: d, Entries: entries, NutritionPerDay: perDay})
	}

	return PlanRange{
		From: from, To: to, Days: days,
		Targets: Targets{Kcal: user.TargetKcal, ProteinG: user.TargetProteinG, CarbsG: user.TargetCarbsG, FatG: user.TargetFatG},
	}, nil
}

// SetEntry sets or swaps the meal (and/or portion) for a date and slot. For
// breakfast/lunch/dinner this is a true upsert (one entry per date+slot).
// For snack, since a URL can't select among several, this always adds a new
// entry. Either way the written entry's from_template_id is NULL: a manual
// set is no longer "from" a template, even if it replaces one that was.
func (s *Plan) SetEntry(ctx context.Context, ownerID uuid.UUID, date time.Time, slot string, in SetPlanEntryInput) (PlanEntry, error) {
	meal, err := s.st.GetMealForUser(ctx, sqlc.GetMealForUserParams{ID: in.MealID, UserID: ownerID})
	if store.IsNotFound(err) {
		return PlanEntry{}, ErrPlanMealNotFound
	}
	if err != nil {
		return PlanEntry{}, fmt.Errorf("get meal: %w", err)
	}

	var row sqlc.PlanEntry
	if slot == "snack" {
		row, err = s.st.InsertPlanEntry(ctx, sqlc.InsertPlanEntryParams{
			OwnerID: ownerID, Date: date, Slot: slot, MealID: in.MealID, Portion: in.Portion, FromTemplateID: nil,
		})
	} else {
		row, err = s.st.UpsertPlanEntry(ctx, sqlc.UpsertPlanEntryParams{
			OwnerID: ownerID, Date: date, Slot: slot, MealID: in.MealID, Portion: in.Portion,
		})
	}
	if err != nil {
		return PlanEntry{}, fmt.Errorf("set plan entry: %w", err)
	}
	return PlanEntry{
		ID: row.ID, Date: row.Date, Slot: row.Slot, MealID: row.MealID, MealName: meal.Name,
		Portion: row.Portion, FromTemplateID: row.FromTemplateID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}, nil
}

// DeleteEntry removes the plan entry (or entries) for a date and slot. For
// breakfast/lunch/dinner this removes the one entry. For snack, since a URL
// can't select among several, this removes every snack entry for that date.
func (s *Plan) DeleteEntry(ctx context.Context, ownerID uuid.UUID, date time.Time, slot string) error {
	var n int64
	var err error
	if slot == "snack" {
		n, err = s.st.DeleteAllSnackEntriesForUserOnDate(ctx, sqlc.DeleteAllSnackEntriesForUserOnDateParams{UserID: ownerID, Date: date})
	} else {
		n, err = s.st.DeletePlanEntryForUserOnDateSlot(ctx, sqlc.DeletePlanEntryForUserOnDateSlotParams{UserID: ownerID, Date: date, Slot: slot})
	}
	if err != nil {
		return fmt.Errorf("delete plan entry: %w", err)
	}
	if n == 0 {
		return ErrPlanEntryNotFound
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/service/... -run TestPlan -v`
Expected: PASS, including all six new tests.

Run: `cd backend && go build ./...`
Expected: builds cleanly.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/service/plan.go backend/internal/service/plan_test.go
git commit -m "feat(backend): add the plan service"
```

---

### Task 7: OpenAPI contract and handlers — `/diet-templates`

Contract and handlers are one task (see Global Constraints): adding `DietTemplates`' methods to `api.ServerInterface` and immediately implementing them keeps `go build` green the whole way through, unlike the meals plan's split Tasks 5/6.

**Files:**
- Modify: `openapi.yaml`
- Modify (generated, commit the output): `backend/internal/api/api.gen.go`
- Create: `backend/internal/httpapi/diet_templates.go`
- Modify: `backend/internal/httpapi/problem.go`
- Modify: `backend/internal/httpapi/account.go`
- Modify: `backend/internal/httpapi/server.go`, `router.go`

**Interfaces:**
- Consumes: `service.DietTemplates` and its types from Task 5; the existing `toNullable`/`toOptionalString`/`decodeJSON`/`requireUser`/`writeJSON` helpers and cursor-codec pattern from `httpapi/meals.go`.
- Produces: `httpapi.DietTemplatesService` interface (consumed by `router.go`'s `Deps.DietTemplates` and by Task 9's tests); `api.Slot` (a `string` type shared by Task 8's plan schemas), `api.TemplateSlot`, `api.DietTemplate`, `api.DietTemplateSummary`, `api.DietTemplateList`, `api.CreateDietTemplateRequest`, `api.UpdateDietTemplateRequest`, `api.TemplateSlotInput`, `api.ReplaceTemplateSlotsRequest`, `api.ApplyDietTemplateRequest`, and the eight `api.ServerInterface` methods `ListDietTemplates`, `CreateDietTemplate`, `GetDietTemplate`, `UpdateDietTemplate`, `DeleteDietTemplate`, `ReplaceTemplateSlots`, `ApplyDietTemplate`, `CopyDietTemplate`.

  `TemplateSlotInput.Portion` and (Task 8) `SetPlanEntryRequest.Portion` are `*float64` (optional, `default: 1`), not plain `float64` — the OpenAPI schema deliberately does not mark `portion` `required`, matching the spec's "portion (numeric, default 1)". The handlers substitute `1.0` when the pointer is nil.

- [ ] **Step 1: Add the tag**

In `openapi.yaml`, after the `Meals` tag, add:

```yaml
  - name: DietTemplates
    description: Reusable meal-schedule templates that can be applied to the plan.
```

- [ ] **Step 2: Add the shared `Slot` schema**

In `openapi.yaml`'s `components.schemas`, after `Unit`, add:

```yaml
    Slot:
      type: string
      enum: [breakfast, lunch, dinner, snack]
```

`Slot` is shared by `template_slots` (this task) and `plan_entries` (Task 8).

- [ ] **Step 3: Add the diet-template schemas**

In `openapi.yaml`'s `components.schemas`, after `MealList`, add:

```yaml
    TemplateSlot:
      type: object
      required: [id, day_index, slot, meal_id, meal_name, portion]
      properties:
        id:
          type: string
          format: uuid
        day_index:
          type: integer
        slot:
          $ref: '#/components/schemas/Slot'
        meal_id:
          type: string
          format: uuid
        meal_name:
          type: string
        portion:
          type: number
          format: double
    DietTemplate:
      type: object
      required: [id, name, day_count, shared_with_partner, slots, created_at, updated_at]
      properties:
        id:
          type: string
          format: uuid
        name:
          type: string
        day_count:
          type: integer
        shared_with_partner:
          type: boolean
        slots:
          type: array
          items:
            $ref: '#/components/schemas/TemplateSlot'
        created_at:
          type: string
          format: date-time
        updated_at:
          type: string
          format: date-time
    DietTemplateSummary:
      type: object
      required: [id, name, day_count, shared_with_partner, created_at, updated_at]
      properties:
        id:
          type: string
          format: uuid
        name:
          type: string
        day_count:
          type: integer
        shared_with_partner:
          type: boolean
        created_at:
          type: string
          format: date-time
        updated_at:
          type: string
          format: date-time
    CreateDietTemplateRequest:
      type: object
      additionalProperties: false
      required: [name, day_count]
      properties:
        name:
          type: string
          minLength: 1
          maxLength: 200
        day_count:
          type: integer
          minimum: 1
          maximum: 31
        shared_with_partner:
          type: boolean
    UpdateDietTemplateRequest:
      type: object
      additionalProperties: false
      properties:
        name:
          type: string
          minLength: 1
          maxLength: 200
        shared_with_partner:
          type: boolean
    TemplateSlotInput:
      type: object
      additionalProperties: false
      required: [day_index, slot, meal_id]
      properties:
        day_index:
          type: integer
          minimum: 0
          maximum: 30
        slot:
          $ref: '#/components/schemas/Slot'
        meal_id:
          type: string
          format: uuid
        portion:
          type: number
          format: double
          exclusiveMinimum: true
          minimum: 0
          maximum: 100
          default: 1
    ReplaceTemplateSlotsRequest:
      type: object
      additionalProperties: false
      required: [items]
      properties:
        items:
          type: array
          maxItems: 500
          items:
            $ref: '#/components/schemas/TemplateSlotInput'
    ApplyDietTemplateRequest:
      type: object
      additionalProperties: false
      required: [start_date]
      properties:
        start_date:
          type: string
          format: date
        overwrite:
          type: boolean
          default: false
    DietTemplateList:
      type: object
      required: [items, next_cursor]
      properties:
        items:
          type: array
          items:
            $ref: '#/components/schemas/DietTemplateSummary'
        next_cursor:
          type: string
          nullable: true
```

- [ ] **Step 4: Add the five paths**

In `openapi.yaml`, immediately before `/meals:`, add:

```yaml
  /diet-templates:
    get:
      tags: [DietTemplates]
      operationId: listDietTemplates
      summary: List the caller's diet templates
      description: Cursor-paginated, alphabetical by name. Does not include slots; fetch a single template for those.
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
          description: The caller's diet templates.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/DietTemplateList'
        '400':
          $ref: '#/components/responses/BadRequest'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '429':
          $ref: '#/components/responses/TooManyRequests'
        '500':
          $ref: '#/components/responses/Problem'
    post:
      tags: [DietTemplates]
      operationId: createDietTemplate
      summary: Create a diet template
      description: Creates the template with no slots. Add slots with `PUT /diet-templates/{id}/slots`.
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/CreateDietTemplateRequest'
      responses:
        '201':
          description: The template was created.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/DietTemplate'
        '400':
          $ref: '#/components/responses/BadRequest'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '429':
          $ref: '#/components/responses/TooManyRequests'
        '500':
          $ref: '#/components/responses/Problem'
  /diet-templates/{id}:
    parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
          format: uuid
    get:
      tags: [DietTemplates]
      operationId: getDietTemplate
      summary: Get a diet template
      description: Includes the slot list.
      responses:
        '200':
          description: The template.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/DietTemplate'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '404':
          $ref: '#/components/responses/NotFound'
        '429':
          $ref: '#/components/responses/TooManyRequests'
        '500':
          $ref: '#/components/responses/Problem'
    patch:
      tags: [DietTemplates]
      operationId: updateDietTemplate
      summary: Update a diet template
      description: Fields that are absent are left unchanged. day_count cannot be changed after creation; recreate or copy the template instead.
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/UpdateDietTemplateRequest'
      responses:
        '200':
          description: The updated template.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/DietTemplate'
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
      tags: [DietTemplates]
      operationId: deleteDietTemplate
      summary: Delete a diet template
      description: Any plan entries created by applying this template keep their row, with from_template_id set to null.
      responses:
        '204':
          description: The template was deleted.
        '401':
          $ref: '#/components/responses/Unauthorized'
        '404':
          $ref: '#/components/responses/NotFound'
        '429':
          $ref: '#/components/responses/TooManyRequests'
        '500':
          $ref: '#/components/responses/Problem'
  /diet-templates/{id}/slots:
    parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
          format: uuid
    put:
      tags: [DietTemplates]
      operationId: replaceTemplateSlots
      summary: Replace a diet template's slot list
      description: Atomically replaces the full slot list. Every row is validated (day_index must be within the template's day_count, and the meal must exist and be visible to the caller) before anything is written; an empty `items` array clears the list.
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/ReplaceTemplateSlotsRequest'
      responses:
        '200':
          description: The updated template.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/DietTemplate'
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
  /diet-templates/{id}/apply:
    parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
          format: uuid
    post:
      tags: [DietTemplates]
      operationId: applyDietTemplate
      summary: Apply a template to the plan
      description: Copies the template's slots into plan entries starting at `start_date`. Without `overwrite`, fails if any non-snack target date+slot already has an entry; snack slots are always added. With `overwrite`, the same non-snack targets are replaced.
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/ApplyDietTemplateRequest'
      responses:
        '204':
          description: The template was applied.
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
  /diet-templates/{id}/copy:
    parameters:
      - name: id
        in: path
        required: true
        schema:
          type: string
          format: uuid
    post:
      tags: [DietTemplates]
      operationId: copyDietTemplate
      summary: Copy a diet template
      description: Creates a new template, owned by the caller, with the same name, day_count and slots. The copy always starts with `shared_with_partner` false.
      responses:
        '201':
          description: The copy.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/DietTemplate'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '404':
          $ref: '#/components/responses/NotFound'
        '429':
          $ref: '#/components/responses/TooManyRequests'
        '500':
          $ref: '#/components/responses/Problem'
```

Run: `make lint-api` — expect the same pre-existing warnings only, no new ones.
Run: `make generate` — regenerates `backend/internal/api/api.gen.go`.

- [ ] **Step 5: Add the new problem codes**

In `backend/internal/httpapi/problem.go`, add to the `Code*` constants:

```go
	CodeDayIndexOutOfRange = "day_index_out_of_range"
	CodeInvalidMeal        = "invalid_meal"
	CodeDuplicateSlot      = "duplicate_slot"
	CodePlanConflict       = "plan_conflict"
```

- [ ] **Step 6: Add the `writeServiceError` cases**

In `backend/internal/httpapi/account.go`, in `writeServiceError`, add (next to the existing `ErrMealInUse` case):

```go
	case errors.Is(err, service.ErrDietTemplateNotFound):
		WriteProblem(w, http.StatusNotFound, CodeNotFound, "")
	case errors.Is(err, service.ErrDayIndexOutOfRange):
		WriteProblem(w, http.StatusBadRequest, CodeDayIndexOutOfRange, "")
	case errors.Is(err, service.ErrTemplateMealNotFound):
		WriteProblem(w, http.StatusBadRequest, CodeInvalidMeal, "")
	case errors.Is(err, service.ErrDuplicateSlot):
		WriteProblem(w, http.StatusConflict, CodeDuplicateSlot, "")
	case errors.Is(err, service.ErrPlanConflict):
		WriteProblem(w, http.StatusConflict, CodePlanConflict, "")
```

- [ ] **Step 7: Write the handlers**

Create `backend/internal/httpapi/diet_templates.go`:

```go
package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/api"
	"github.com/InzKazik/mealplanner/backend/internal/service"
)

// DietTemplatesService is what the diet-template handlers need from the
// diet templates service.
type DietTemplatesService interface {
	Create(ctx context.Context, ownerID uuid.UUID, in service.CreateDietTemplateInput) (service.DietTemplate, error)
	Get(ctx context.Context, ownerID, id uuid.UUID) (service.DietTemplate, error)
	Update(ctx context.Context, ownerID, id uuid.UUID, in service.UpdateDietTemplateInput) (service.DietTemplate, error)
	Delete(ctx context.Context, ownerID, id uuid.UUID) error
	List(ctx context.Context, ownerID uuid.UUID, in service.ListDietTemplatesInput) (service.DietTemplatePage, error)
	ReplaceSlots(ctx context.Context, ownerID, id uuid.UUID, slots []service.TemplateSlotInput) (service.DietTemplate, error)
	Copy(ctx context.Context, callerID, id uuid.UUID) (service.DietTemplate, error)
	Apply(ctx context.Context, ownerID, id uuid.UUID, in service.ApplyTemplateInput) (int, error)
}

const defaultDietTemplateLimit = 20

func (s *server) ListDietTemplates(w http.ResponseWriter, r *http.Request, params api.ListDietTemplatesParams) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	limit := defaultDietTemplateLimit
	if params.Limit != nil {
		limit = *params.Limit
	}
	var cursor *service.DietTemplateCursor
	if params.Cursor != nil {
		c, ok := decodeDietTemplateCursor(*params.Cursor)
		if !ok {
			WriteValidationProblem(w, "cursor is invalid", []FieldError{{Field: "cursor", Code: FieldInvalidForm}})
			return
		}
		cursor = &c
	}
	page, err := s.dietTemplates.List(r.Context(), userID, service.ListDietTemplatesInput{Cursor: cursor, Limit: limit})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	next := ""
	if page.NextCursor != nil {
		next = encodeDietTemplateCursor(*page.NextCursor)
	}
	writeJSON(w, http.StatusOK, toDietTemplateList(page.Items, next))
}

func (s *server) CreateDietTemplate(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.CreateDietTemplateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in := service.CreateDietTemplateInput{Name: req.Name, DayCount: req.DayCount}
	if req.SharedWithPartner != nil {
		in.SharedWithPartner = *req.SharedWithPartner
	}
	tpl, err := s.dietTemplates.Create(r.Context(), userID, in)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAPIDietTemplate(tpl))
}

func (s *server) GetDietTemplate(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	tpl, err := s.dietTemplates.Get(r.Context(), userID, id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIDietTemplate(tpl))
}

func (s *server) UpdateDietTemplate(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.UpdateDietTemplateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	tpl, err := s.dietTemplates.Update(r.Context(), userID, id, service.UpdateDietTemplateInput{Name: req.Name, SharedWithPartner: req.SharedWithPartner})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIDietTemplate(tpl))
}

func (s *server) DeleteDietTemplate(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	if err := s.dietTemplates.Delete(r.Context(), userID, id); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) ReplaceTemplateSlots(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.ReplaceTemplateSlotsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	slots := make([]service.TemplateSlotInput, len(req.Items))
	for i, it := range req.Items {
		portion := 1.0
		if it.Portion != nil {
			portion = *it.Portion
		}
		slots[i] = service.TemplateSlotInput{DayIndex: it.DayIndex, Slot: string(it.Slot), MealID: it.MealId, Portion: portion}
	}
	tpl, err := s.dietTemplates.ReplaceSlots(r.Context(), userID, id, slots)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIDietTemplate(tpl))
}

func (s *server) ApplyDietTemplate(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.ApplyDietTemplateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	overwrite := false
	if req.Overwrite != nil {
		overwrite = *req.Overwrite
	}
	if _, err := s.dietTemplates.Apply(r.Context(), userID, id, service.ApplyTemplateInput{StartDate: req.StartDate.Time, Overwrite: overwrite}); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) CopyDietTemplate(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	tpl, err := s.dietTemplates.Copy(r.Context(), userID, id)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAPIDietTemplate(tpl))
}

func toDietTemplateList(items []service.DietTemplateSummary, nextCursor string) api.DietTemplateList {
	list := api.DietTemplateList{Items: make([]api.DietTemplateSummary, len(items))}
	for i, t := range items {
		list.Items[i] = toAPIDietTemplateSummary(t)
	}
	if nextCursor == "" {
		list.NextCursor = nullable.NewNullNullable[string]()
	} else {
		list.NextCursor = nullable.NewNullableWithValue(nextCursor)
	}
	return list
}

func toAPIDietTemplateSummary(t service.DietTemplateSummary) api.DietTemplateSummary {
	return api.DietTemplateSummary{
		Id: t.ID, Name: t.Name, DayCount: t.DayCount, SharedWithPartner: t.SharedWithPartner,
		CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
}

func toAPIDietTemplate(t service.DietTemplate) api.DietTemplate {
	slots := make([]api.TemplateSlot, len(t.Slots))
	for i, sl := range t.Slots {
		slots[i] = api.TemplateSlot{
			Id: sl.ID, DayIndex: sl.DayIndex, Slot: api.Slot(sl.Slot),
			MealId: sl.MealID, MealName: sl.MealName, Portion: sl.Portion,
		}
	}
	return api.DietTemplate{
		Id: t.ID, Name: t.Name, DayCount: t.DayCount, SharedWithPartner: t.SharedWithPartner,
		Slots: slots, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt,
	}
}

type dietTemplateCursorPayload struct {
	Name string    `json:"n"`
	ID   uuid.UUID `json:"i"`
}

func encodeDietTemplateCursor(c service.DietTemplateCursor) string {
	b, _ := json.Marshal(dietTemplateCursorPayload{Name: c.Name, ID: c.ID})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeDietTemplateCursor(s string) (service.DietTemplateCursor, bool) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return service.DietTemplateCursor{}, false
	}
	var p dietTemplateCursorPayload
	if err := json.Unmarshal(b, &p); err != nil || p.Name == "" || p.ID == uuid.Nil {
		return service.DietTemplateCursor{}, false
	}
	return service.DietTemplateCursor{Name: p.Name, ID: p.ID}, true
}
```

Add `"github.com/oapi-codegen/nullable"` to the imports (for `toDietTemplateList`'s `nullable.NewNullNullable`/`NewNullableWithValue`, mirroring `httpapi/meals.go`).

**A note on `req.StartDate`'s type**, since it was not scratch-verified the way the meals plan verified its riskier type inferences: `openapi.yaml`'s `start_date` field is `{type: string, format: date}`. oapi-codegen's documented default for `format: date` (no `output-options.date-type` override in `backend/internal/api/oapi.yaml`) is `openapi_types.Date` (from `github.com/oapi-codegen/runtime/types`, already imported by `httpapi/meals.go` under that alias for UUID path params), a struct wrapping `Time time.Time` with `"2006-01-02"` JSON (un)marshalling — hence `req.StartDate.Time` above. If `make generate` in Step 4 produces a different type for `start_date` (for example a plain `string`, or `time.Time` directly), adjust `ApplyDietTemplate`'s one line accordingly; nothing else in this task depends on the exact type.

- [ ] **Step 8: Wire the new service into `server` and the router**

In `backend/internal/httpapi/server.go`, add a `dietTemplates DietTemplatesService` field to the `server` struct.

In `backend/internal/httpapi/router.go`, add `DietTemplates DietTemplatesService` to `Deps`, add it to the nil-check panic guard (`d.DietTemplates == nil`) and its message, and add `dietTemplates: d.DietTemplates` to the `srv := &server{...}` literal.

- [ ] **Step 9: Build**

Run: `cd backend && go build ./...`
Expected: builds cleanly.

Run: `cd backend && go vet ./...`
Expected: clean.

- [ ] **Step 10: Commit**

```bash
git add openapi.yaml backend/internal/api/api.gen.go backend/internal/httpapi/diet_templates.go \
  backend/internal/httpapi/problem.go backend/internal/httpapi/account.go \
  backend/internal/httpapi/server.go backend/internal/httpapi/router.go
git commit -m "feat(api): add the diet-templates endpoints and their handlers"
```

---

### Task 8: OpenAPI contract and handlers — `/plan`

Also one combined task, same reasoning as Task 7.

**Files:**
- Modify: `openapi.yaml`
- Modify (generated, commit the output): `backend/internal/api/api.gen.go`
- Create: `backend/internal/httpapi/plan.go`
- Modify: `backend/internal/httpapi/problem.go`
- Modify: `backend/internal/httpapi/account.go`
- Modify: `backend/internal/httpapi/server.go`, `router.go`

**Interfaces:**
- Consumes: `service.Plan` and its types from Task 6; `api.Slot` from Task 7; the same helpers `diet_templates.go` uses.
- Produces: `httpapi.PlanService` interface; `api.PlanEntry`, `api.DailyTotal`, `api.Targets`, `api.PlanRange`, `api.SetPlanEntryRequest`, and the three `api.ServerInterface` methods `GetPlan`, `SetPlanEntry`, `DeletePlanEntry`.

- [ ] **Step 1: Add the tag**

In `openapi.yaml`, after the `DietTemplates` tag, add:

```yaml
  - name: Plan
    description: The calendar of scheduled meals and their computed nutrition totals.
```

- [ ] **Step 2: Add the plan schemas**

In `openapi.yaml`'s `components.schemas`, after `DietTemplateList`, add:

```yaml
    PlanEntry:
      type: object
      required: [id, date, slot, meal_id, meal_name, portion, from_template_id, created_at, updated_at]
      properties:
        id:
          type: string
          format: uuid
        date:
          type: string
          format: date
        slot:
          $ref: '#/components/schemas/Slot'
        meal_id:
          type: string
          format: uuid
        meal_name:
          type: string
        portion:
          type: number
          format: double
        from_template_id:
          type: string
          format: uuid
          nullable: true
        created_at:
          type: string
          format: date-time
        updated_at:
          type: string
          format: date-time
    DailyTotal:
      type: object
      required: [date, entries, nutrition_per_day]
      properties:
        date:
          type: string
          format: date
        entries:
          type: array
          items:
            $ref: '#/components/schemas/PlanEntry'
        nutrition_per_day:
          $ref: '#/components/schemas/NutrientAmounts'
    Targets:
      type: object
      required: [target_kcal, target_protein_g, target_carbs_g, target_fat_g]
      properties:
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
    PlanRange:
      type: object
      required: [from, to, days, targets]
      properties:
        from:
          type: string
          format: date
        to:
          type: string
          format: date
        days:
          type: array
          items:
            $ref: '#/components/schemas/DailyTotal'
        targets:
          $ref: '#/components/schemas/Targets'
    SetPlanEntryRequest:
      type: object
      additionalProperties: false
      required: [meal_id]
      properties:
        meal_id:
          type: string
          format: uuid
        portion:
          type: number
          format: double
          exclusiveMinimum: true
          minimum: 0
          maximum: 100
          default: 1
```

- [ ] **Step 3: Add the two paths**

In `openapi.yaml`, immediately before `/healthz:`, add:

```yaml
  /plan:
    get:
      tags: [Plan]
      operationId: getPlan
      summary: Get the plan for a date range
      description: One DailyTotal per calendar date in [from, to] inclusive, with nutrition computed from each entry's meal and compared against the caller's targets. Capped at 92 days.
      parameters:
        - name: from
          in: query
          required: true
          schema:
            type: string
            format: date
        - name: to
          in: query
          required: true
          schema:
            type: string
            format: date
      responses:
        '200':
          description: The plan for the range.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/PlanRange'
        '400':
          $ref: '#/components/responses/BadRequest'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '429':
          $ref: '#/components/responses/TooManyRequests'
        '500':
          $ref: '#/components/responses/Problem'
  /plan/{date}/{slot}:
    parameters:
      - name: date
        in: path
        required: true
        schema:
          type: string
          format: date
      - name: slot
        in: path
        required: true
        schema:
          $ref: '#/components/schemas/Slot'
    put:
      tags: [Plan]
      operationId: setPlanEntry
      summary: Set or swap the meal for a date and slot
      description: For breakfast/lunch/dinner this upserts the one entry for that date+slot. For snack, since the URL can't select among several, this always adds a new entry. Either way, the written entry's from_template_id is null.
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/SetPlanEntryRequest'
      responses:
        '200':
          description: The entry.
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/PlanEntry'
        '400':
          $ref: '#/components/responses/BadRequest'
        '401':
          $ref: '#/components/responses/Unauthorized'
        '429':
          $ref: '#/components/responses/TooManyRequests'
        '500':
          $ref: '#/components/responses/Problem'
    delete:
      tags: [Plan]
      operationId: deletePlanEntry
      summary: Remove the plan entry (or entries) for a date and slot
      description: For breakfast/lunch/dinner this removes the one entry. For snack this removes every snack entry for that date.
      responses:
        '204':
          description: The entry (or entries) was removed.
        '401':
          $ref: '#/components/responses/Unauthorized'
        '404':
          $ref: '#/components/responses/NotFound'
        '429':
          $ref: '#/components/responses/TooManyRequests'
        '500':
          $ref: '#/components/responses/Problem'
```

Run: `make lint-api` — expect the same pre-existing warnings only, no new ones.
Run: `make generate` — regenerates `backend/internal/api/api.gen.go`.

- [ ] **Step 4: Add the new problem codes**

In `backend/internal/httpapi/problem.go`, add to the `Code*` constants:

```go
	CodePlanRangeTooLong = "plan_range_too_long"
```

- [ ] **Step 5: Add the `writeServiceError` cases**

In `backend/internal/httpapi/account.go`, in `writeServiceError`, add:

```go
	case errors.Is(err, service.ErrPlanEntryNotFound):
		WriteProblem(w, http.StatusNotFound, CodeNotFound, "")
	case errors.Is(err, service.ErrPlanMealNotFound):
		WriteProblem(w, http.StatusBadRequest, CodeInvalidMeal, "")
	case errors.Is(err, service.ErrPlanRangeTooLong):
		WriteProblem(w, http.StatusBadRequest, CodePlanRangeTooLong, "")
```

- [ ] **Step 6: Write the handlers**

Create `backend/internal/httpapi/plan.go`:

```go
package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/InzKazik/mealplanner/backend/internal/api"
	"github.com/InzKazik/mealplanner/backend/internal/service"
)

// PlanService is what the plan handlers need from the plan service.
type PlanService interface {
	GetRange(ctx context.Context, ownerID uuid.UUID, from, to time.Time) (service.PlanRange, error)
	SetEntry(ctx context.Context, ownerID uuid.UUID, date time.Time, slot string, in service.SetPlanEntryInput) (service.PlanEntry, error)
	DeleteEntry(ctx context.Context, ownerID uuid.UUID, date time.Time, slot string) error
}

func (s *server) GetPlan(w http.ResponseWriter, r *http.Request, params api.GetPlanParams) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	rng, err := s.plan.GetRange(r.Context(), userID, params.From.Time, params.To.Time)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIPlanRange(rng))
}

func (s *server) SetPlanEntry(w http.ResponseWriter, r *http.Request, date openapi_types.Date, slot api.Slot) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req api.SetPlanEntryRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	portion := 1.0
	if req.Portion != nil {
		portion = *req.Portion
	}
	entry, err := s.plan.SetEntry(r.Context(), userID, date.Time, string(slot), service.SetPlanEntryInput{MealID: req.MealId, Portion: portion})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toAPIPlanEntry(entry))
}

func (s *server) DeletePlanEntry(w http.ResponseWriter, r *http.Request, date openapi_types.Date, slot api.Slot) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	if err := s.plan.DeleteEntry(r.Context(), userID, date.Time, string(slot)); err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toAPIPlanEntry(e service.PlanEntry) api.PlanEntry {
	var fromTemplate openapi_types.UUID
	hasFromTemplate := e.FromTemplateID != nil
	if hasFromTemplate {
		fromTemplate = *e.FromTemplateID
	}
	entry := api.PlanEntry{
		Id: e.ID, Date: openapi_types.Date{Time: e.Date}, Slot: api.Slot(e.Slot),
		MealId: e.MealID, MealName: e.MealName, Portion: e.Portion,
		CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
	if hasFromTemplate {
		entry.FromTemplateId = &fromTemplate
	}
	return entry
}

func toAPIDailyTotal(d service.DailyTotal) api.DailyTotal {
	entries := make([]api.PlanEntry, len(d.Entries))
	for i, e := range d.Entries {
		entries[i] = toAPIPlanEntry(e)
	}
	return api.DailyTotal{Date: openapi_types.Date{Time: d.Date}, Entries: entries, NutritionPerDay: nutrientsToAPI(d.NutritionPerDay)}
}

func toAPIPlanRange(r service.PlanRange) api.PlanRange {
	days := make([]api.DailyTotal, len(r.Days))
	for i, d := range r.Days {
		days[i] = toAPIDailyTotal(d)
	}
	return api.PlanRange{
		From: openapi_types.Date{Time: r.From}, To: openapi_types.Date{Time: r.To}, Days: days,
		Targets: api.Targets{
			TargetKcal: r.Targets.Kcal, TargetProteinG: r.Targets.ProteinG,
			TargetCarbsG: r.Targets.CarbsG, TargetFatG: r.Targets.FatG,
		},
	}
}
```

Add `openapi_types "github.com/oapi-codegen/runtime/types"` to the imports (the same alias `httpapi/meals.go` uses for `openapi_types.UUID`).

**The same type-inference caveat from Task 7 applies here, for every `date`-typed field and path parameter** (`params.From`/`params.To` in `GetPlanParams`, the `date` path parameter in `SetPlanEntry`/`DeletePlanEntry`, `PlanEntry.Date`/`DailyTotal.Date`/`PlanRange.From`/`PlanRange.To`): this plan assumes oapi-codegen's default `openapi_types.Date` for `format: date`, giving path parameters the signature `(w, r, date openapi_types.Date, slot api.Slot)` shown above. If Step 3's `make generate` produces different types, adjust this step's signatures and field accesses to match — nothing else in Task 8 depends on the exact type.

- [ ] **Step 7: Wire the new service into `server` and the router**

In `backend/internal/httpapi/server.go`, add a `plan PlanService` field to the `server` struct.

In `backend/internal/httpapi/router.go`, add `Plan PlanService` to `Deps`, add it to the nil-check panic guard (`d.Plan == nil`) and its message, and add `plan: d.Plan` to the `srv := &server{...}` literal.

- [ ] **Step 8: Build**

Run: `cd backend && go build ./...`
Expected: builds cleanly.

Run: `cd backend && go vet ./...`
Expected: clean.

- [ ] **Step 9: Commit**

```bash
git add openapi.yaml backend/internal/api/api.gen.go backend/internal/httpapi/plan.go \
  backend/internal/httpapi/problem.go backend/internal/httpapi/account.go \
  backend/internal/httpapi/server.go backend/internal/httpapi/router.go
git commit -m "feat(api): add the plan endpoints and their handlers"
```

---

### Task 9: Wire `cmd/api` and write end-to-end tests

**Files:**
- Modify: `backend/cmd/api/main.go`
- Modify: `backend/internal/httpapi/contract_test.go`
- Create: `backend/internal/httpapi/diet_templates_flow_test.go`
- Create: `backend/internal/httpapi/plan_flow_test.go`

**Interfaces:**
- Consumes: everything from Tasks 5 to 8, plus the existing `newTestRouter`, `contract`, `decodeAs[T]`, `withBody`, `withBearer` helpers already in the `httpapi_test` package.

- [ ] **Step 1: Wire the services in `cmd/api/main.go`**

Next to `meals := service.NewMeals(st)`, add:

```go
	dietTemplates := service.NewDietTemplates(st)
	plan := service.NewPlan(st, meals)
```

Add `DietTemplates: dietTemplates,` and `Plan: plan,` to the `httpapi.Deps{...}` literal, alongside `Meals: meals,`.

- [ ] **Step 2: Add stubs to the shared test router**

In `backend/internal/httpapi/contract_test.go`, add two no-op stubs next to `stubMeals` and wire them into `newTestRouter`'s default `Deps`:

```go
// stubDietTemplates panics on any call, so tests that must not reach the
// diet templates service fail loudly if they do.
type stubDietTemplates struct{ httpapi.DietTemplatesService }

// stubPlan panics on any call, so tests that must not reach the plan
// service fail loudly if they do.
type stubPlan struct{ httpapi.PlanService }
```

In `newTestRouter`, add `DietTemplates: stubDietTemplates{}, Plan: stubPlan{},` to the `httpapi.Deps{...}` literal (alongside `Meals: stubMeals{}`). Also add `d.DietTemplates == nil`/`"no diet templates"` and `d.Plan == nil`/`"no plan"` cases to `TestNewRouterPanicsWithoutRequiredDependencies`'s `tests` map and its `full` literal, mirroring the existing `"no meals"` case exactly.

- [ ] **Step 3: Write the diet-templates end-to-end test**

Create `backend/internal/httpapi/diet_templates_flow_test.go`, modeled on `meals_flow_test.go`'s `newMealsRouter`/`TestMealsLifecycle`:

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

func newDietTemplatesRouter(t *testing.T) (router http.Handler, token1, token2 string) {
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
	router = newTestRouter(t, func(d *httpapi.Deps) {
		d.Ingredients = service.NewIngredients(st)
		d.Meals = meals
		d.DietTemplates = service.NewDietTemplates(st)
		d.Plan = service.NewPlan(st, meals)
		d.Tokens = stubTwoUserTokens{u1: u1.ID, u2: u2.ID}
		d.Limits = httpapi.RateLimits{AuthPerMinute: 1000, UserPerMinute: 1000}
	})
	return router, "user1-token", "user2-token"
}

func TestDietTemplatesLifecycle(t *testing.T) {
	router, token1, token2 := newDietTemplatesRouter(t)

	rec := contract(t, router, http.MethodPost, "/ingredients", withBearer(token1),
		withBody(`{"name":"Rice","category":"grains_bread","nutrients":{"calories":130}}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create ingredient: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rice := decodeAs[api.Ingredient](t, rec)

	rec = contract(t, router, http.MethodPost, "/meals", withBearer(token1), withBody(`{"name":"Rice Bowl","servings":1}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create meal: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	meal := decodeAs[api.Meal](t, rec)
	rec = contract(t, router, http.MethodPut, "/meals/"+meal.Id.String()+"/ingredients", withBearer(token1),
		withBody(`{"items":[{"ingredient_id":"`+rice.Id.String()+`","quantity":200,"unit":"g"}]}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("replace meal ingredients: status = %d, body = %s", rec.Code, rec.Body.String())
	}

	rec = contract(t, router, http.MethodPost, "/diet-templates", withBearer(token1), withBody(`{"name":"One Week","day_count":7}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create diet template: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	tpl := decodeAs[api.DietTemplate](t, rec)
	if len(tpl.Slots) != 0 {
		t.Errorf("a freshly created template has slots = %+v, want none", tpl.Slots)
	}

	rec = contract(t, router, http.MethodPut, "/diet-templates/"+tpl.Id.String()+"/slots", withBearer(token1),
		withBody(`{"items":[{"day_index":0,"slot":"breakfast","meal_id":"`+meal.Id.String()+`"}]}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("replace template slots: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	tpl = decodeAs[api.DietTemplate](t, rec)
	if len(tpl.Slots) != 1 || tpl.Slots[0].Portion != 1 {
		t.Fatalf("template slots after replace = %+v, want one slot at portion 1 (the omitted portion defaulted)", tpl.Slots)
	}

	rec = contract(t, router, http.MethodGet, "/diet-templates/"+tpl.Id.String(), withBearer(token2))
	if rec.Code != http.StatusNotFound {
		t.Errorf("get another user's template: status = %d, want 404", rec.Code)
	}

	rec = contract(t, router, http.MethodPost, "/diet-templates/"+tpl.Id.String()+"/apply", withBearer(token1),
		withBody(`{"start_date":"2026-05-04"}`))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("apply template: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rec = contract(t, router, http.MethodPost, "/diet-templates/"+tpl.Id.String()+"/apply", withBearer(token1),
		withBody(`{"start_date":"2026-05-04"}`))
	if rec.Code != http.StatusConflict {
		t.Errorf("re-apply without overwrite: status = %d, want 409", rec.Code)
	}

	rec = contract(t, router, http.MethodPost, "/diet-templates/"+tpl.Id.String()+"/copy", withBearer(token1))
	if rec.Code != http.StatusCreated {
		t.Fatalf("copy template: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	cp := decodeAs[api.DietTemplate](t, rec)
	if cp.SharedWithPartner {
		t.Error("copy has shared_with_partner = true, want false")
	}

	rec = contract(t, router, http.MethodDelete, "/meals/"+meal.Id.String(), withBearer(token1))
	if rec.Code != http.StatusConflict {
		t.Errorf("delete a meal still scheduled in a template/plan: status = %d, want 409", rec.Code)
	}

	rec = contract(t, router, http.MethodDelete, "/diet-templates/"+tpl.Id.String(), withBearer(token1))
	if rec.Code != http.StatusNoContent {
		t.Errorf("delete template: status = %d, want 204", rec.Code)
	}
}
```

- [ ] **Step 4: Write the plan end-to-end test**

Create `backend/internal/httpapi/plan_flow_test.go`, reusing `newDietTemplatesRouter` (it already wires `Plan`):

```go
package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/InzKazik/mealplanner/backend/internal/api"
)

func TestPlanLifecycle(t *testing.T) {
	router, token1, token2 := newDietTemplatesRouter(t)

	rec := contract(t, router, http.MethodPost, "/ingredients", withBearer(token1),
		withBody(`{"name":"Oats","category":"grains_bread","nutrients":{"calories":389}}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create ingredient: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	oats := decodeAs[api.Ingredient](t, rec)

	rec = contract(t, router, http.MethodPost, "/meals", withBearer(token1), withBody(`{"name":"Oatmeal","servings":1}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create meal: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	meal := decodeAs[api.Meal](t, rec)
	rec = contract(t, router, http.MethodPut, "/meals/"+meal.Id.String()+"/ingredients", withBearer(token1),
		withBody(`{"items":[{"ingredient_id":"`+oats.Id.String()+`","quantity":50,"unit":"g"}]}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("replace meal ingredients: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	// 50g oats at 389 kcal/100g: 194.5 kcal per serving.

	rec = contract(t, router, http.MethodPut, "/plan/2026-05-04/breakfast", withBearer(token1),
		withBody(`{"meal_id":"`+meal.Id.String()+`"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("set plan entry: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	entry := decodeAs[api.PlanEntry](t, rec)
	if entry.FromTemplateId != nil {
		t.Error("a manually set entry has from_template_id set, want nil")
	}

	rec = contract(t, router, http.MethodGet, "/plan?from=2026-05-04&to=2026-05-04", withBearer(token1))
	if rec.Code != http.StatusOK {
		t.Fatalf("get plan: status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rng := decodeAs[api.PlanRange](t, rec)
	if len(rng.Days) != 1 || len(rng.Days[0].Entries) != 1 {
		t.Fatalf("plan range = %+v, want one day with one entry", rng)
	}
	if !rng.Days[0].NutritionPerDay.Calories.IsSpecified() || rng.Days[0].NutritionPerDay.Calories.MustGet() != 194.5 {
		t.Errorf("day calories = %+v, want 194.5", rng.Days[0].NutritionPerDay.Calories)
	}

	rec = contract(t, router, http.MethodGet, "/plan?from=2026-05-04&to=2026-05-04", withBearer(token2))
	if rec.Code != http.StatusOK {
		t.Fatalf("get plan (other user): status = %d, body = %s", rec.Code, rec.Body.String())
	}
	rng2 := decodeAs[api.PlanRange](t, rec)
	if len(rng2.Days) != 1 || len(rng2.Days[0].Entries) != 0 {
		t.Errorf("other user's plan range = %+v, want the same date with no entries (isolated)", rng2)
	}

	rec = contract(t, router, http.MethodDelete, "/plan/2026-05-04/breakfast", withBearer(token1))
	if rec.Code != http.StatusNoContent {
		t.Errorf("delete plan entry: status = %d, want 204", rec.Code)
	}
	rec = contract(t, router, http.MethodDelete, "/plan/2026-05-04/breakfast", withBearer(token1))
	if rec.Code != http.StatusNotFound {
		t.Errorf("delete plan entry again: status = %d, want 404", rec.Code)
	}
}
```

- [ ] **Step 5: Run the tests**

Run: `cd backend && go test ./... -v`
Expected: PASS, all packages, including both new e2e tests and `TestNewRouterPanicsWithoutRequiredDependencies`'s two new cases.

Run: `cd backend && go build ./cmd/api`
Expected: builds cleanly.

- [ ] **Step 6: Commit**

```bash
git add backend/cmd/api/main.go backend/internal/httpapi/contract_test.go \
  backend/internal/httpapi/diet_templates_flow_test.go backend/internal/httpapi/plan_flow_test.go
git commit -m "feat(backend): wire the diet templates and plan services into cmd/api and add end-to-end coverage"
```

---

### Task 10: Documentation and final checks

**Files:**
- Modify: `backend/CLAUDE.md`

**Interfaces:** none (documentation only).

- [ ] **Step 1: Update `backend/CLAUDE.md`**

In "Behaviour worth knowing", add three entries after the "Editing an ingredient can retroactively break a meal that references it" line:

```
- **`plan_entries` and `template_slots` allow multiple `snack` rows per day; the other three slots don't.** `PUT/DELETE /plan/{date}/{slot}` can only address a slot by its enum value, so `snack` can't be a true upsert the way `breakfast`/`lunch`/`dinner` are: `PUT .../snack` always adds a new entry, and `DELETE .../snack` removes every snack entry for that date. A manual `PUT` (any slot) always clears `from_template_id` to `null`, even when it overwrites an entry that came from applying a template.
- **`diet_templates.day_count` is immutable after creation.** There is no PATCH field for it; recreate or `POST /diet-templates/{id}/copy` a template to change it. `PUT /diet-templates/{id}/slots` validates every slot's `day_index` against the existing `day_count` at write time — there is no database constraint for this (a `CHECK` can't compare against another table's column).
- **Deleting a meal fails with `409 meal_in_use` if a diet template's slot or a plan entry still references it**, the same `NO ACTION` foreign-key pattern as `ingredient_in_use`. `Auth.DeleteUser` deletes `plan_entries`, then `diet_templates` (which cascades `template_slots`), then `meals`, then the user row, in that order, in one transaction — see the note on cascades above.
```

Update the existing "Cascades alone are not enough for account deletion" bullet to mention the extended ordering:

```
- **Cascades alone are not enough for account deletion.** `users` cascades to `ingredients`, `meals`, `diet_templates` and `plan_entries`, but `meal_ingredients.ingredient_id` and `template_slots`/`plan_entries.meal_id` are deliberately `NO ACTION` (what makes deleting an in-use ingredient or meal a `409`), and Postgres fires a table's own cascade trigger in an order this codebase does not control. `Auth.DeleteUser` deletes explicitly, in one transaction, in the only order that works: `plan_entries` → `diet_templates` (cascades `template_slots`) → `meals` (cascades `meal_ingredients`) → `users` (cascades `ingredients`, now safe since nothing in `meal_ingredients` still references them). Any future table that both cascades from `users` and is referenced with `NO ACTION` from another user-owned table needs the same treatment, in the right position in this chain.
```

In "Not built yet", replace the line about the domain beyond meals with:

```
- The domain beyond diets and plan: shopping lists, partners (later plans). **Diet templates have no partner visibility yet**, for the same reason meals don't: the `partnerships` table does not exist until the partner plan (backend build order item 6, after shopping lists). `plan_entries` has no sharing concept in the spec at all — it is always owner-only.
```

- [ ] **Step 2: Run everything CI runs**

Run: `make check`
Expected: PASS (lints `openapi.yaml`, vets and tests the backend, runs golangci-lint, fails if generated code is stale).

Run: `make check-generated`
Expected: clean (no diff).

- [ ] **Step 3: Commit**

```bash
git add backend/CLAUDE.md
git commit -m "docs(backend): document diets and plan, close out the deferred meal_in_use guard"
```

---

## Self-Review

- **Spec coverage:** §3.4 (`diet_templates`/`template_slots`/`plan_entries` schema, day_index<day_count, snack multiplicity, apply-with-overwrite, totals from plan_entries "with the same math as meal nutrition") → Tasks 1, 5, 6. §3.6 (sharing rule — diet templates get the same read-only+copy partner treatment as meals, explicitly scoped down to owner-only since `partnerships` doesn't exist, same as the meals plan) → Task 5's owner-only `DietTemplates`, documented in Global Constraints and Task 10. §4.1 (`GET/POST diet-templates`, `GET/PATCH/DELETE diet-templates/{id}`, `PUT .../slots`, `POST .../apply`, `POST .../copy`, `GET plan?from=&to=`, `PUT/DELETE plan/{date}/{slot}`) → Tasks 7–8. §4.2 (cursor pagination for diet-templates' list; RFC 9457 errors throughout) → Task 7 (list mirrors meals/ingredients; `GET /plan` is deliberately NOT paginated, a bounded date range instead, documented in Global Constraints). §6 (nutrition golden test, TDD, ownership/sharing tests) → Task 6's `TestPlanGetRangeComputesDailyTotalsAndPropagatesUnknownNutrients`. The meals plan's explicitly deferred consolidation of the ingredient/meal-id dedup pattern is closed by Task 5's `uniqueUUIDs`, before a third and fourth instance could appear, as that plan's final review recommended. The meals plan's cascade-ordering bug is proactively extended (not rediscovered) by Task 4, because the same `NO ACTION`-referencing-a-user-owned-table shape was recognized while designing this plan's schema.
- **Placeholder scan:** none — every step has real, complete code or SQL. The "note on `req.StartDate`'s type" and its Task 8 counterpart are not placeholders: they are honest, explicit flags that one specific type inference (which the meals plan would have called "scratch-verified" had this plan's author actually run `sqlc generate`/`oapi-codegen` during planning, the way that plan did for `GetIngredientsForUserParams.UserID`) was reasoned from oapi-codegen's documented default behavior rather than empirically confirmed, with a one-line fallback instruction if the assumption is wrong. This is a narrower, more honest claim than an unqualified assertion would be, not a substitute for real code.
- **Type consistency:** `service.DietTemplate`/`TemplateSlot`, `service.PlanEntry`/`DailyTotal`/`PlanRange`, and `httpapi`'s `toAPIDietTemplate`/`toAPIPlanEntry`/`toAPIPlanRange` conversions were checked against the exact struct fields Tasks 5–6 define. `sqlc.TemplateSlot.DayIndex`/`Portion` and `sqlc.PlanEntry.Portion` match `service.TemplateSlot`/`PlanEntry`'s equivalent fields' types exactly (`int32`↔`int` conversions are explicit at every boundary, matching the meals plan's `Position` precedent). `GetMealsForUser`'s `UserID uuid.UUID` (plain, not pointer — `meals.owner_id` is `NOT NULL`, unlike `ingredients.owner_id`) is used identically in `diet_templates.go`, `plan.go`, and Task 4's `Meals.Delete`. `uniqueUUIDs[T any](items []T, get func(T) uuid.UUID) []uuid.UUID` (Task 5) is used with matching signatures at all four call sites: `meals.go`'s `sqlc.MealIngredient`→`IngredientID`, `diet_templates.go`'s `TemplateSlotInput`/`sqlc.TemplateSlot`→`MealID` (two call sites), and `plan.go`'s `sqlc.PlanEntry`→`MealID`.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-09-22-backend-diets-and-plan.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
