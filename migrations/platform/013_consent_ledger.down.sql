DROP TRIGGER IF EXISTS consent_ledger_no_delete ON consent_ledger;
DROP TRIGGER IF EXISTS consent_ledger_no_update ON consent_ledger;
DROP FUNCTION IF EXISTS consent_ledger_append_only();
DROP TABLE IF EXISTS consent_ledger;
