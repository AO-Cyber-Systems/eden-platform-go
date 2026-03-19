-- Encrypted credentials with blind indexing
CREATE TABLE IF NOT EXISTS encrypted_credentials (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    credential_type TEXT NOT NULL,
    encrypted_value BYTEA NOT NULL,
    blind_index TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (company_id, credential_type)
);

CREATE INDEX idx_encrypted_credentials_company ON encrypted_credentials (company_id);
CREATE INDEX idx_encrypted_credentials_blind_index ON encrypted_credentials (blind_index);
