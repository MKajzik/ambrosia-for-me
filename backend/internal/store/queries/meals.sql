-- name: CreateMeal :one
INSERT INTO meals (owner_id, name, notes, servings, shared_with_partner)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetMealForUser :one
-- The read predicate (spec §5): the caller's own meal, or one the caller's
-- active partner has shared. partner_id is NULL when the caller has no
-- partner (or when the caller wants owner-only access), which makes the second
-- branch match nothing. Every write keeps the strict owner-only queries.
SELECT * FROM meals
WHERE id = sqlc.arg('id')
  AND (
    owner_id = sqlc.arg('user_id')
    OR (owner_id = sqlc.narg('partner_id')::uuid AND shared_with_partner)
  );

-- name: TouchMealForUser :one
-- Bumps updated_at (via the meals_set_updated_at trigger) and, just as
-- importantly, takes the row's write lock: ReplaceIngredients uses this
-- instead of a plain SELECT so two concurrent replaces on the same meal
-- serialize instead of racing the DELETE+INSERT below it. See
-- ReplaceIngredients in internal/service/meals.go.
UPDATE meals SET updated_at = now()
WHERE id = sqlc.arg('id') AND owner_id = sqlc.arg('user_id')
RETURNING *;

-- name: ListMealsForUser :many
-- shared_only is for GET partner/meals: user_id is then the partner's id and
-- only what the partner has shared comes back.
SELECT * FROM meals
WHERE owner_id = sqlc.arg('user_id')
  AND (NOT sqlc.arg('shared_only')::boolean OR shared_with_partner)
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

-- name: DeleteMealsForUser :exec
-- Used by account deletion, before the users row itself goes. users cascades
-- to both ingredients and meals, but meal_ingredients.ingredient_id is NO
-- ACTION (that is what makes deleting an in-use ingredient a 409), and
-- Postgres fires the ingredients cascade first. Clearing the owner's meals up
-- front removes the meal_ingredients rows (ON DELETE CASCADE on meal_id) that
-- would otherwise block that cascade.
DELETE FROM meals WHERE owner_id = sqlc.arg('user_id');

-- name: ReplaceMealIngredients :exec
DELETE FROM meal_ingredients WHERE meal_id = $1;

-- name: InsertMealIngredient :one
INSERT INTO meal_ingredients (meal_id, ingredient_id, quantity, unit, position)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetMealIngredients :many
SELECT * FROM meal_ingredients WHERE meal_id = sqlc.arg('meal_id') ORDER BY position;

-- name: GetMealsForUser :many
-- The batch counterpart to GetMealForUser: given a set of meal ids, returns
-- only the ones that exist and are owned by user_id. Used by DietTemplates
-- and Plan (Tasks 5, 6) to validate a slot's or entry's meal_id, and by
-- DietTemplates.toTemplate to fetch each slot's meal name in one query.
SELECT * FROM meals
WHERE id = ANY(sqlc.arg('ids')::uuid[]) AND owner_id = sqlc.arg('user_id');

-- name: GetMealIngredientsForMeals :many
-- The batch counterpart to GetMealIngredients, for ShoppingLists.Generate:
-- every ingredient line of every meal scheduled in a date range, in one query.
SELECT * FROM meal_ingredients
WHERE meal_id = ANY(sqlc.arg('meal_ids')::uuid[])
ORDER BY meal_id, position;
