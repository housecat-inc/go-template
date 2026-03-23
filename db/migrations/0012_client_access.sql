-- +goose Up
CREATE TABLE client_access (
    client_id INTEGER NOT NULL REFERENCES oidc_clients(id),
    domain TEXT NOT NULL DEFAULT '',
    email TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_client_access_email ON client_access(email);
CREATE INDEX idx_client_access_domain ON client_access(domain);

-- +goose Down
DROP TABLE client_access;
