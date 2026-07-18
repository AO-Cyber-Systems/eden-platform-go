-- Reverse COMPANION-AV05-AOID-JIT TRD-06 sso_configs JIT-policy columns.
ALTER TABLE sso_configs DROP COLUMN IF EXISTS jit_enabled;
ALTER TABLE sso_configs DROP COLUMN IF EXISTS jit_default_role;
ALTER TABLE sso_configs DROP COLUMN IF EXISTS email_domain_allowlist;
