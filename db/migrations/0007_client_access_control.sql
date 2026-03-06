-- +goose Up
ALTER TABLE oidc_clients ADD COLUMN allowed_domain TEXT NOT NULL DEFAULT 'housecat.com';
ALTER TABLE oidc_clients ADD COLUMN allowed_emails TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE oidc_clients DROP COLUMN allowed_domain;
ALTER TABLE oidc_clients DROP COLUMN allowed_emails;
