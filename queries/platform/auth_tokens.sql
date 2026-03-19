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
