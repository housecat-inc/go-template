-- name: InsertAuthRequest :exec
INSERT INTO oidc_auth_requests (id, client_id, redirect_uri, scopes, state, nonce, response_type, code_challenge, code_challenge_method)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetAuthRequest :one
SELECT * FROM oidc_auth_requests WHERE id = ?;

-- name: CompleteAuthRequest :exec
UPDATE oidc_auth_requests
SET user_id = ?, user_email = ?, auth_time = CURRENT_TIMESTAMP, done = 1
WHERE id = ?;

-- name: DeleteAuthRequest :exec
DELETE FROM oidc_auth_requests WHERE id = ?;

-- name: InsertAuthCode :exec
INSERT INTO oidc_codes (code, auth_request_id) VALUES (?, ?);

-- name: GetAuthRequestByCode :one
SELECT r.* FROM oidc_auth_requests r
JOIN oidc_codes c ON c.auth_request_id = r.id
WHERE c.code = ?;

-- name: DeleteAuthCode :exec
DELETE FROM oidc_codes WHERE code = ?;

-- name: DeleteAuthCodesByRequestID :exec
DELETE FROM oidc_codes WHERE auth_request_id = ?;

-- name: InsertAccessToken :exec
INSERT INTO oidc_access_tokens (id, application_id, subject, audience, scopes, expires_at)
VALUES (?, ?, ?, ?, ?, ?);

-- name: GetAccessToken :one
SELECT * FROM oidc_access_tokens
WHERE id = ? AND expires_at > CURRENT_TIMESTAMP;

-- name: DeleteAccessToken :exec
DELETE FROM oidc_access_tokens WHERE id = ?;

-- name: DeleteAccessTokensBySubject :exec
DELETE FROM oidc_access_tokens WHERE subject = ? AND application_id = ?;

-- name: InsertRefreshToken :exec
INSERT INTO oidc_refresh_tokens (id, token, auth_time, audience, user_id, application_id, scopes, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetRefreshToken :one
SELECT * FROM oidc_refresh_tokens
WHERE token = ? AND expires_at > CURRENT_TIMESTAMP;

-- name: DeleteRefreshToken :exec
DELETE FROM oidc_refresh_tokens WHERE id = ?;

-- name: GetLatestAuthRequestByUserAndClient :one
SELECT * FROM oidc_auth_requests
WHERE user_id = ? AND client_id = ? AND done = 1
ORDER BY created_at DESC
LIMIT 1;
