-- name: InsertSession :exec
INSERT INTO sessions (id, subject, email, provider, expires_at)
VALUES (?, ?, ?, ?, ?);

-- name: GetSession :one
SELECT * FROM sessions
WHERE id = ? AND expires_at > CURRENT_TIMESTAMP;

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = ?;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at <= CURRENT_TIMESTAMP;

-- name: CountSessionsBySubject :one
SELECT COUNT(*) FROM sessions
WHERE subject = ? AND expires_at > CURRENT_TIMESTAMP;

-- name: GetEmailBySubject :one
SELECT email FROM sessions
WHERE subject = ? AND expires_at > CURRENT_TIMESTAMP
ORDER BY created_at DESC
LIMIT 1;
