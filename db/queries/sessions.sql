-- name: InsertSession :exec
INSERT INTO sessions (id, user_id, email, expires_at)
VALUES (?, ?, ?, ?);

-- name: GetSession :one
SELECT * FROM sessions
WHERE id = ? AND expires_at > CURRENT_TIMESTAMP;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = ?;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at <= CURRENT_TIMESTAMP;
