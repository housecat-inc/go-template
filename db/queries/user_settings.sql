-- name: GetUserSettings :one
SELECT * FROM user_settings
WHERE subject = ?;

-- name: UpsertUserSettings :exec
INSERT INTO user_settings (subject, settings)
VALUES (?, ?)
ON CONFLICT(subject) DO UPDATE SET
    settings = excluded.settings,
    updated_at = CURRENT_TIMESTAMP;
