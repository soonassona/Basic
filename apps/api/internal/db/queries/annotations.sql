-- Annotation queries. Optimistic locking lives on annotation_sets.version
-- (spec §10). PatchAnnotation runs inside a tx with these three queries
-- so the version check + bump + row update are atomic.

-- name: GetAnnotationSetByImage :one
SELECT id, org_id, image_id, version, notes, created_by, created_at, updated_at
FROM annotation_sets
WHERE image_id = $1 AND org_id = $2;

-- name: ListAnnotationsBySet :many
SELECT id, org_id, annotation_set_id, label_id, kind, geometry,
       mask_storage_key, ai_score, quality_score, model_used,
       human_accepted, created_by, created_at, updated_at
FROM annotations
WHERE annotation_set_id = $1 AND org_id = $2 AND deleted_at IS NULL
ORDER BY created_at ASC;

-- name: GetAnnotationWithSetVersion :one
SELECT a.id, a.org_id, a.annotation_set_id, a.label_id, a.kind,
       a.geometry, a.mask_storage_key, a.ai_score, a.quality_score,
       a.model_used, a.human_accepted, a.created_by, a.created_at, a.updated_at,
       s.version AS set_version
FROM annotations a
JOIN annotation_sets s ON s.id = a.annotation_set_id
WHERE a.id = $1 AND a.org_id = $2 AND a.deleted_at IS NULL;

-- name: BumpAnnotationSetVersion :one
-- Returns the new version on success, or no rows on If-Match mismatch.
UPDATE annotation_sets
SET version    = version + 1,
    updated_at = now()
WHERE id = $1 AND org_id = $2 AND version = $3
RETURNING version;

-- name: ApplyAnnotationPatch :one
-- COALESCE-on-NULL pattern: nil pointer from Go arrives as SQL NULL,
-- which leaves the column unchanged. Geometry is jsonb so we cast.
UPDATE annotations
SET geometry       = COALESCE($3::jsonb, geometry),
    label_id       = COALESCE($4, label_id),
    human_accepted = COALESCE($5, human_accepted),
    updated_at     = now()
WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL
RETURNING id, org_id, annotation_set_id, label_id, kind, geometry,
          mask_storage_key, ai_score, quality_score, model_used,
          human_accepted, created_by, created_at, updated_at;
