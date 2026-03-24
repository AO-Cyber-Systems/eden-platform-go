DROP TABLE IF EXISTS oauth_credentials;

ALTER TABLE sso_configs DROP COLUMN IF EXISTS updated_at;
ALTER TABLE sso_configs DROP COLUMN IF EXISTS enforce_sso;
ALTER TABLE sso_configs DROP COLUMN IF EXISTS extra_scopes;
ALTER TABLE sso_configs DROP COLUMN IF EXISTS display_name;

ALTER TABLE sso_configs DROP CONSTRAINT IF EXISTS sso_configs_provider_check;
ALTER TABLE sso_configs ADD CONSTRAINT sso_configs_provider_check
    CHECK (provider IN ('oidc', 'saml'));
