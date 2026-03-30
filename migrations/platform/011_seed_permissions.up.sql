-- Seed all feature permissions
INSERT INTO permissions (id, feature, action, description) VALUES
    -- Content
    (gen_random_uuid(), 'content', 'view', 'View content'),
    (gen_random_uuid(), 'content', 'create', 'Create content'),
    (gen_random_uuid(), 'content', 'edit', 'Edit content'),
    (gen_random_uuid(), 'content', 'delete', 'Delete content'),
    -- Glossary
    (gen_random_uuid(), 'glossary', 'view', 'View glossary'),
    (gen_random_uuid(), 'glossary', 'create', 'Create glossary terms'),
    (gen_random_uuid(), 'glossary', 'edit', 'Edit glossary terms'),
    (gen_random_uuid(), 'glossary', 'delete', 'Delete glossary terms'),
    -- Library
    (gen_random_uuid(), 'library', 'view', 'View library'),
    (gen_random_uuid(), 'library', 'create', 'Create library items'),
    (gen_random_uuid(), 'library', 'edit', 'Edit library items'),
    (gen_random_uuid(), 'library', 'delete', 'Delete library items'),
    -- LMS
    (gen_random_uuid(), 'lms', 'view', 'View LMS data'),
    (gen_random_uuid(), 'lms', 'create', 'Create courses'),
    (gen_random_uuid(), 'lms', 'edit', 'Edit courses'),
    (gen_random_uuid(), 'lms', 'delete', 'Delete courses'),
    -- Scheduling
    (gen_random_uuid(), 'scheduling', 'view', 'View scheduling data'),
    (gen_random_uuid(), 'scheduling', 'create', 'Create events'),
    (gen_random_uuid(), 'scheduling', 'edit', 'Edit events'),
    (gen_random_uuid(), 'scheduling', 'delete', 'Delete events'),
    -- Audience
    (gen_random_uuid(), 'audience', 'view', 'View audience data'),
    (gen_random_uuid(), 'audience', 'create', 'Create audience records'),
    (gen_random_uuid(), 'audience', 'edit', 'Edit audience records'),
    (gen_random_uuid(), 'audience', 'delete', 'Delete audience records'),
    -- Brand
    (gen_random_uuid(), 'brand', 'view', 'View brand data'),
    (gen_random_uuid(), 'brand', 'create', 'Create brand records'),
    (gen_random_uuid(), 'brand', 'edit', 'Edit brand records'),
    (gen_random_uuid(), 'brand', 'delete', 'Delete brand records'),
    -- AI
    (gen_random_uuid(), 'ai', 'view', 'View AI features'),
    (gen_random_uuid(), 'ai', 'admin', 'Admin AI features'),
    -- Settings
    (gen_random_uuid(), 'settings', 'view', 'View settings'),
    (gen_random_uuid(), 'settings', 'edit', 'Edit settings'),
    (gen_random_uuid(), 'settings', 'admin', 'Admin settings')
ON CONFLICT (feature, action) DO NOTHING;

-- Grant ALL permissions to owner role (10000000-0000-0000-0000-000000000001)
INSERT INTO role_permissions (role_id, permission_id)
SELECT '10000000-0000-0000-0000-000000000001'::uuid, id FROM permissions
ON CONFLICT DO NOTHING;

-- Grant ALL permissions to admin role (10000000-0000-0000-0000-000000000002)
INSERT INTO role_permissions (role_id, permission_id)
SELECT '10000000-0000-0000-0000-000000000002'::uuid, id FROM permissions
ON CONFLICT DO NOTHING;

-- Grant ALL permissions to super_admin role (10000000-0000-0000-0000-000000000000)
INSERT INTO role_permissions (role_id, permission_id)
SELECT '10000000-0000-0000-0000-000000000000'::uuid, id FROM permissions
ON CONFLICT DO NOTHING;

-- Grant view-only permissions to manager role (10000000-0000-0000-0000-000000000005)
INSERT INTO role_permissions (role_id, permission_id)
SELECT '10000000-0000-0000-0000-000000000005'::uuid, id FROM permissions
WHERE action = 'view'
ON CONFLICT DO NOTHING;

-- Grant view-only permissions to member role (10000000-0000-0000-0000-000000000003)
INSERT INTO role_permissions (role_id, permission_id)
SELECT '10000000-0000-0000-0000-000000000003'::uuid, id FROM permissions
WHERE action = 'view'
ON CONFLICT DO NOTHING;
