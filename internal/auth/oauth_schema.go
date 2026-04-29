package auth

// oauthSchemaSQL adds the three tables that back the in-process OAuth
// 2.1 authorization server: registered MCP clients, short-lived
// authorization codes, and audience-bound access + refresh tokens.
//
// Kept separate from the core users / user_tokens schema because:
//   - the lifecycles differ (codes expire in minutes, access tokens in
//     hours, refresh tokens rotate on every use)
//   - PATs (user_tokens) and OAuth-issued tokens are validated through
//     different code paths even though both arrive as Bearer credentials
//   - ripping OAuth out later (e.g. delegating to an external AS) only
//     touches these tables
//
// All token-bearing values are stored as sha256 hashes; plaintext is
// returned once at issuance and then forgotten, mirroring the existing
// PAT model.
const oauthSchemaSQL = `
CREATE TABLE IF NOT EXISTS oauth_clients (
    client_id                  TEXT NOT NULL PRIMARY KEY,
    client_secret_hash         TEXT,                       -- nullable: public clients have no secret
    client_name                TEXT NOT NULL,
    redirect_uris_json         TEXT NOT NULL,              -- JSON array of pre-registered redirect URIs
    grant_types_json           TEXT NOT NULL DEFAULT '["authorization_code","refresh_token"]',
    token_endpoint_auth_method TEXT NOT NULL DEFAULT 'none' CHECK (token_endpoint_auth_method IN ('none','client_secret_basic','client_secret_post')),
    scope                      TEXT NOT NULL DEFAULT 'mcp',
    created_at                 INTEGER NOT NULL,
    created_by                 TEXT                         -- username that registered it (NULL for DCR)
) STRICT;

CREATE TABLE IF NOT EXISTS oauth_codes (
    code_hash             TEXT NOT NULL PRIMARY KEY,        -- sha256(plaintext)
    client_id             TEXT NOT NULL REFERENCES oauth_clients(client_id),
    username              TEXT NOT NULL REFERENCES users(username),
    redirect_uri          TEXT NOT NULL,
    code_challenge        TEXT NOT NULL,
    code_challenge_method TEXT NOT NULL CHECK (code_challenge_method = 'S256'),
    scope                 TEXT NOT NULL,
    audience              TEXT NOT NULL,                    -- RFC 8707 resource indicator
    expires_at            INTEGER NOT NULL,
    used_at               INTEGER                           -- single-use; UPDATE WHERE used_at IS NULL gates redemption
) STRICT;

CREATE TABLE IF NOT EXISTS oauth_tokens (
    id                    TEXT NOT NULL PRIMARY KEY,
    access_token_hash     TEXT NOT NULL UNIQUE,             -- sha256(plaintext)
    refresh_token_hash    TEXT UNIQUE,                      -- nullable; populated when refresh issued
    client_id             TEXT NOT NULL REFERENCES oauth_clients(client_id),
    username              TEXT NOT NULL REFERENCES users(username),
    scope                 TEXT NOT NULL,
    audience              TEXT NOT NULL,
    access_expires_at     INTEGER NOT NULL,
    refresh_expires_at    INTEGER,
    created_at            INTEGER NOT NULL,
    revoked_at            INTEGER,
    last_used_at          INTEGER
) STRICT;

CREATE INDEX IF NOT EXISTS idx_oauth_tokens_username     ON oauth_tokens(username);
CREATE INDEX IF NOT EXISTS idx_oauth_tokens_client       ON oauth_tokens(client_id);
CREATE INDEX IF NOT EXISTS idx_oauth_tokens_active_access ON oauth_tokens(access_token_hash) WHERE revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_oauth_tokens_active_refresh ON oauth_tokens(refresh_token_hash) WHERE revoked_at IS NULL AND refresh_token_hash IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_oauth_codes_expires ON oauth_codes(expires_at);
`
