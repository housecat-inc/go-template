-- name: InsertChat :exec
INSERT INTO chats (id, title, user_id)
VALUES (?, ?, ?);

-- name: GetChat :one
SELECT * FROM chats
WHERE id = ? AND user_id = ?;

-- name: ListChatsByUser :many
SELECT * FROM chats
WHERE user_id = ?
ORDER BY updated_at DESC
LIMIT ?;

-- name: UpdateChatTitle :exec
UPDATE chats SET title = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: UpdateChatTimestamp :exec
UPDATE chats SET updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: DeleteChat :exec
DELETE FROM chats WHERE id = ? AND user_id = ?;
