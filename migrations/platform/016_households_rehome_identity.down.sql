-- Down: revert 016, restoring the 012 user-keyed shape. Safe because the
-- tables are dormant (no rows).

ALTER INDEX idx_platform_household_members_identity
    RENAME TO idx_platform_household_members_user;

DROP INDEX IF EXISTS uq_platform_household_one_account_owner;

ALTER TABLE platform_household_members
    DROP CONSTRAINT IF EXISTS chk_child_not_account_owner,
    DROP CONSTRAINT IF EXISTS chk_child_not_manager,
    DROP CONSTRAINT IF EXISTS chk_household_member_role;

ALTER TABLE platform_household_members
    DROP COLUMN IF EXISTS is_account_owner,
    DROP COLUMN IF EXISTS is_manager;

ALTER TABLE platform_household_members
    RENAME COLUMN identity_id TO user_id;
ALTER TABLE platform_household_members
    ADD CONSTRAINT platform_household_members_user_id_fkey
        FOREIGN KEY (user_id) REFERENCES users(id);

ALTER INDEX idx_platform_households_primary_contact_identity
    RENAME TO idx_platform_households_primary_contact;
ALTER TABLE platform_households
    RENAME COLUMN primary_contact_identity_id TO primary_contact_user_id;
ALTER TABLE platform_households
    ADD CONSTRAINT platform_households_primary_contact_user_id_fkey
        FOREIGN KEY (primary_contact_user_id) REFERENCES users(id);
