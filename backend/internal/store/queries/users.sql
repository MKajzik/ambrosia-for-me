-- name: CreateUser :one
INSERT INTO users (email, password_hash, display_name)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: UpdateUserProfile :one
UPDATE users SET
    display_name     = COALESCE(sqlc.narg('display_name'), display_name),
    target_kcal      = CASE WHEN sqlc.arg('set_target_kcal')::boolean THEN sqlc.narg('target_kcal') ELSE target_kcal END,
    target_protein_g = CASE WHEN sqlc.arg('set_target_protein_g')::boolean THEN sqlc.narg('target_protein_g') ELSE target_protein_g END,
    target_carbs_g   = CASE WHEN sqlc.arg('set_target_carbs_g')::boolean THEN sqlc.narg('target_carbs_g') ELSE target_carbs_g END,
    target_fat_g     = CASE WHEN sqlc.arg('set_target_fat_g')::boolean THEN sqlc.narg('target_fat_g') ELSE target_fat_g END
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteUser :execrows
DELETE FROM users WHERE id = $1;
