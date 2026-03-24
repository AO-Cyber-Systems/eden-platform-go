-- name: CreateRefreshToken :exec
INSERT INTO refresh_tokens (user_id, token_hash, expires_at)
VALUES ($1, $2, $3);

-- name: GetRefreshToken :one
SELECT * FROM refresh_tokens
WHERE token_hash = $1 AND revoked = false AND expires_at > now();

-- name: RevokeRefreshToken :exec
UPDATE refresh_tokens SET revoked = true WHERE token_hash = $1;

-- name: RevokeAllUserTokens :exec
UPDATE refresh_tokens SET revoked = true WHERE user_id = $1;

-- name: GetSSOConfig :one
SELECT * FROM sso_configs
WHERE company_id = $1 AND provider = $2 AND is_active = true;

-- name: CreateSSOConfig :one
INSERT INTO sso_configs (company_id, provider, issuer_url, client_id, client_secret, metadata_url)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListSSOConfigs :many
SELECT * FROM sso_configs WHERE company_id = $1 ORDER BY provider;

-- name: UpsertSSOConfig :exec
INSERT INTO sso_configs (company_id, provider, issuer_url, client_id, client_secret, metadata_url, display_name, extra_scopes, enforce_sso, is_active, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now())
ON CONFLICT (company_id, provider) DO UPDATE SET
    issuer_url = EXCLUDED.issuer_url,
    client_id = EXCLUDED.client_id,
    client_secret = EXCLUDED.client_secret,
    metadata_url = EXCLUDED.metadata_url,
    display_name = EXCLUDED.display_name,
    extra_scopes = EXCLUDED.extra_scopes,
    enforce_sso = EXCLUDED.enforce_sso,
    is_active = EXCLUDED.is_active,
    updated_at = now();

-- name: DeleteSSOConfig :exec
DELETE FROM sso_configs WHERE company_id = $1 AND provider = $2;

-- name: HasEnforcedSSO :one
SELECT EXISTS(
    SELECT 1 FROM sso_configs
    WHERE company_id = $1 AND enforce_sso = true AND is_active = true
) AS enforced;

-- name: UpsertOAuthCredential :exec
INSERT INTO oauth_credentials (company_id, user_id, provider, access_token, refresh_token, token_expiry, scopes, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, now())
ON CONFLICT (company_id, user_id, provider) DO UPDATE SET
    access_token = EXCLUDED.access_token,
    refresh_token = EXCLUDED.refresh_token,
    token_expiry = EXCLUDED.token_expiry,
    scopes = EXCLUDED.scopes,
    updated_at = now();

-- name: GetOAuthCredential :one
SELECT * FROM oauth_credentials WHERE user_id = $1 AND provider = $2;
