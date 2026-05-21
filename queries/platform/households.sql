-- name: CreateHousehold :one
INSERT INTO platform_households (primary_contact_user_id, display_name, metadata)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetHousehold :one
SELECT * FROM platform_households WHERE id = $1;

-- name: UpdateHousehold :one
UPDATE platform_households
SET display_name = $2, metadata = $3, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteHousehold :exec
DELETE FROM platform_households WHERE id = $1;

-- name: AddHouseholdMember :one
INSERT INTO platform_household_members (household_id, user_id, role, status, birthdate, capabilities)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetHouseholdMember :one
SELECT * FROM platform_household_members WHERE id = $1;

-- name: UpdateHouseholdMemberRole :one
UPDATE platform_household_members
SET role = $2, capabilities = $3
WHERE id = $1
RETURNING *;

-- name: RemoveHouseholdMember :exec
UPDATE platform_household_members
SET status = 'removed', removed_at = now()
WHERE id = $1;

-- name: ListHouseholdMembers :many
SELECT * FROM platform_household_members
WHERE household_id = $1 AND status != 'removed'
ORDER BY added_at ASC;

-- name: ListHouseholdsForUser :many
SELECT h.* FROM platform_households h
JOIN platform_household_members m ON m.household_id = h.id
WHERE m.user_id = $1 AND m.status != 'removed'
ORDER BY h.created_at DESC;

-- name: EstablishParentOfRecord :one
INSERT INTO platform_parent_of_record (child_member_id, parent_member_id)
VALUES ($1, $2)
RETURNING *;

-- name: RevokeParentOfRecord :exec
UPDATE platform_parent_of_record
SET revoked_at = now()
WHERE id = $1 AND revoked_at IS NULL;

-- name: ListParentsOfRecord :many
SELECT * FROM platform_parent_of_record
WHERE child_member_id = $1 AND revoked_at IS NULL
ORDER BY established_at ASC;

-- name: ListChildrenForParent :many
SELECT * FROM platform_parent_of_record
WHERE parent_member_id = $1 AND revoked_at IS NULL
ORDER BY established_at ASC;
