-- name: UpsertOAuthToken :exec
INSERT INTO oauth_tokens (access_token, client_id, expires_at, level, provider, refresh_token, scopes, service, user_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(user_id, service, level) DO UPDATE SET
    access_token = excluded.access_token,
    client_id = excluded.client_id,
    expires_at = excluded.expires_at,
    refresh_token = excluded.refresh_token,
    scopes = excluded.scopes,
    updated_at = CURRENT_TIMESTAMP;

-- name: GetOAuthToken :one
SELECT * FROM oauth_tokens
WHERE user_id = ? AND service = ? AND level = ?;

-- name: ListOAuthTokensByUser :many
SELECT * FROM oauth_tokens
WHERE user_id = ?
ORDER BY service, level;

-- name: DeleteOAuthToken :exec
DELETE FROM oauth_tokens
WHERE user_id = ? AND service = ? AND level = ?;

-- name: DeleteOAuthTokensByUserAndService :exec
DELETE FROM oauth_tokens
WHERE user_id = ? AND service = ?;
