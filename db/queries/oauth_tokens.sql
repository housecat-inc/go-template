-- name: UpsertOAuthToken :exec
INSERT INTO oauth_tokens (access_token, client_id, expires_at, level, provider, refresh_token, scopes, service, subject)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(subject, service, level) DO UPDATE SET
    access_token = excluded.access_token,
    client_id = excluded.client_id,
    expires_at = excluded.expires_at,
    refresh_token = excluded.refresh_token,
    scopes = excluded.scopes,
    updated_at = CURRENT_TIMESTAMP;

-- name: GetOAuthToken :one
SELECT * FROM oauth_tokens
WHERE subject = ? AND service = ? AND level = ?;

-- name: ListOAuthTokensBySubject :many
SELECT * FROM oauth_tokens
WHERE subject = ?
ORDER BY service, level;

-- name: DeleteOAuthToken :exec
DELETE FROM oauth_tokens
WHERE subject = ? AND service = ? AND level = ?;

-- name: DeleteOAuthTokensBySubjectAndService :exec
DELETE FROM oauth_tokens
WHERE subject = ? AND service = ?;
