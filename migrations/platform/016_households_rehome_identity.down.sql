-- Down: revert 016, restoring the 012 user-keyed shape.
--
-- CAVEAT: this migration is what makes the household tables NON-dormant. Once
-- any household or member row exists, identity_id / primary_contact_identity_id
-- hold aoid.identities(id) values, NOT platform.users(id) — so the two
-- ADD CONSTRAINT ... REFERENCES users(id) statements below will fail their
-- validation scan. That is inherent to reverting this change: the down
-- migration restores the prior, stricter schema and can only succeed on an
-- empty (or users-keyed) table. Delete or remap every household row before
-- running this down. Same shape as the 017 / 018 CAVEATs.

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
