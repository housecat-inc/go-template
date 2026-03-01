-- +goose Up
ALTER TABLE sessions ADD COLUMN provider TEXT NOT NULL DEFAULT 'exe.dev';

CREATE INDEX IF NOT EXISTS idx_sessions_user_id_expires_at
    ON sessions (user_id, expires_at);

CREATE INDEX idx_activities_actor_created_at ON activities(actor_id, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_activities_actor_created_at;
DROP INDEX IF EXISTS idx_sessions_user_id_expires_at;
