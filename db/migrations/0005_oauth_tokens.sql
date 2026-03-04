-- +goose Up

CREATE TABLE IF NOT EXISTS oauth_tokens (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    access_token TEXT NOT NULL,
    expires_at TIMESTAMP,
    level TEXT NOT NULL,
    provider TEXT NOT NULL,
    refresh_token TEXT NOT NULL DEFAULT '',
    scopes TEXT NOT NULL DEFAULT '',
    service TEXT NOT NULL,
    user_id TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX idx_oauth_tokens_user_service_level ON oauth_tokens(user_id, service, level);
CREATE INDEX idx_oauth_tokens_user_id ON oauth_tokens(user_id);

-- +goose Down
DROP TABLE IF EXISTS oauth_tokens;
