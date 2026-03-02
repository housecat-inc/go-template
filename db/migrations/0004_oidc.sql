-- +goose Up

-- OIDC clients (matches op.Client interface)
CREATE TABLE IF NOT EXISTS oidc_clients (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    client_id TEXT NOT NULL UNIQUE,
    client_secret TEXT NOT NULL,
    name TEXT NOT NULL,
    redirect_uris TEXT NOT NULL DEFAULT '',
    post_logout_redirect_uris TEXT NOT NULL DEFAULT '',
    application_type TEXT NOT NULL DEFAULT 'web',
    auth_method TEXT NOT NULL DEFAULT 'client_secret_basic',
    response_types TEXT NOT NULL DEFAULT 'code',
    grant_types TEXT NOT NULL DEFAULT 'authorization_code',
    access_token_type TEXT NOT NULL DEFAULT 'bearer',
    scopes TEXT NOT NULL DEFAULT 'openid,email,profile',
    created_by TEXT NOT NULL,
    archived_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_oidc_clients_client_id ON oidc_clients(client_id);

-- Auth requests (short-lived, matches op.AuthRequest interface)
CREATE TABLE IF NOT EXISTS oidc_auth_requests (
    id TEXT PRIMARY KEY,
    client_id TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    scopes TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT '',
    nonce TEXT NOT NULL DEFAULT '',
    response_type TEXT NOT NULL DEFAULT 'code',
    code_challenge TEXT NOT NULL DEFAULT '',
    code_challenge_method TEXT NOT NULL DEFAULT '',
    user_id TEXT NOT NULL DEFAULT '',
    user_email TEXT NOT NULL DEFAULT '',
    auth_time TIMESTAMP,
    done INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Authorization codes -> auth request mapping
CREATE TABLE IF NOT EXISTS oidc_codes (
    code TEXT PRIMARY KEY,
    auth_request_id TEXT NOT NULL REFERENCES oidc_auth_requests(id),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Access tokens
CREATE TABLE IF NOT EXISTS oidc_access_tokens (
    id TEXT PRIMARY KEY,
    application_id TEXT NOT NULL,
    subject TEXT NOT NULL,
    audience TEXT NOT NULL DEFAULT '',
    scopes TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_oidc_access_tokens_subject ON oidc_access_tokens(subject);

-- Refresh tokens
CREATE TABLE IF NOT EXISTS oidc_refresh_tokens (
    id TEXT PRIMARY KEY,
    token TEXT NOT NULL UNIQUE,
    auth_time TIMESTAMP NOT NULL,
    audience TEXT NOT NULL DEFAULT '',
    user_id TEXT NOT NULL,
    application_id TEXT NOT NULL,
    scopes TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- +goose Down
DROP TABLE IF EXISTS oidc_refresh_tokens;
DROP TABLE IF EXISTS oidc_access_tokens;
DROP TABLE IF EXISTS oidc_codes;
DROP TABLE IF EXISTS oidc_auth_requests;
DROP TABLE IF EXISTS oidc_clients;
