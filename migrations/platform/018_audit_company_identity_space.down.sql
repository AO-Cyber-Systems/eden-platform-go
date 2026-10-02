-- Down: revert 018, restoring the 005 NOT NULL on audit_logs.company_id.
--
-- CAVEAT: if any audit_logs row was written with a NULL company_id (i.e. a
-- household / identity-space event) while the constraint was absent, re-adding
-- NOT NULL will fail the validation scan. That is inherent to reverting this
-- change: the down migration restores the prior, stricter schema and can only
-- succeed if every company_id is currently non-NULL. Backfill or delete the
-- NULL-company (household / COPPA) rows before running this down. The FK was
-- never dropped by 018, so nothing about it is restored here.

COMMENT ON COLUMN audit_logs.company_id IS NULL;

ALTER TABLE audit_logs
    ALTER COLUMN company_id SET NOT NULL;
