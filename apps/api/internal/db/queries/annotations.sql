-- Annotation queries. Optimistic locking lives on annotation_sets.version
-- (spec §10). PatchAnnotation runs inside a tx with these three queries
-- so the version check + bump + row update are atomic.

-- name: GetAnnotationSetByImage :one
SELECT id, org_id, image_id, version, notes, created_by, created_at, updated_at
FROM annotation_sets
WHERE image_id = $1 AND org_id = $2;

-- name: GetAnnotationSetByID :one
-- Read by primary key. Used by CreateAnnotation to distinguish "set not
-- found in this org" (404) from "stale If-Match" (409) on a no-rows
-- bump-version response.
SELECT id, org_id, image_id, version, notes, created_by, created_at, updated_at
FROM annotation_sets
WHERE id = $1 AND org_id = $2;

-- name: EnsureAnnotationSet :one
-- Idempotent insert. ON CONFLICT (org_id, image_id) DO UPDATE bumps
-- updated_at to a no-op so RETURNING always yields a row — that lets
-- callers fetch the canonical id without a separate read. Called by
-- FinalizeUpload so every ready image has a set ready for the studio.
INSERT INTO annotation_sets (org_id, image_id, created_by)
VALUES ($1, $2, $3)
ON CONFLICT (org_id, image_id) DO UPDATE SET updated_at = annotation_sets.updated_at
RETURNING id, org_id, image_id, version, notes, created_by, created_at, updated_at;

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

-- name: CreateAnnotation :one
-- Inserts a new annotation. Caller is responsible for bumping the parent
-- annotation_set's version inside the same transaction (mirrors
-- ApplyAnnotationPatch's locking flow). Returns the inserted row.
INSERT INTO annotations (
    org_id, annotation_set_id, label_id, kind, geometry, created_by
) VALUES (
    $1, $2, $3, $4, $5::jsonb, $6
)
RETURNING id, org_id, annotation_set_id, label_id, kind, geometry,
          mask_storage_key, ai_score, quality_score, model_used,
          human_accepted, created_by, created_at, updated_at;

-- name: SoftDeleteAnnotation :one
-- Marks an annotation deleted via deleted_at timestamp. Returns the
-- annotation_set_id so the use-case can audit which set was affected
-- without a separate read.
UPDATE annotations
SET deleted_at = now(),
    updated_at = now()
WHERE id = $1 AND org_id = $2 AND deleted_at IS NULL
RETURNING annotation_set_id;

-- name: WriteAIResult :exec
-- Writes AI inference fields onto un-reviewed annotations in a set.
-- Skips rows where human_accepted IS NOT NULL so human corrections are
-- never overwritten by a later AI inference pass.
UPDATE annotations
SET mask_storage_key = $3,
    ai_score         = $4,
    model_used       = $5,
    updated_at       = now()
WHERE annotation_set_id  = $1
  AND org_id             = $2
  AND deleted_at         IS NULL
  AND human_accepted     IS NULL;
