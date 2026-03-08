-- +goose Up
ALTER TABLE oauth_tokens ADD COLUMN client_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE oauth_tokens DROP COLUMN client_id;
