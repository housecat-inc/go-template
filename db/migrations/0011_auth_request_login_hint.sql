-- +goose Up

ALTER TABLE oidc_auth_requests ADD COLUMN login_hint TEXT NOT NULL DEFAULT '';

-- +goose Down

ALTER TABLE oidc_auth_requests DROP COLUMN login_hint;
