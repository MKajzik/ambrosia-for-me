-- name: GetActivePartnerID :one
-- The other side of the user's active partnership. store.IsNotFound on the
-- error means the user has no active partner.
SELECT (CASE WHEN user_a = sqlc.arg('user_id') THEN user_b ELSE user_a END)::uuid AS partner_id
FROM partnerships
WHERE status = 'active' AND (user_a = sqlc.arg('user_id') OR user_b = sqlc.arg('user_id'));

-- name: GetActivePartnerIDForShare :one
-- GetActivePartnerID plus a shared lock on the partnership row. Shopping-item
-- writes use it: Partners.Unlink's DELETE waits for the edits in flight, and an
-- edit that starts afterwards finds no partner (spec §6, unlink ordering).
SELECT (CASE WHEN user_a = sqlc.arg('user_id') THEN user_b ELSE user_a END)::uuid AS partner_id
FROM partnerships
WHERE status = 'active' AND (user_a = sqlc.arg('user_id') OR user_b = sqlc.arg('user_id'))
FOR SHARE;

-- name: LockUsersForUpdate :many
-- Locks the given users rows in id order, so two transactions that lock the
-- same pair cannot deadlock. FOR NO KEY UPDATE conflicts with itself but not
-- with the FOR KEY SHARE that inserts of dependent rows take, so it does not
-- block a user's other writes. Partners.Invite and Partners.Accept use it to
-- serialize their check-then-write over one user.
SELECT id FROM users
WHERE id = ANY(sqlc.arg('ids')::uuid[])
ORDER BY id
FOR NO KEY UPDATE;

-- name: GetPartnershipForUser :one
-- The user's partnership row (pending or active) with the other side's display
-- name, which is NULL while the invite is still pending. A user has one row;
-- if that invariant is ever violated, the active one wins.
SELECT partnerships.id, partnerships.status, partnerships.invite_expires_at, partnerships.updated_at,
       other.display_name AS partner_display_name
FROM partnerships
LEFT JOIN users AS other
       ON other.id = CASE WHEN partnerships.user_a = sqlc.arg('user_id') THEN partnerships.user_b ELSE partnerships.user_a END
WHERE partnerships.user_a = sqlc.arg('user_id') OR partnerships.user_b = sqlc.arg('user_id')
ORDER BY (partnerships.status = 'active') DESC
LIMIT 1;

-- name: GetPendingPartnershipByCodeHash :one
SELECT * FROM partnerships
WHERE status = 'pending' AND invite_code_hash = sqlc.arg('invite_code_hash');

-- name: DeletePendingPartnershipForUser :exec
DELETE FROM partnerships WHERE user_a = sqlc.arg('user_id') AND status = 'pending';

-- name: CreatePartnerInvite :one
INSERT INTO partnerships (user_a, status, invite_code_hash, invite_expires_at, created_by)
VALUES (sqlc.arg('user_id'), 'pending', sqlc.arg('invite_code_hash'), sqlc.arg('invite_expires_at'), sqlc.arg('user_id'))
RETURNING *;

-- name: ActivatePartnership :one
-- Turns a pending invite into an active partnership and clears the code, so a
-- spent code cannot be replayed. No row means the invite is gone (cancelled,
-- replaced or accepted a moment ago).
UPDATE partnerships SET
    user_b            = sqlc.arg('user_b'),
    status            = 'active',
    invite_code_hash  = NULL,
    invite_expires_at = NULL
WHERE id = sqlc.arg('id') AND status = 'pending'
RETURNING *;

-- name: DeletePartnershipsForUser :many
-- Ends the user's active partnership or cancels their pending invite. Returns
-- what was deleted so Partners.Unlink can close the partner's event streams.
DELETE FROM partnerships
WHERE user_a = sqlc.arg('user_id') OR user_b = sqlc.arg('user_id')
RETURNING id, user_a, user_b, status;
