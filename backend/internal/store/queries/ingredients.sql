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

-- Deleting an ingredient referenced by a meal_ingredients row fails with a
-- foreign-key violation on meal_ingredients_ingredient_id_fkey, which
-- Ingredients.Delete (internal/service/ingredients.go) translates into
-- ErrIngredientInUse.
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

-- name: GetIngredientsForUser :many
SELECT * FROM ingredients
WHERE id = ANY(sqlc.arg('ids')::uuid[]) AND (owner_id IS NULL OR owner_id = sqlc.arg('user_id'));

-- name: UpsertUSDAIngredient :one
INSERT INTO ingredients (name, category, usda_fdc_id)
VALUES ($1, $2, $3)
ON CONFLICT (usda_fdc_id) DO UPDATE SET name = EXCLUDED.name, category = EXCLUDED.category
RETURNING *;
