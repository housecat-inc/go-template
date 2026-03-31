-- +goose Up
DELETE FROM oauth_tokens WHERE service = 'notion' AND level = 'write'
  AND subject IN (SELECT subject FROM oauth_tokens WHERE service = 'notion' AND level = 'read');
UPDATE oauth_tokens SET level = 'read' WHERE service = 'notion' AND level = 'write';

-- +goose Down
UPDATE oauth_tokens SET level = 'write' WHERE service = 'notion' AND level = 'read';
