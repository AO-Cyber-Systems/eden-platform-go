-- 016: Re-home platform households onto AOID identity (Option B) + two-axis
-- role / capability model.
--
-- Context: the 012 household tables are dormant (no rows, no production
-- consumers). Per the signed-off Eden Family ADR (Option B), household
-- membership keys on a LOGICAL identity_id = aoid.identities(id) rather than
-- platform.users(id). identity_id is validated at provisioning time; it is
-- NOT an enforced cross-DB foreign key because platform (eden_platform) and
-- AOID (aoid) live in separate databases.
--
-- Role axis (relationship):   guardian | adult | child
-- Capability axis:            is_manager       (0..n per household)
--                             is_account_owner (at most 1 per household)
--
-- Enforced here (DB): role domain, child-never-manager, child-never-owner,
-- at-most-one account_owner (the partial unique index below).
--
-- Enforced in the service layer (not the DB): "account_owner must be an adult"
-- (non-child), the account_owner cannot be removed without first transferring
-- it, and the last manager cannot be removed.
--
-- NOT enforced by anything in this phase: any LOWER bound. There is no
-- ">=1 manager" or "exactly-one owner" guarantee here — CreateHousehold makes
-- an EMPTY household, so zero managers / zero owners is reachable through the
-- store/service alone. The atomic existence guarantee (create a household and
-- seed its first manager + account_owner together) is built at the Phase 0.4
-- provisioning seam, NOT in this migration or this phase.

-- ---- platform_households: primary contact becomes an identity reference ----
ALTER TABLE platform_households
    DROP CONSTRAINT IF EXISTS platform_households_primary_contact_user_id_fkey;
ALTER TABLE platform_households
    RENAME COLUMN primary_contact_user_id TO primary_contact_identity_id;
ALTER INDEX idx_platform_households_primary_contact
    RENAME TO idx_platform_households_primary_contact_identity;

-- ---- platform_household_members: re-home + two-axis model ----
ALTER TABLE platform_household_members
    DROP CONSTRAINT IF EXISTS platform_household_members_user_id_fkey;
ALTER TABLE platform_household_members
    RENAME COLUMN user_id TO identity_id;

-- Capability columns (default false; existing dormant rows: none).
ALTER TABLE platform_household_members
    ADD COLUMN is_manager BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN is_account_owner BOOLEAN NOT NULL DEFAULT false;

-- Constrain the relationship role to the new domain.
ALTER TABLE platform_household_members
    ADD CONSTRAINT chk_household_member_role
        CHECK (role IN ('guardian', 'adult', 'child'));

-- A child can never hold a capability.
ALTER TABLE platform_household_members
    ADD CONSTRAINT chk_child_not_manager
        CHECK (NOT (role = 'child' AND is_manager)),
    ADD CONSTRAINT chk_child_not_account_owner
        CHECK (NOT (role = 'child' AND is_account_owner));

-- Exactly-one account_owner per household is enforced as at-most-one here
-- (partial unique index over active members) + a >=1 guard in the service.
CREATE UNIQUE INDEX uq_platform_household_one_account_owner
    ON platform_household_members (household_id)
    WHERE is_account_owner AND status <> 'removed';

-- The 012 per-user index now covers identity_id after the rename; rename it
-- to match so lookups by identity are self-documenting.
ALTER INDEX idx_platform_household_members_user
    RENAME TO idx_platform_household_members_identity;
