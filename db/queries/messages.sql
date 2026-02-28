-- name: InsertMessage :exec
INSERT INTO messages (chat_id, content, role)
VALUES (?, ?, ?);

-- name: ListMessagesByChat :many
SELECT * FROM messages
WHERE chat_id = ?
ORDER BY created_at ASC;
