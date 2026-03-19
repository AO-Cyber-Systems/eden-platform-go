-- Companies table with hierarchy support
CREATE TABLE IF NOT EXISTS companies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    parent_company_id UUID REFERENCES companies(id),
    company_type TEXT NOT NULL DEFAULT 'standalone'
        CHECK (company_type IN ('holding', 'subsidiary', 'brand', 'standalone')),
    inherited_role_cap INTEGER,
    inherited_access_level TEXT CHECK (inherited_access_level IN ('full', 'read_only', 'none')),
    settings JSONB NOT NULL DEFAULT '{"enabled_features": []}',
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_companies_slug ON companies (slug);
CREATE INDEX idx_companies_parent ON companies (parent_company_id);
CREATE INDEX idx_companies_type ON companies (company_type);

-- Company hierarchy closure table
CREATE TABLE IF NOT EXISTS company_hierarchies (
    ancestor_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    descendant_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    generations INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (ancestor_id, descendant_id)
);

CREATE INDEX idx_company_hierarchies_descendant ON company_hierarchies (descendant_id);
CREATE INDEX idx_company_hierarchies_ancestor ON company_hierarchies (ancestor_id);
CREATE INDEX idx_company_hierarchies_generations ON company_hierarchies (descendant_id, generations);
