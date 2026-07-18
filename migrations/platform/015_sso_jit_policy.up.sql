-- COMPANION-AV05-AOID-JIT TRD-06: move the JIT provisioning policy OFF biz env
-- vars and ONTO the per-company sso_configs row (biz-managed, admin-editable
-- data). The company binding is DERIVED from the SSOConfig (issuer_url +
-- email-domain allowlist match), so the biz AOID_JIT_COMPANY_ID /
-- AOID_JIT_EMAIL_DOMAIN_ALLOWLIST / AOID_JIT_DEFAULT_ROLE env vars are removed.
--
-- Consumers vendor these SQL migrations (platform-go is a library, not a
-- service -- see MEMORY platform-go-layering); biz picks the columns up via the
-- ../../eden-libs/eden-platform-go replace + its own migration-parity copy.

ALTER TABLE sso_configs ADD COLUMN IF NOT EXISTS email_domain_allowlist TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE sso_configs ADD COLUMN IF NOT EXISTS jit_default_role TEXT NOT NULL DEFAULT '';
ALTER TABLE sso_configs ADD COLUMN IF NOT EXISTS jit_enabled BOOLEAN NOT NULL DEFAULT false;
