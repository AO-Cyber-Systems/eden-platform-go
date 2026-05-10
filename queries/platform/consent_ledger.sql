-- name: InsertConsentEntry :one
-- Inserts a grant (revokes_id NULL) or revocation (revokes_id set).
INSERT INTO consent_ledger (
    household_id, principal_member_id, consenter_member_id,
    purpose, scope, consent_text_version, evidence, revokes_id
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetConsentEntry :one
SELECT * FROM consent_ledger WHERE id = $1;

-- name: LatestConsentForPurpose :one
-- Returns the most recent ledger row for (principal, purpose), regardless of
-- whether it is a grant or revocation. Service-layer interprets the result.
SELECT * FROM consent_ledger
WHERE principal_member_id = $1 AND purpose = $2
ORDER BY granted_at DESC, created_at DESC
LIMIT 1;

-- name: ListConsentEntriesForPrincipal :many
SELECT * FROM consent_ledger
WHERE principal_member_id = $1
ORDER BY granted_at DESC, created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListConsentEntriesByPurpose :many
SELECT * FROM consent_ledger
WHERE purpose = $1
ORDER BY granted_at DESC, created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListConsentEntriesForHousehold :many
SELECT * FROM consent_ledger
WHERE household_id = $1
ORDER BY granted_at DESC, created_at DESC
LIMIT $2 OFFSET $3;
