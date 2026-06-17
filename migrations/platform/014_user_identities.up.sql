-- User social identities (consumer social login).
-- User-scoped only: NO company_id. This is NOT the company-scoped SSOService.
-- One row per (provider, provider_sub). A user may have several identities.
CREATE TABLE IF NOT EXISTS user_identities (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider      TEXT NOT NULL,                  -- 'google'|'apple'|'microsoft'|'facebook'|'x'
    provider_sub  TEXT NOT NULL,                  -- provider's stable user identifier (sub/id)
    email         TEXT,                           -- may be NULL (X always; Facebook if declined)
    is_verified   BOOLEAN NOT NULL DEFAULT false, -- was the email verified by the provider
    display_name  TEXT,
    avatar_url    TEXT,
    raw_claims    JSONB,                          -- identity claims for audit; NEVER provider tokens
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider, provider_sub)
);

CREATE INDEX idx_user_identities_user  ON user_identities (user_id);
CREATE INDEX idx_user_identities_email ON user_identities (email) WHERE email IS NOT NULL;
