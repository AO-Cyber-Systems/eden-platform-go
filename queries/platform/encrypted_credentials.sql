-- name: UpsertCredential :exec
INSERT INTO encrypted_credentials (company_id, credential_type, encrypted_value, blind_index)
VALUES ($1, $2, $3, $4)
ON CONFLICT (company_id, credential_type) DO UPDATE
SET encrypted_value = EXCLUDED.encrypted_value, blind_index = EXCLUDED.blind_index, updated_at = now();

-- name: GetCredential :one
SELECT * FROM encrypted_credentials
WHERE company_id = $1 AND credential_type = $2;

-- name: GetCredentialByBlindIndex :one
SELECT * FROM encrypted_credentials
WHERE blind_index = $1;

-- name: DeleteCredential :exec
DELETE FROM encrypted_credentials
WHERE company_id = $1 AND credential_type = $2;

-- name: ListCredentialTypes :many
SELECT credential_type FROM encrypted_credentials
WHERE company_id = $1
ORDER BY credential_type;
