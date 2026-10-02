-- name: CreateHousehold :one
INSERT INTO platform_households (primary_contact_identity_id, display_name, metadata)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetHouseholdByID :one
SELECT * FROM platform_households WHERE id = $1;

-- name: UpdateHousehold :one
UPDATE platform_households
SET display_name = $2, metadata = $3, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteHousehold :exec
DELETE FROM platform_households WHERE id = $1;

-- name: AddHouseholdMember :one
INSERT INTO platform_household_members (
    household_id, identity_id, role, status, birthdate,
    is_manager, is_account_owner, capabilities
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetHouseholdMember :one
SELECT * FROM platform_household_members WHERE id = $1;

-- name: GetMemberByHouseholdAndIdentity :one
SELECT * FROM platform_household_members
WHERE household_id = $1 AND identity_id = $2 AND status <> 'removed';

-- name: UpdateHouseholdMemberRole :one
UPDATE platform_household_members
SET role = $2, is_manager = $3, is_account_owner = $4, capabilities = $5
WHERE id = $1
RETURNING *;

-- name: RemoveHouseholdMember :exec
UPDATE platform_household_members
SET status = 'removed', removed_at = now()
WHERE id = $1;

-- name: ListHouseholdMembers :many
SELECT * FROM platform_household_members
WHERE household_id = $1 AND status <> 'removed'
ORDER BY added_at ASC;

-- name: LockHousehold :one
-- Serialises every invariant-bearing mutation on one household (remove /
-- demote a manager, transfer the account_owner). Callers take this inside the
-- same transaction as their read-check-write so two concurrent removals of the
-- only two managers cannot both observe count=2.
SELECT id FROM platform_households WHERE id = $1 FOR UPDATE;

-- name: GetHouseholdMemberForUpdate :one
SELECT * FROM platform_household_members WHERE id = $1 FOR UPDATE;

-- name: CountHouseholdManagers :one
SELECT count(*) FROM platform_household_members
WHERE household_id = $1 AND is_manager AND status <> 'removed';

-- name: GetActiveMembershipForIdentity :one
-- Clean read path: the first (oldest) active membership for an identity. Phase
-- 0 assumes one household per identity; ordering keeps the choice deterministic
-- if that ever changes.
SELECT * FROM platform_household_members
WHERE identity_id = $1 AND status <> 'removed'
ORDER BY added_at ASC
LIMIT 1;

-- name: ListHouseholdsForIdentity :many
SELECT h.* FROM platform_households h
JOIN platform_household_members m ON m.household_id = h.id
WHERE m.identity_id = $1 AND m.status <> 'removed'
ORDER BY h.created_at DESC;

-- name: ClearHouseholdAccountOwner :exec
-- Step 1 of an account_owner transfer: demote the current owner. Scoped to the
-- household so a transfer can never touch another household's rows.
UPDATE platform_household_members
SET is_account_owner = false
WHERE household_id = $1 AND is_account_owner AND status <> 'removed';

-- name: SetHouseholdAccountOwner :one
-- Step 2 of an account_owner transfer: promote the named member. Household-
-- scoped: the member id must belong to $1 or no row is updated.
UPDATE platform_household_members
SET is_account_owner = true
WHERE id = $2 AND household_id = $1 AND status <> 'removed'
RETURNING *;

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
