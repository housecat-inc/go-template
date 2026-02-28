-- name: InsertActivity :exec
INSERT INTO activities (actor_id, actor_type, action, metadata)
VALUES (?, ?, ?, ?);

-- name: ListActivitiesByActor :many
SELECT * FROM activities
WHERE actor_id = ?
ORDER BY created_at DESC
LIMIT ?;
