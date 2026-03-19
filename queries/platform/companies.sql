-- name: CreateCompany :one
INSERT INTO companies (id, name, slug, parent_company_id, company_type, inherited_role_cap, inherited_access_level, settings)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetCompany :one
SELECT * FROM companies WHERE id = $1;

-- name: UpdateCompany :one
UPDATE companies
SET name = $2, slug = $3, company_type = $4, inherited_role_cap = $5, inherited_access_level = $6, settings = $7, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListCompanies :many
SELECT * FROM companies WHERE is_active = true ORDER BY name;

-- name: InsertHierarchyEntry :exec
INSERT INTO company_hierarchies (ancestor_id, descendant_id, generations)
VALUES ($1, $2, $3)
ON CONFLICT (ancestor_id, descendant_id) DO NOTHING;

-- name: GetAncestors :many
SELECT * FROM company_hierarchies
WHERE descendant_id = $1
ORDER BY generations ASC;

-- name: GetDescendants :many
SELECT * FROM company_hierarchies
WHERE ancestor_id = $1
ORDER BY generations ASC;

-- name: GetSelfAndDescendantIDs :many
SELECT descendant_id FROM company_hierarchies
WHERE ancestor_id = $1;

-- name: DeleteHierarchyEntries :exec
DELETE FROM company_hierarchies WHERE descendant_id = $1;
