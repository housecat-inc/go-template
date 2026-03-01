-- +goose Up
CREATE TABLE IF NOT EXISTS activities (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    actor_id TEXT NOT NULL,
    actor_type TEXT NOT NULL DEFAULT 'user',
    action TEXT NOT NULL,
    object_id TEXT NOT NULL,
    object_type TEXT NOT NULL,
    target_id TEXT,
    target_type TEXT,
    metadata TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_activities_object ON activities(object_type, object_id);
CREATE INDEX idx_activities_target ON activities(target_type, target_id);

-- +goose Down
DROP TABLE IF EXISTS activities;
