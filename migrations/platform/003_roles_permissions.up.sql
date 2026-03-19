-- Roles table
CREATE TABLE IF NOT EXISTS roles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID REFERENCES companies(id),
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    level INTEGER NOT NULL DEFAULT 40,
    is_system BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_roles_company ON roles (company_id);

-- Seed system roles
INSERT INTO roles (id, name, description, level, is_system) VALUES
    ('10000000-0000-0000-0000-000000000000', 'super_admin', 'Super Administrator with full platform access', 100, true),
    ('10000000-0000-0000-0000-000000000001', 'owner', 'Company owner with full access', 90, true),
    ('10000000-0000-0000-0000-000000000002', 'admin', 'Company administrator', 80, true),
    ('10000000-0000-0000-0000-000000000005', 'manager', 'Team manager', 60, true),
    ('10000000-0000-0000-0000-000000000003', 'member', 'Standard member', 40, true),
    ('10000000-0000-0000-0000-000000000004', 'viewer', 'Read-only viewer', 20, true)
ON CONFLICT (id) DO NOTHING;

-- Permissions table
CREATE TABLE IF NOT EXISTS permissions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    feature TEXT NOT NULL,
    action TEXT NOT NULL,
    resource TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    UNIQUE (feature, action)
);

-- Role-permission mapping
CREATE TABLE IF NOT EXISTS role_permissions (
    role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id UUID NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);

CREATE INDEX idx_role_permissions_role ON role_permissions (role_id);

-- Company memberships
CREATE TABLE IF NOT EXISTS company_memberships (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id UUID NOT NULL REFERENCES companies(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id UUID NOT NULL REFERENCES roles(id),
    permission_overrides JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (company_id, user_id)
);

CREATE INDEX idx_company_memberships_user ON company_memberships (user_id);
CREATE INDEX idx_company_memberships_company ON company_memberships (company_id);
CREATE INDEX idx_company_memberships_role ON company_memberships (role_id);
