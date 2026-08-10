-- Down: revert 017, restoring the 005 FK on audit_logs.actor_id -> users(id).
--
-- CAVEAT: if any audit_logs row was written with an actor_id that is an AOID
-- identity (i.e. not a platform.users(id)) while the FK was absent, re-adding
-- the FK will fail the validation scan. That is inherent to reverting this
-- change: the down migration restores the prior, stricter schema and can only
-- succeed if every actor_id currently resolves to a users row. Clean up or
-- remap identity-space actor rows before running this down.

COMMENT ON COLUMN audit_logs.actor_id IS NULL;

ALTER TABLE audit_logs
    ADD CONSTRAINT audit_logs_actor_id_fkey
        FOREIGN KEY (actor_id) REFERENCES users(id);
