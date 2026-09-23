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

-- name: IngredientHasUnconvertibleMealUsage :one
-- True when a meal_ingredients row still depends on this ingredient's
-- grams_per_piece (a row with unit = 'piece') or density_g_per_ml (unit =
-- 'ml') for unit conversion, checked only for the field(s) an update would
-- clear. Ingredients.Update uses this to reject the edit at write time
-- (ErrIngredientInUseByUnconvertibleUnit) instead of leaving the meal
-- permanently unreadable. Joined to ingredients and filtered by owner_id so
-- this never reveals anything about an ingredient the caller does not own:
-- Ingredients.Update calls it before the ownership-checked UPDATE below, so
-- without this filter a caller could probe another user's ingredient (or a
-- global one) for meal usage and get a 409 instead of the expected 404.
SELECT EXISTS (
    SELECT 1 FROM meal_ingredients mi
    JOIN ingredients i ON i.id = mi.ingredient_id
    WHERE mi.ingredient_id = sqlc.arg('ingredient_id')
      AND i.owner_id = sqlc.arg('user_id')
      AND (
        (sqlc.arg('check_piece')::boolean AND mi.unit = 'piece')
        OR (sqlc.arg('check_ml')::boolean AND mi.unit = 'ml')
      )
) AS in_use;

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
