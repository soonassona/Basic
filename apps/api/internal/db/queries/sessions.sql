-- name: GetSessionByToken :one
-- Better Auth populates this; the API reads it to authenticate requests.
SELECT s.id, s.user_id, s.token, s.expires_at, s.created_at, s.updated_at
FROM better_auth_session s
WHERE s.token = $1 AND s.expires_at > now();

-- name: DeleteExpiredSessions :exec
DELETE FROM better_auth_session WHERE expires_at <= now();
