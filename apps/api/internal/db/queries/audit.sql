-- name: AppendAudit :exec
INSERT INTO audit_log (
    org_id, actor_id, actor_kind, action, resource, resource_id,
    metadata, request_id, trace_id
) VALUES ($1, $2, $3, $4, $5, $6, COALESCE($7, '{}'::jsonb), $8, $9);
