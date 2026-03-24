-- Expand SSO configs to support named providers (Microsoft, Google) and enforce SSO.

-- Drop the old CHECK constraint and add a more permissive one.
ALTER TABLE sso_configs DROP CONSTRAINT IF EXISTS sso_configs_provider_check;
ALTER TABLE sso_configs ADD CONSTRAINT sso_configs_provider_check
    CHECK (provider IN ('oidc', 'saml', 'microsoft', 'google'));

-- New columns
ALTER TABLE sso_configs ADD COLUMN IF NOT EXISTS display_name TEXT NOT NULL DEFAULT '';
ALTER TABLE sso_configs ADD COLUMN IF NOT EXISTS extra_scopes TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE sso_configs ADD COLUMN IF NOT EXISTS enforce_sso BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE sso_configs ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- OAuth credentials: store provider access/refresh tokens for API use (email, calendar, directory sync).
CREATE TABLE IF NOT EXISTS oauth_credentials (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,  -- microsoft, google
    access_token TEXT NOT NULL,
    refresh_token TEXT NOT NULL DEFAULT '',
    token_expiry TIMESTAMPTZ,
    scopes TEXT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (company_id, user_id, provider)
);

CREATE INDEX idx_oauth_creds_user ON oauth_credentials (user_id);
CREATE INDEX idx_oauth_creds_company ON oauth_credentials (company_id);
