-- +goose Up
CREATE INDEX idx_activities_actor ON activities(actor_id, created_at DESC);
CREATE INDEX idx_sessions_user ON sessions(user_id, expires_at);

-- +goose Down
DROP INDEX IF EXISTS idx_activities_actor;
DROP INDEX IF EXISTS idx_sessions_user;
