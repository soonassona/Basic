-- name: CreateMembership :one
INSERT INTO memberships (org_id, user_id, role, invited_by)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetMembership :one
SELECT * FROM memberships
WHERE org_id = $1 AND user_id = $2;

-- name: ListMembershipsForUser :many
SELECT m.*, o.slug AS org_slug, o.name AS org_name
FROM memberships m
JOIN organizations o ON o.id = m.org_id
WHERE m.user_id = $1 AND o.deleted_at IS NULL
ORDER BY m.created_at ASC;

-- name: UpdateMembershipRole :one
UPDATE memberships SET role = $3
WHERE org_id = $1 AND user_id = $2
RETURNING *;

-- name: DeleteMembership :exec
DELETE FROM memberships
WHERE org_id = $1 AND user_id = $2;
