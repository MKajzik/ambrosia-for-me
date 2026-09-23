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
-- Ordered by meal-time position (breakfast, lunch, dinner, snack), not
-- plain text ("slot" would sort dinner before lunch). created_at, then id,
-- is the tiebreak for same-date, same-slot rows (only possible for snack,
-- which allows several per day).
SELECT * FROM plan_entries
WHERE owner_id = sqlc.arg('user_id')
  AND date >= sqlc.arg('from_date')::date AND date <= sqlc.arg('to_date')::date
ORDER BY date, array_position(ARRAY['breakfast', 'lunch', 'dinner', 'snack'], slot), created_at, id;

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
