-- All job queries are tenant-scoped (org_id appears in every WHERE).
-- The 10-minute dedup window from spec §11 is applied as a `created_at`
-- predicate parameterised by the caller, not hard-coded here, so the
-- use-case owns the policy.

-- name: CreateJob :one
INSERT INTO jobs (
    id, org_id, image_id, submitted_by, type, state, payload, dedup_key, attempt
) VALUES (
    $1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9
)
RETURNING id, org_id, image_id, submitted_by, type, state, payload,
          dedup_key, attempt, error, result, started_at, finished_at,
          created_at, updated_at;

-- name: FindActiveJobByDedupKey :one
SELECT id, org_id, image_id, submitted_by, type, state, payload,
       dedup_key, attempt, error, result, started_at, finished_at,
       created_at, updated_at
FROM jobs
WHERE org_id = $1
  AND dedup_key = $2
  AND state IN ('pending', 'running', 'succeeded')
  AND created_at >= $3
ORDER BY created_at DESC
LIMIT 1;

-- name: GetJob :one
SELECT id, org_id, image_id, submitted_by, type, state, payload,
       dedup_key, attempt, error, result, started_at, finished_at,
       created_at, updated_at
FROM jobs
WHERE id = $1 AND org_id = $2;

-- name: MarkJobErrored :exec
UPDATE jobs
SET state       = 'failed',
    error       = $3,
    finished_at = now(),
    updated_at  = now()
WHERE id = $1 AND org_id = $2;

-- name: ApplyJobCallback :one
-- Worker callback: identified by job id only (the endpoint is HMAC-
-- authenticated; the org is read out of the existing row via RETURNING
-- and used for downstream audit/SSE fan-out). Idempotent guard: refuse
-- to overwrite an already-terminal state.
--
-- Note on the explicit ::job_state casts on every state reference:
-- without them, Postgres deduces the state parameter as job_state from
-- the SET assignment and as text from the CASE comparisons against
-- literals, then errors with "inconsistent types deduced for parameter
-- $N" (SQLSTATE 42P08). Casting at every reference forces a single
-- inferred type for the parameter and lets Postgres compare enum values
-- to literals via implicit upcast.
UPDATE jobs
SET state       = sqlc.arg(state)::job_state,
    attempt     = sqlc.arg(attempt),
    error       = sqlc.arg(error),
    result      = sqlc.arg(result)::jsonb,
    started_at  = COALESCE(started_at, CASE WHEN sqlc.arg(state)::job_state = 'running' THEN now() ELSE NULL END),
    finished_at = CASE WHEN sqlc.arg(state)::job_state IN ('succeeded', 'failed', 'cancelled') THEN now() ELSE finished_at END,
    updated_at  = now()
WHERE id = sqlc.arg(id)
  AND state NOT IN ('succeeded', 'failed', 'cancelled')
RETURNING id, org_id, image_id, submitted_by, type, state, payload,
          dedup_key, attempt, error, result, started_at, finished_at,
          created_at, updated_at;

-- (jobs queries continue below — annotation queries live in annotations.sql)

-- name: GetJobByID :one
-- Worker-side helper for the callback handler — same as GetJob but
-- without the org_id filter (HMAC has already authenticated). External
-- handlers MUST keep using GetJob so cross-tenant reads stay 404.
SELECT id, org_id, image_id, submitted_by, type, state, payload,
       dedup_key, attempt, error, result, started_at, finished_at,
       created_at, updated_at
FROM jobs
WHERE id = $1;
