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
