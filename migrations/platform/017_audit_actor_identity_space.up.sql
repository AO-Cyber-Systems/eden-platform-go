-- 017: Re-home the audit actor onto the identity space.
--
-- Context: 005 created audit_logs.actor_id as
--     actor_id UUID NOT NULL REFERENCES users(id)
-- That FK is now wrong. Household mutations are audited with the acting AOID
-- identity as the actor (see platform/household.AuditContext), and an identity
-- is NOT a platform.users row. With the FK in place, following the documented
-- household audit contract in production would FK-fail on every insert — and
-- the audit logger swallows insert errors (platform/audit/logger.go), so the
-- COPPA / consent trail this package exists for would be SILENTLY DROPPED.
--
-- Fix: drop the REFERENCES users(id) FK so actor_id is a LOGICAL actor UUID.
-- The column stays UUID NOT NULL. It may now hold either a platform.users(id)
-- (auth/login/signup events keep passing user.ID — a plain UUID, no FK = fine)
-- OR an aoid.identities(id) (household / consent events). No reader JOINs
-- actor_id to users(id); every query treats it as an opaque UUID column, so
-- reads are unaffected.
--
-- 005 auto-named the inline FK "audit_logs_actor_id_fkey" (Postgres default:
-- <table>_<column>_fkey). Drop it by that name; IF EXISTS keeps this idempotent.

ALTER TABLE audit_logs
    DROP CONSTRAINT IF EXISTS audit_logs_actor_id_fkey;

COMMENT ON COLUMN audit_logs.actor_id IS
    'Logical actor UUID (NOT NULL, no FK). Dual id-space: a platform.users(id) '
    'for auth/admin events, or an aoid.identities(id) for household/consent '
    'events. Not FK-constrained because AOID identities live in a separate '
    'database from eden_platform.';
