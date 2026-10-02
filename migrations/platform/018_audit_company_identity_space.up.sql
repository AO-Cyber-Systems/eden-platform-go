-- 018: Let audit_logs.company_id be NULL for the household / identity actor
-- space (companion of migration 017, which re-homed audit_logs.actor_id).
--
-- Context: 005 created
--     company_id UUID NOT NULL REFERENCES companies(id)
-- Every COMPANY-scoped audit event sets a real companies(id) and is unchanged
-- by this migration. But the household service (platform/household) audits
-- household / COPPA events (household.created / member_added / member_removed /
-- household.deleted / parent_of_record.*) and a household is NOT a platform
-- company: it has no companies(id). The service emits those events with
-- company_id = uuid.Nil (the all-zero UUID). Under the 005 schema that value is
--   (a) NOT NULL — passes the column constraint, but
--   (b) still FK-checked against companies(id) — and no company has the all-zero
--       id, so every insert violates audit_logs_company_id_fkey.
-- The audit logger is async and swallows insert errors (slog.Warn in
-- platform/audit/logger.go), so the household / COPPA audit trail this package
-- exists for is SILENTLY DROPPED in production.
--
-- Fix: drop NOT NULL so company_id may be NULL for household / identity-space
-- events. The FK (audit_logs_company_id_fkey, auto-named by 005) is LEFT IN
-- PLACE and unchanged: SQL foreign keys exempt NULL, so a NULL company_id
-- inserts cleanly while every NON-NULL company_id is still validated against
-- companies(id) exactly as before. The CreateAuditLog query (queries/platform/
-- audit_logs.sql) maps a zero-UUID company argument to SQL NULL via NULLIF, so
-- the household path lands a true NULL rather than the all-zero UUID.

ALTER TABLE audit_logs
    ALTER COLUMN company_id DROP NOT NULL;

COMMENT ON COLUMN audit_logs.company_id IS
    'Company scope for the event, or NULL for identity-space events that have '
    'no company (household / COPPA audit — a household is not a companies(id)). '
    'FK-validated against companies(id) when NON-NULL; NULL is allowed and '
    'exempt from the FK. See migration 018 and platform/household.';
