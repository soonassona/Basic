-- All image queries are tenant-scoped: org_id is the first parameter and
-- appears in WHERE on every read/write. See ADR-0002.

-- name: CreateImage :one
INSERT INTO images (
    org_id, uploaded_by, storage_key, content_type, byte_size, status, metadata
) VALUES ($1, $2, $3, $4, $5, 'uploading', COALESCE($6, '{}'::jsonb))
RETURNING *;

-- name: FinalizeImage :one
UPDATE images
SET status       = 'ready',
    storage_etag = $3,
    width        = $4,
    height       = $5,
    sha256       = $6,
    updated_at   = now()
WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL
RETURNING *;

-- name: GetImage :one
SELECT * FROM images
WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL;

-- name: ListImages :many
SELECT *
FROM images
WHERE org_id = $1
  AND deleted_at IS NULL
  AND ($2::image_status IS NULL OR status = $2)
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: CountImages :one
SELECT COUNT(*)
FROM images
WHERE org_id = $1
  AND deleted_at IS NULL
  AND ($2::image_status IS NULL OR status = $2);

-- name: SoftDeleteImage :exec
UPDATE images SET deleted_at = now()
WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL;
