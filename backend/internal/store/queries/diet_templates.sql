-- name: CreateDietTemplate :one
INSERT INTO diet_templates (owner_id, name, day_count, shared_with_partner)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetDietTemplateForUser :one
-- The read predicate (spec §5): the caller's own template, or one the
-- caller's active partner has shared. partner_id is NULL when the caller has
-- no partner (or wants owner-only access, as Apply does), which makes the
-- second branch match nothing. Every write keeps the strict owner-only
-- queries.
SELECT * FROM diet_templates
WHERE id = sqlc.arg('id')
  AND (
    owner_id = sqlc.arg('user_id')
    OR (owner_id = sqlc.narg('partner_id')::uuid AND shared_with_partner)
  );

-- name: TouchDietTemplateForUser :one
-- Bumps updated_at (via the diet_templates_set_updated_at trigger) and, just
-- as importantly, takes the row's write lock: ReplaceSlots uses this instead
-- of a plain SELECT so two concurrent slot replaces on the same template
-- serialize instead of racing the DELETE+INSERT below it. See ReplaceSlots
-- in internal/service/diet_templates.go, and TouchMealForUser in
-- meals.sql for the same fix in the meals domain.
UPDATE diet_templates SET updated_at = now()
WHERE id = sqlc.arg('id') AND owner_id = sqlc.arg('user_id')
RETURNING *;

-- name: ListDietTemplatesForUser :many
-- shared_only is for GET partner/diet-templates: user_id is then the partner's
-- id and only what the partner has shared comes back.
SELECT * FROM diet_templates
WHERE owner_id = sqlc.arg('user_id')
  AND (NOT sqlc.arg('shared_only')::boolean OR shared_with_partner)
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
-- Ordered by meal-time position (breakfast, lunch, dinner, snack), not
-- plain text ("slot" would sort dinner before lunch). template_slots has no
-- timestamps (see backend/CLAUDE.md's line-item-table exception), so id is
-- the tiebreak for same-day, same-slot rows (only possible for snack, which
-- allows several per day).
SELECT * FROM template_slots
WHERE template_id = sqlc.arg('template_id')
ORDER BY day_index, array_position(ARRAY['breakfast', 'lunch', 'dinner', 'snack'], slot), id;
