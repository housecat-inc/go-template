-- name: InsertChat :exec
INSERT INTO chats (id, title, user_id)
VALUES (?, ?, ?);

-- name: GetChat :one
SELECT * FROM chats
WHERE id = ? AND user_id = ? AND deleted_at IS NULL;

-- name: ListChatsByUser :many
SELECT * FROM chats
WHERE user_id = ? AND deleted_at IS NULL
ORDER BY updated_at DESC
LIMIT ?;

-- name: UpdateChatTitle :exec
UPDATE chats SET title = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: UpdateChatTimestamp :exec
UPDATE chats SET updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: SoftDeleteChat :exec
UPDATE chats SET deleted_at = CURRENT_TIMESTAMP
WHERE id = ? AND user_id = ?;
