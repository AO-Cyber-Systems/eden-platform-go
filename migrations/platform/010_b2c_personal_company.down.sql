-- Remove 'personal' from allowed company_type values
UPDATE companies SET company_type = 'standalone' WHERE company_type = 'personal';
ALTER TABLE companies DROP CONSTRAINT IF EXISTS companies_company_type_check;
ALTER TABLE companies ADD CONSTRAINT companies_company_type_check
    CHECK (company_type IN ('holding', 'subsidiary', 'brand', 'standalone'));
