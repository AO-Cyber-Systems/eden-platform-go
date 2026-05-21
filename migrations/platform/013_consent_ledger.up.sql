-- Append-only COPPA / GDPR-K consent ledger.
-- Mutations to existing rows are forbidden; revocations are NEW rows whose
-- revokes_id points at the original grant row.
CREATE TABLE IF NOT EXISTS consent_ledger (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES platform_households(id),
    principal_member_id UUID NOT NULL REFERENCES platform_household_members(id),
    consenter_member_id UUID NOT NULL REFERENCES platform_household_members(id),
    purpose TEXT NOT NULL,
    scope JSONB NOT NULL DEFAULT '{}',
    consent_text_version TEXT NOT NULL DEFAULT '',
    evidence JSONB NOT NULL DEFAULT '{}',
    granted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revokes_id UUID REFERENCES consent_ledger(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_consent_ledger_principal_purpose
    ON consent_ledger (principal_member_id, purpose, granted_at DESC);
CREATE INDEX idx_consent_ledger_household
    ON consent_ledger (household_id, granted_at DESC);
CREATE INDEX idx_consent_ledger_revokes
    ON consent_ledger (revokes_id) WHERE revokes_id IS NOT NULL;

-- Append-only enforcement: deny UPDATE / DELETE on existing rows.
-- TRUNCATE bypasses row-level triggers so test cleanup is still possible.
CREATE OR REPLACE FUNCTION consent_ledger_append_only()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'consent_ledger is append-only; UPDATE / DELETE denied';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER consent_ledger_no_update
BEFORE UPDATE ON consent_ledger
FOR EACH ROW EXECUTE FUNCTION consent_ledger_append_only();

CREATE TRIGGER consent_ledger_no_delete
BEFORE DELETE ON consent_ledger
FOR EACH ROW EXECUTE FUNCTION consent_ledger_append_only();
