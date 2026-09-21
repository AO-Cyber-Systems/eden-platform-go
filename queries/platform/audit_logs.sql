-- name: CreateAuditLog :exec
-- company_id is nullable (migration 018): identity-space events (household /
-- COPPA audit) have no companies(id) and pass NULL. The pgstore/devstore
-- adapters map a zero (all-bits-0) uuid.UUID to NULL here; a real company id is
-- stored unchanged and is still FK-validated against companies(id).
INSERT INTO audit_logs (company_id, actor_id, action, resource, resource_id, details, ip_address)
VALUES (sqlc.narg(company_id), sqlc.arg(actor_id), sqlc.arg(action), sqlc.arg(resource), sqlc.arg(resource_id), sqlc.arg(details), sqlc.arg(ip_address));

-- name: ListAuditLogs :many
SELECT * FROM audit_logs
WHERE company_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListAuditLogsByActor :many
SELECT * FROM audit_logs
WHERE company_id = $1 AND actor_id = $2
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: CountAuditLogs :one
SELECT count(*) FROM audit_logs WHERE company_id = $1;

-- name: ListAuditLogsByAction :many
SELECT * FROM audit_logs
WHERE company_id = $1 AND action = $2
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: ListAuditLogsByResource :many
SELECT * FROM audit_logs
WHERE company_id = $1 AND resource = $2
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: CountAuditLogsByActor :one
SELECT count(*) FROM audit_logs WHERE company_id = $1 AND actor_id = $2;

-- name: ListHouseholdAuditLogs :many
-- Identity-space reader for the household / COPPA trail. Household events are
-- written with company_id NULL and resource = 'household', so none of the
-- company-scoped readers above can ever return them. Scoped by resource_id
-- (the household), so it can never cross households or read a company row.
SELECT * FROM audit_logs
WHERE company_id IS NULL AND resource = 'household' AND resource_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CountHouseholdAuditLogs :one
SELECT count(*) FROM audit_logs
WHERE company_id IS NULL AND resource = 'household' AND resource_id = $1;
