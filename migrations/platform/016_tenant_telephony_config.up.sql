-- Per-tenant telephony provider configuration. auth_token / webhook_secret
-- are stored ENCRYPTED at rest -- platform/telephony's Encrypter seam
-- (platform/encryption.FieldEncryptor in production, NopEncrypter in
-- dev/test) encrypts before a Go caller ever writes bytes here, and decrypts
-- only after reading them back out.
CREATE TABLE IF NOT EXISTS tenant_telephony_config (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    account_sid TEXT NOT NULL,
    auth_token_encrypted BYTEA NOT NULL,
    webhook_secret_encrypted BYTEA,
    space_url TEXT,
    sending_number TEXT NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_tenant_telephony_config_company
    ON tenant_telephony_config (company_id)
    WHERE is_active = true;

-- A sending number is owned by AT MOST ONE active tenant per provider.
-- LookupBySendingNumber is the inbound-webhook tenant-resolution chokepoint
-- (a status callback or inbound SMS carries only the destination number in
-- its body); a number shared across two active tenants would route inbound
-- traffic to a non-deterministic tenant and verify its signature against the
-- wrong secret. Reconciled against eden-biz/telephonycreds'
-- uq_telephony_credentials_from_number, which enforces the identical
-- invariant for its single-row-per-company model; PostgresConfigStore.Upsert
-- maps the resulting unique violation to ErrSendingNumberClaimed so a second
-- tenant claiming an owned number fails closed instead of silently colliding.
CREATE UNIQUE INDEX uq_tenant_telephony_config_active_number
    ON tenant_telephony_config (provider, sending_number)
    WHERE is_active = true;
