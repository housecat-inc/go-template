-- name: InsertClientAccess :exec
INSERT INTO client_access (client_id, domain, email) VALUES (?, ?, ?);

-- name: DeleteClientAccessByClientID :exec
DELETE FROM client_access WHERE client_id = ?;

-- name: ListClientAccessByClientID :many
SELECT * FROM client_access WHERE client_id = ?
ORDER BY domain ASC, email ASC;

-- name: ListAllClientAccess :many
SELECT * FROM client_access
ORDER BY client_id, domain ASC, email ASC;

-- name: ListOidcClientsByAccess :many
SELECT DISTINCT c.* FROM oidc_clients c
JOIN client_access ca ON ca.client_id = c.id
WHERE (ca.email = ? OR ca.domain = ? OR ca.domain = '*')
AND c.archived_at IS NULL
ORDER BY c.created_at DESC;
