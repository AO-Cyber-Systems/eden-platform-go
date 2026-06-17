-- name: UpsertUserIdentity :one
INSERT INTO user_identities (user_id, provider, provider_sub, email, is_verified, display_name, avatar_url, raw_claims)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (provider, provider_sub) DO UPDATE SET
    email = EXCLUDED.email,
    is_verified = EXCLUDED.is_verified,
    display_name = EXCLUDED.display_name,
    avatar_url = EXCLUDED.avatar_url,
    raw_claims = EXCLUDED.raw_claims,
    updated_at = now()
RETURNING *;

-- name: GetUserIdentityByProviderSub :one
SELECT * FROM user_identities WHERE provider = $1 AND provider_sub = $2;

-- name: GetUserIdentityByEmail :one
SELECT * FROM user_identities WHERE email = $1;

-- name: ListUserIdentitiesByUser :many
SELECT * FROM user_identities WHERE user_id = $1 ORDER BY created_at;

-- name: DeleteUserIdentity :exec
DELETE FROM user_identities WHERE id = $1;
