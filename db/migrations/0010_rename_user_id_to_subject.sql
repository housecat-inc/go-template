-- +goose Up

ALTER TABLE oauth_tokens RENAME COLUMN user_id TO subject;
ALTER TABLE oidc_auth_requests RENAME COLUMN user_id TO subject;
ALTER TABLE oidc_refresh_tokens RENAME COLUMN user_id TO subject;
ALTER TABLE sessions RENAME COLUMN user_id TO subject;

-- +goose Down

ALTER TABLE oauth_tokens RENAME COLUMN subject TO user_id;
ALTER TABLE oidc_auth_requests RENAME COLUMN subject TO user_id;
ALTER TABLE oidc_refresh_tokens RENAME COLUMN subject TO user_id;
ALTER TABLE sessions RENAME COLUMN subject TO user_id;
