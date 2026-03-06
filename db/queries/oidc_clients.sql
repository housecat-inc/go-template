-- name: InsertOidcClient :one
INSERT INTO oidc_clients (allowed_domain, allowed_emails, client_id, client_secret, name, redirect_uris, scopes, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: InsertOidcClientFull :one
INSERT INTO oidc_clients (allowed_domain, allowed_emails, client_id, client_secret, name, redirect_uris, scopes, auth_method, grant_types, created_by)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetOidcClient :one
SELECT * FROM oidc_clients
WHERE id = ? AND archived_at IS NULL;

-- name: GetOidcClientByClientID :one
SELECT * FROM oidc_clients
WHERE client_id = ? AND archived_at IS NULL;

-- name: ListOidcClients :many
SELECT * FROM oidc_clients
WHERE archived_at IS NULL
ORDER BY created_at DESC;

-- name: UpdateOidcClient :exec
UPDATE oidc_clients
SET allowed_domain = ?, allowed_emails = ?, name = ?, redirect_uris = ?, scopes = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND archived_at IS NULL;

-- name: ArchiveOidcClient :exec
UPDATE oidc_clients
SET archived_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE id = ? AND archived_at IS NULL;

-- name: CountOidcClients :one
SELECT COUNT(*) FROM oidc_clients
WHERE archived_at IS NULL;
