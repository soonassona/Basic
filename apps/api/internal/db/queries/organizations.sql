-- name: CreateOrganization :one
INSERT INTO organizations (slug, name, plan)
VALUES ($1, $2, COALESCE($3, 'free'))
RETURNING *;

-- name: GetOrganizationByID :one
SELECT * FROM organizations
WHERE id = $1 AND deleted_at IS NULL;

-- name: GetOrganizationBySlug :one
SELECT * FROM organizations
WHERE slug = $1 AND deleted_at IS NULL;

-- name: ListOrganizationsForUser :many
SELECT o.*
FROM organizations o
JOIN memberships m ON m.org_id = o.id
WHERE m.user_id = $1 AND o.deleted_at IS NULL
ORDER BY o.created_at ASC;
