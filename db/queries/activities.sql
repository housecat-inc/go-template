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

-- name: ListActivitiesByObjectType :many
SELECT * FROM activities
WHERE object_type = ?
ORDER BY created_at DESC
LIMIT ?;

-- name: ListVMCreators :many
SELECT a.object_id, a.created_at, a.metadata FROM activities a
INNER JOIN (
    SELECT object_id, MIN(id) AS min_id FROM activities
    WHERE action = 'created_vm' AND object_type = 'vm'
    GROUP BY object_id
) b ON a.id = b.min_id;

-- name: ListActivitiesByActorAndObjectType :many
SELECT * FROM activities
WHERE actor_id = ? AND object_type = ?
ORDER BY created_at DESC
LIMIT ?;
