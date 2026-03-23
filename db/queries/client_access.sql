-- name: InsertClientAccess :exec
INSERT INTO client_access (client_id, domain, email) VALUES (?, ?, ?);

-- name: DeleteClientAccessByClientID :exec
DELETE FROM client_access WHERE client_id = ?;

-- name: ListOidcClientsByAccess :many
SELECT DISTINCT c.* FROM oidc_clients c
JOIN client_access ca ON ca.client_id = c.id
WHERE (ca.email = ? OR ca.domain = ?)
AND c.archived_at IS NULL
ORDER BY c.created_at DESC;
