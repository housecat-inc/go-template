-- name: InsertSession :exec
INSERT INTO sessions (id, user_id, email, provider, expires_at)
VALUES (?, ?, ?, ?, ?);

-- name: GetSession :one
SELECT * FROM sessions
WHERE id = ? AND expires_at > CURRENT_TIMESTAMP;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = ?;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at <= CURRENT_TIMESTAMP;

-- name: CountSessionsByUser :one
SELECT COUNT(*) FROM sessions
WHERE user_id = ? AND expires_at > CURRENT_TIMESTAMP;

-- name: GetEmailByUserID :one
SELECT email FROM sessions
WHERE user_id = ? AND expires_at > CURRENT_TIMESTAMP
ORDER BY created_at DESC
LIMIT 1;
