-- name: CreateUser :one
INSERT INTO users (id, email, email_verified, display_name, avatar_url, locale)
VALUES ($1, $2, COALESCE($3, FALSE), $4, $5, COALESCE($6, 'en'))
RETURNING *;

-- name: GetUserByID :one
SELECT * FROM users
WHERE id = $1 AND deleted_at IS NULL;

-- name: GetUserByEmail :one
SELECT * FROM users
WHERE LOWER(email) = LOWER($1) AND deleted_at IS NULL;

-- name: SoftDeleteUser :exec
UPDATE users SET deleted_at = now() WHERE id = $1;
