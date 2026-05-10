-- Households: a billable / governable group (Eden Family family-plan target,
-- AOFamily-AI parent-of-record / child-account anchor).
CREATE TABLE IF NOT EXISTS households (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    primary_contact_user_id UUID NOT NULL REFERENCES users(id),
    display_name TEXT NOT NULL DEFAULT '',
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_households_primary_contact ON households (primary_contact_user_id);

-- Members: an individual associated with a household.
-- role: 'parent' | 'child' | 'guardian' | 'adult_non_parent' | 'other'
-- status: 'pending' | 'active' | 'removed'
CREATE TABLE IF NOT EXISTS household_members (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    household_id UUID NOT NULL REFERENCES households(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id),
    role TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active',
    birthdate DATE,
    capabilities JSONB NOT NULL DEFAULT '{}',
    added_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    removed_at TIMESTAMPTZ,
    UNIQUE (household_id, user_id)
);
CREATE INDEX idx_household_members_household ON household_members (household_id) WHERE status != 'removed';
CREATE INDEX idx_household_members_user ON household_members (user_id) WHERE status != 'removed';

-- Parent-of-record: legally responsible parent for a child member (COPPA / GDPR-K).
-- Multiple parents-of-record per child allowed (split households).
CREATE TABLE IF NOT EXISTS parent_of_record (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    child_member_id UUID NOT NULL REFERENCES household_members(id) ON DELETE CASCADE,
    parent_member_id UUID NOT NULL REFERENCES household_members(id) ON DELETE CASCADE,
    established_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at TIMESTAMPTZ,
    UNIQUE (child_member_id, parent_member_id)
);
CREATE INDEX idx_parent_of_record_child ON parent_of_record (child_member_id) WHERE revoked_at IS NULL;
CREATE INDEX idx_parent_of_record_parent ON parent_of_record (parent_member_id) WHERE revoked_at IS NULL;
