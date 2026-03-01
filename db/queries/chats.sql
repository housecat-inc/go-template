-- name: CreateChat :one
INSERT INTO chats (owner_id, title)
VALUES (?, ?)
RETURNING *;

-- name: GetChat :one
SELECT * FROM chats WHERE id = ?;

-- name: ListChatsByUser :many
SELECT c.* FROM chats c
JOIN chat_members cm ON cm.chat_id = c.id
WHERE cm.user_id = ?
ORDER BY c.updated_at DESC;

-- name: UpdateChatTitle :exec
UPDATE chats SET title = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?;

-- name: TouchChat :exec
UPDATE chats SET updated_at = CURRENT_TIMESTAMP WHERE id = ?;

-- name: DeleteChat :exec
DELETE FROM chats WHERE id = ?;

-- name: AddChatMember :exec
INSERT OR IGNORE INTO chat_members (chat_id, user_email, user_id)
VALUES (?, ?, ?);

-- name: ListChatMembers :many
SELECT * FROM chat_members WHERE chat_id = ? ORDER BY created_at;

-- name: IsChatMember :one
SELECT COUNT(*) > 0 AS is_member FROM chat_members WHERE chat_id = ? AND user_id = ?;

-- name: CreateMessage :one
INSERT INTO messages (chat_id, content, sender_email, sender_id)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: ListMessages :many
SELECT * FROM messages WHERE chat_id = ? ORDER BY created_at ASC;

-- name: GetLatestMessage :one
SELECT * FROM messages WHERE chat_id = ? ORDER BY created_at DESC LIMIT 1;
