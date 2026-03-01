-- name: InsertActivity :exec
INSERT INTO activities (actor_id, actor_type, action, object_id, object_type, target_id, target_type, metadata)
VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListActivitiesByActor :many
SELECT * FROM activities
WHERE actor_id = ?
ORDER BY created_at DESC
LIMIT ?;

-- name: ListActivitiesByObject :many
SELECT * FROM activities
WHERE object_type = ? AND object_id = ?
ORDER BY created_at DESC
LIMIT ?;

-- name: ListActivitiesByTarget :many
SELECT * FROM activities
WHERE target_type = ? AND target_id = ?
ORDER BY created_at DESC
LIMIT ?;

-- name: CountActivitiesByActor :one
SELECT COUNT(*) FROM activities
WHERE actor_id = ?;
