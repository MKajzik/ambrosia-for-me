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
