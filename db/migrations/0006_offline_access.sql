-- +goose Up

-- Add offline_access scope and refresh_token grant type to all existing clients
UPDATE oidc_clients
SET scopes = scopes || ',offline_access',
    grant_types = CASE
        WHEN grant_types = '' THEN 'authorization_code,refresh_token'
        ELSE grant_types || ',refresh_token'
    END,
    updated_at = CURRENT_TIMESTAMP
WHERE archived_at IS NULL
  AND ',' || scopes || ',' NOT LIKE '%,offline_access,%';

-- +goose Down

UPDATE oidc_clients
SET scopes = REPLACE(REPLACE(REPLACE(scopes, ',offline_access', ''), 'offline_access,', ''), 'offline_access', ''),
    grant_types = REPLACE(REPLACE(REPLACE(grant_types, ',refresh_token', ''), 'refresh_token,', ''), 'refresh_token', ''),
    updated_at = CURRENT_TIMESTAMP
WHERE archived_at IS NULL;
