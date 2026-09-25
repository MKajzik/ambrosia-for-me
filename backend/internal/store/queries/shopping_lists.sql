-- name: CreateShoppingList :one
INSERT INTO shopping_lists (owner_id, name, shared_with_partner, source_from, source_to)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetShoppingListForUser :one
-- The read predicate (spec §5): the caller's own list, or one the caller's
-- active partner has shared. partner_id is NULL when the caller has no
-- partner, which makes the second branch match nothing.
SELECT * FROM shopping_lists
WHERE id = sqlc.arg('id')
  AND (
    owner_id = sqlc.arg('user_id')
    OR (owner_id = sqlc.narg('partner_id')::uuid AND shared_with_partner)
  );

-- name: TouchShoppingListForUser :one
-- Bumps updated_at (via the shopping_lists_set_updated_at trigger) and, just
-- as importantly, takes the list row's write lock: AddItem uses this instead
-- of a plain SELECT so two concurrent adds cannot read the same
-- NextShoppingItemPosition. See TouchMealForUser in meals.sql for the same
-- pattern in the meals domain. Uses the read predicate, because a partner may
-- add items to a shared list.
UPDATE shopping_lists SET updated_at = now()
WHERE id = sqlc.arg('id')
  AND (
    owner_id = sqlc.arg('user_id')
    OR (owner_id = sqlc.narg('partner_id')::uuid AND shared_with_partner)
  )
RETURNING *;

-- name: ListShoppingListsForUser :many
-- Newest first. The row comparison is the keyset cursor over
-- (created_at DESC, id DESC). shared_only is for GET partner/shopping-lists:
-- user_id is then the partner's id and only what the partner has shared comes
-- back.
SELECT * FROM shopping_lists
WHERE owner_id = sqlc.arg('user_id')
  AND (NOT sqlc.arg('shared_only')::boolean OR shared_with_partner)
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
-- it. The join is the visibility check (the read predicate: the caller's own
-- list, or a list the caller's active partner has shared); only the item row
-- is locked.
SELECT shopping_items.* FROM shopping_items
JOIN shopping_lists ON shopping_lists.id = shopping_items.list_id
WHERE shopping_items.id = sqlc.arg('id')
  AND shopping_items.list_id = sqlc.arg('list_id')
  AND (
    shopping_lists.owner_id = sqlc.arg('user_id')
    OR (shopping_lists.owner_id = sqlc.narg('partner_id')::uuid AND shopping_lists.shared_with_partner)
  )
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
-- item_deleted event. Uses the read predicate: a partner may delete items
-- from a list the owner shared.
DELETE FROM shopping_items
USING shopping_lists
WHERE shopping_items.id = sqlc.arg('id')
  AND shopping_items.list_id = sqlc.arg('list_id')
  AND shopping_lists.id = shopping_items.list_id
  AND (
    shopping_lists.owner_id = sqlc.arg('user_id')
    OR (shopping_lists.owner_id = sqlc.narg('partner_id')::uuid AND shopping_lists.shared_with_partner)
  )
RETURNING shopping_items.id, shopping_items.version;

-- name: DeleteGeneratedShoppingItems :exec
DELETE FROM shopping_items WHERE list_id = sqlc.arg('list_id') AND origin = 'generated';
