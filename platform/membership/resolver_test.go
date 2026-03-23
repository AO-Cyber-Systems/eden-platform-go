package membership

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
)

// mockMembershipStore implements MembershipStore for testing.
type mockMembershipStore struct {
	memberships map[string]*MembershipRecord // "companyID:userID"
	ancestors   map[uuid.UUID][]AncestorInfo // companyID -> ancestors
	companyIDs  map[uuid.UUID][]uuid.UUID    // userID -> companyIDs
}

func newMockStore() *mockMembershipStore {
	return &mockMembershipStore{
		memberships: make(map[string]*MembershipRecord),
		ancestors:   make(map[uuid.UUID][]AncestorInfo),
		companyIDs:  make(map[uuid.UUID][]uuid.UUID),
	}
}

func (m *mockMembershipStore) GetDirectMembership(_ context.Context, companyID, userID uuid.UUID) (*MembershipRecord, error) {
	key := companyID.String() + ":" + userID.String()
	rec, ok := m.memberships[key]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return rec, nil
}

func (m *mockMembershipStore) GetCompanyAncestorChain(_ context.Context, companyID uuid.UUID) ([]AncestorInfo, error) {
	chain, ok := m.ancestors[companyID]
	if !ok {
		return nil, nil
	}
	return chain, nil
}

func (m *mockMembershipStore) ListUserCompanyIDs(_ context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	ids, ok := m.companyIDs[userID]
	if !ok {
		return nil, nil
	}
	return ids, nil
}

func (m *mockMembershipStore) addMembership(companyID, userID, roleID uuid.UUID, roleName string, roleLevel int) {
	key := companyID.String() + ":" + userID.String()
	m.memberships[key] = &MembershipRecord{
		CompanyID: companyID,
		UserID:    userID,
		RoleID:    roleID,
		RoleName:  roleName,
		RoleLevel: roleLevel,
	}
}

func TestResolver_DirectMembership(t *testing.T) {
	store := newMockStore()
	resolver := NewResolver(store)
	ctx := context.Background()

	companyID := uuid.New()
	userID := uuid.New()
	store.addMembership(companyID, userID, uuid.New(), "admin", 80)

	resolved, err := resolver.Resolve(ctx, companyID, userID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if !resolved.IsDirect {
		t.Errorf("IsDirect = false, want true")
	}
	if resolved.CappedLevel != 80 {
		t.Errorf("CappedLevel = %d, want 80", resolved.CappedLevel)
	}
	if resolved.AccessLevel != "full" {
		t.Errorf("AccessLevel = %q, want 'full'", resolved.AccessLevel)
	}
	if resolved.SourceCompany != companyID {
		t.Errorf("SourceCompany = %v, want %v", resolved.SourceCompany, companyID)
	}
}

func TestResolver_InheritedMembership(t *testing.T) {
	store := newMockStore()
	resolver := NewResolver(store)
	ctx := context.Background()

	parentID := uuid.New()
	childID := uuid.New()
	userID := uuid.New()

	// User has membership in parent, not in child
	store.addMembership(parentID, userID, uuid.New(), "admin", 80)

	// Child's ancestor chain points to parent
	store.ancestors[childID] = []AncestorInfo{
		{CompanyID: parentID, Generations: 1, InheritRoleCap: nil, AccessLevel: nil},
	}

	resolved, err := resolver.Resolve(ctx, childID, userID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.IsDirect {
		t.Errorf("IsDirect = true, want false")
	}
	if resolved.SourceCompany != parentID {
		t.Errorf("SourceCompany = %v, want parent %v", resolved.SourceCompany, parentID)
	}
	if resolved.CappedLevel != 80 {
		t.Errorf("CappedLevel = %d, want 80", resolved.CappedLevel)
	}
}

func TestResolver_RoleCap(t *testing.T) {
	store := newMockStore()
	resolver := NewResolver(store)
	ctx := context.Background()

	parentID := uuid.New()
	childID := uuid.New()
	userID := uuid.New()

	store.addMembership(parentID, userID, uuid.New(), "admin", 80)

	roleCap := 40
	store.ancestors[childID] = []AncestorInfo{
		{CompanyID: parentID, Generations: 1, InheritRoleCap: &roleCap, AccessLevel: nil},
	}

	resolved, err := resolver.Resolve(ctx, childID, userID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.CappedLevel != 40 {
		t.Errorf("CappedLevel = %d, want 40 (capped from 80)", resolved.CappedLevel)
	}
	if resolved.RoleLevel != 80 {
		t.Errorf("RoleLevel = %d, want 80 (original)", resolved.RoleLevel)
	}
}

func TestResolver_AccessLevelNone(t *testing.T) {
	store := newMockStore()
	resolver := NewResolver(store)
	ctx := context.Background()

	parentID := uuid.New()
	grandparentID := uuid.New()
	childID := uuid.New()
	userID := uuid.New()

	// User has membership in grandparent only
	store.addMembership(grandparentID, userID, uuid.New(), "admin", 80)

	noneAccess := "none"
	store.ancestors[childID] = []AncestorInfo{
		{CompanyID: parentID, Generations: 1, InheritRoleCap: nil, AccessLevel: &noneAccess},
		{CompanyID: grandparentID, Generations: 2, InheritRoleCap: nil, AccessLevel: nil},
	}
	// Parent has no membership, grandparent does but parent is "none" access
	// Since parent has no membership, it's skipped anyway; grandparent should be found
	store.addMembership(parentID, userID, uuid.New(), "member", 40)

	resolved, err := resolver.Resolve(ctx, childID, userID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	// Parent ancestor has access="none", so skipped; falls through to grandparent
	if resolved.SourceCompany != grandparentID {
		t.Errorf("SourceCompany = %v, want grandparent %v (parent should be skipped due to 'none' access)", resolved.SourceCompany, grandparentID)
	}
}

func TestResolver_AccessLevelReadOnly(t *testing.T) {
	store := newMockStore()
	resolver := NewResolver(store)
	ctx := context.Background()

	parentID := uuid.New()
	childID := uuid.New()
	userID := uuid.New()

	store.addMembership(parentID, userID, uuid.New(), "admin", 80)

	readOnly := "read_only"
	store.ancestors[childID] = []AncestorInfo{
		{CompanyID: parentID, Generations: 1, InheritRoleCap: nil, AccessLevel: &readOnly},
	}

	resolved, err := resolver.Resolve(ctx, childID, userID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.AccessLevel != "read_only" {
		t.Errorf("AccessLevel = %q, want 'read_only'", resolved.AccessLevel)
	}
}

func TestResolver_NoMembership(t *testing.T) {
	store := newMockStore()
	resolver := NewResolver(store)
	ctx := context.Background()

	companyID := uuid.New()
	userID := uuid.New()

	_, err := resolver.Resolve(ctx, companyID, userID)
	if err == nil {
		t.Fatalf("Resolve() expected error for no membership, got nil")
	}
}

func TestResolver_ListAccessibleCompanies(t *testing.T) {
	store := newMockStore()
	resolver := NewResolver(store)
	ctx := context.Background()

	userID := uuid.New()
	company1 := uuid.New()
	company2 := uuid.New()
	store.companyIDs[userID] = []uuid.UUID{company1, company2}

	ids, err := resolver.ListAccessibleCompanies(ctx, userID)
	if err != nil {
		t.Fatalf("ListAccessibleCompanies() error = %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("ListAccessibleCompanies() returned %d, want 2", len(ids))
	}
}
