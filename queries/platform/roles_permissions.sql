-- name: GetRoleByID :one
SELECT * FROM roles WHERE id = $1;

-- name: ListRolesByCompany :many
SELECT * FROM roles
WHERE company_id = $1 OR is_system = true
ORDER BY level DESC;

-- name: CreateRole :one
INSERT INTO roles (company_id, name, description, level)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListPermissionsByRole :many
SELECT p.* FROM permissions p
JOIN role_permissions rp ON rp.permission_id = p.id
WHERE rp.role_id = $1;

-- name: ListAllPermissions :many
SELECT * FROM permissions ORDER BY feature, action;

-- name: AddRolePermission :exec
INSERT INTO role_permissions (role_id, permission_id)
VALUES ($1, $2)
ON CONFLICT (role_id, permission_id) DO NOTHING;

-- name: GetUserRole :one
SELECT r.* FROM roles r
JOIN company_memberships cm ON cm.role_id = r.id
WHERE cm.company_id = $1 AND cm.user_id = $2;

-- name: GetMembership :one
SELECT cm.id, cm.company_id, cm.user_id, cm.role_id, r.name as role_name, r.level as role_level, cm.permission_overrides
FROM company_memberships cm
JOIN roles r ON r.id = cm.role_id
WHERE cm.company_id = $1 AND cm.user_id = $2;

-- name: GetUserPermissions :many
SELECT p.* FROM permissions p
JOIN role_permissions rp ON rp.permission_id = p.id
JOIN company_memberships cm ON cm.role_id = rp.role_id
WHERE cm.company_id = $1 AND cm.user_id = $2;

-- name: AssignRoleToUser :exec
UPDATE company_memberships SET role_id = $3, updated_at = now()
WHERE company_id = $1 AND user_id = $2;

-- name: CreateMembership :exec
INSERT INTO company_memberships (company_id, user_id, role_id)
VALUES ($1, $2, $3)
ON CONFLICT (company_id, user_id) DO NOTHING;

-- name: GetCompanyAncestors :many
SELECT ch.ancestor_id as company_id, ch.generations,
    c.inherited_role_cap, c.inherited_access_level as access_level
FROM company_hierarchies ch
JOIN companies c ON c.id = ch.ancestor_id
WHERE ch.descendant_id = $1 AND ch.generations > 0
ORDER BY ch.generations ASC;

-- name: ListUserCompanyIDs :many
SELECT company_id FROM company_memberships WHERE user_id = $1;
