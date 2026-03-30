-- Add 'personal' to allowed company_type values for B2C mode
ALTER TABLE companies DROP CONSTRAINT IF EXISTS companies_company_type_check;
ALTER TABLE companies ADD CONSTRAINT companies_company_type_check
    CHECK (company_type IN ('holding', 'subsidiary', 'brand', 'standalone', 'personal'));
