-- name: CreateAuditLog :exec
INSERT INTO audit_logs (company_id, actor_id, action, resource, resource_id, details, ip_address)
VALUES ($1, $2, $3, $4, $5, $6, $7);

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
