package rbac

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
)

// mockHierarchyStore implements RBACStore for hierarchy tests.
type mockHierarchyStore struct {
	ancestors   map[uuid.UUID][]CompanyAncestor
	memberships map[string]*Membership // "companyID:userID"
	roles       map[uuid.UUID]Role
	permissions map[uuid.UUID][]Permission
}

func newMockHierarchyStore() *mockHierarchyStore {
	return &mockHierarchyStore{
		ancestors:   make(map[uuid.UUID][]CompanyAncestor),
		memberships: make(map[string]*Membership),
		roles:       make(map[uuid.UUID]Role),
		permissions: make(map[uuid.UUID][]Permission),
	}
}

func (m *mockHierarchyStore) GetRoleByID(_ context.Context, id uuid.UUID) (Role, error) {
	r, ok := m.roles[id]
	if !ok {
		return Role{}, fmt.Errorf("not found")
	}
	return r, nil
}

func (m *mockHierarchyStore) ListRolesByCompany(_ context.Context, _ uuid.UUID) ([]Role, error) {
	var roles []Role
	for _, r := range m.roles {
		roles = append(roles, r)
	}
	return roles, nil
}

func (m *mockHierarchyStore) CreateRole(_ context.Context, companyID uuid.UUID, name, description string, level RoleLevel) (Role, error) {
	r := Role{ID: uuid.New(), CompanyID: &companyID, Name: name, Description: description, Level: level}
	m.roles[r.ID] = r
	return r, nil
}

func (m *mockHierarchyStore) ListPermissionsByRole(_ context.Context, roleID uuid.UUID) ([]Permission, error) {
	return m.permissions[roleID], nil
}

func (m *mockHierarchyStore) ListAllPermissions(_ context.Context) ([]Permission, error) {
	return nil, nil
}

func (m *mockHierarchyStore) AddRolePermission(_ context.Context, roleID, permissionID uuid.UUID) error {
	return nil
}

func (m *mockHierarchyStore) GetUserRole(_ context.Context, companyID, userID uuid.UUID) (Role, error) {
	key := companyID.String() + ":" + userID.String()
	mem, ok := m.memberships[key]
	if !ok {
		return Role{}, fmt.Errorf("not found")
	}
	return m.roles[mem.RoleID], nil
}

func (m *mockHierarchyStore) GetMembership(_ context.Context, companyID, userID uuid.UUID) (Membership, error) {
	key := companyID.String() + ":" + userID.String()
	mem, ok := m.memberships[key]
	if !ok {
		return Membership{}, fmt.Errorf("not found")
	}
	return *mem, nil
}

func (m *mockHierarchyStore) GetUserPermissions(_ context.Context, _, _ uuid.UUID) ([]Permission, error) {
	return nil, nil
}

func (m *mockHierarchyStore) AssignRoleToUser(_ context.Context, companyID, userID, roleID uuid.UUID) error {
	key := companyID.String() + ":" + userID.String()
	role := m.roles[roleID]
	m.memberships[key] = &Membership{
		CompanyID: companyID, UserID: userID, RoleID: roleID,
		RoleName: role.Name, RoleLevel: role.Level,
	}
	return nil
}

func (m *mockHierarchyStore) CreateMembership(_ context.Context, companyID, userID, roleID uuid.UUID) error {
	return m.AssignRoleToUser(context.Background(), companyID, userID, roleID)
}

func (m *mockHierarchyStore) GetCompanyAncestors(_ context.Context, companyID uuid.UUID) ([]CompanyAncestor, error) {
	return m.ancestors[companyID], nil
}

func (m *mockHierarchyStore) GetMembershipInCompany(_ context.Context, companyID, userID uuid.UUID) (*Membership, error) {
	key := companyID.String() + ":" + userID.String()
	mem, ok := m.memberships[key]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return mem, nil
}

func TestHierarchyResolver_DirectMembership(t *testing.T) {
	store := newMockHierarchyStore()
	resolver := NewHierarchyResolver(store)
	ctx := context.Background()

	companyID := uuid.New()
	userID := uuid.New()
	roleID := uuid.New()
	store.roles[roleID] = Role{ID: roleID, Name: "admin", Level: RoleLevelAdmin}
	store.memberships[companyID.String()+":"+userID.String()] = &Membership{
		CompanyID: companyID, UserID: userID, RoleID: roleID,
		RoleName: "admin", RoleLevel: RoleLevelAdmin,
	}

	resolved, err := resolver.ResolveMembership(ctx, companyID, userID)
	if err != nil {
		t.Fatalf("ResolveMembership() error = %v", err)
	}
	if !resolved.IsDirect {
		t.Errorf("IsDirect = false, want true")
	}
	if resolved.CappedLevel != RoleLevelAdmin {
		t.Errorf("CappedLevel = %d, want %d", resolved.CappedLevel, RoleLevelAdmin)
	}
	if resolved.AccessLevel != "full" {
		t.Errorf("AccessLevel = %q, want 'full'", resolved.AccessLevel)
	}
}

func TestHierarchyResolver_InheritedFromParent(t *testing.T) {
	store := newMockHierarchyStore()
	resolver := NewHierarchyResolver(store)
	ctx := context.Background()

	parentID := uuid.New()
	childID := uuid.New()
	userID := uuid.New()
	roleID := uuid.New()
	store.roles[roleID] = Role{ID: roleID, Name: "admin", Level: RoleLevelAdmin}

	// Membership in parent only
	store.memberships[parentID.String()+":"+userID.String()] = &Membership{
		CompanyID: parentID, UserID: userID, RoleID: roleID,
		RoleName: "admin", RoleLevel: RoleLevelAdmin,
	}

	// Child has parent as ancestor
	store.ancestors[childID] = []CompanyAncestor{
		{CompanyID: parentID, Generations: 1},
	}

	resolved, err := resolver.ResolveMembership(ctx, childID, userID)
	if err != nil {
		t.Fatalf("ResolveMembership() error = %v", err)
	}
	if resolved.IsDirect {
		t.Errorf("IsDirect = true, want false")
	}
	if resolved.SourceCompany != parentID {
		t.Errorf("SourceCompany = %v, want %v", resolved.SourceCompany, parentID)
	}
}

func TestHierarchyResolver_RoleCap(t *testing.T) {
	store := newMockHierarchyStore()
	resolver := NewHierarchyResolver(store)
	ctx := context.Background()

	parentID := uuid.New()
	childID := uuid.New()
	userID := uuid.New()
	roleID := uuid.New()
	store.roles[roleID] = Role{ID: roleID, Name: "admin", Level: RoleLevelAdmin}

	store.memberships[parentID.String()+":"+userID.String()] = &Membership{
		CompanyID: parentID, UserID: userID, RoleID: roleID,
		RoleName: "admin", RoleLevel: RoleLevelAdmin,
	}

	cap := 40
	store.ancestors[childID] = []CompanyAncestor{
		{CompanyID: parentID, Generations: 1, InheritedRoleCap: &cap},
	}

	resolved, err := resolver.ResolveMembership(ctx, childID, userID)
	if err != nil {
		t.Fatalf("ResolveMembership() error = %v", err)
	}
	if resolved.CappedLevel != 40 {
		t.Errorf("CappedLevel = %d, want 40", resolved.CappedLevel)
	}
	if resolved.RoleLevel != RoleLevelAdmin {
		t.Errorf("RoleLevel = %d, want %d (original)", resolved.RoleLevel, RoleLevelAdmin)
	}
}

func TestHierarchyResolver_AccessLevelNone(t *testing.T) {
	store := newMockHierarchyStore()
	resolver := NewHierarchyResolver(store)
	ctx := context.Background()

	parentID := uuid.New()
	grandparentID := uuid.New()
	childID := uuid.New()
	userID := uuid.New()
	roleID := uuid.New()
	store.roles[roleID] = Role{ID: roleID, Name: "admin", Level: RoleLevelAdmin}

	// Membership in both parent and grandparent
	store.memberships[parentID.String()+":"+userID.String()] = &Membership{
		CompanyID: parentID, UserID: userID, RoleID: roleID,
		RoleName: "admin", RoleLevel: RoleLevelAdmin,
	}
	store.memberships[grandparentID.String()+":"+userID.String()] = &Membership{
		CompanyID: grandparentID, UserID: userID, RoleID: roleID,
		RoleName: "admin", RoleLevel: RoleLevelAdmin,
	}

	noneAccess := "none"
	store.ancestors[childID] = []CompanyAncestor{
		{CompanyID: parentID, Generations: 1, AccessLevel: &noneAccess},
		{CompanyID: grandparentID, Generations: 2},
	}

	resolved, err := resolver.ResolveMembership(ctx, childID, userID)
	if err != nil {
		t.Fatalf("ResolveMembership() error = %v", err)
	}
	if resolved.SourceCompany != grandparentID {
		t.Errorf("SourceCompany = %v, want grandparent %v (parent should be skipped due to 'none')", resolved.SourceCompany, grandparentID)
	}
}

func TestHierarchyResolver_NoMembership(t *testing.T) {
	store := newMockHierarchyStore()
	resolver := NewHierarchyResolver(store)
	ctx := context.Background()

	_, err := resolver.ResolveMembership(ctx, uuid.New(), uuid.New())
	if err == nil {
		t.Fatalf("ResolveMembership() expected error, got nil")
	}
}

func TestHierarchyResolver_CanAccessCompany(t *testing.T) {
	store := newMockHierarchyStore()
	resolver := NewHierarchyResolver(store)
	ctx := context.Background()

	companyID := uuid.New()
	userID := uuid.New()
	roleID := uuid.New()
	store.roles[roleID] = Role{ID: roleID, Name: "member", Level: RoleLevelMember}
	store.memberships[companyID.String()+":"+userID.String()] = &Membership{
		CompanyID: companyID, UserID: userID, RoleID: roleID,
		RoleName: "member", RoleLevel: RoleLevelMember,
	}

	ok, err := resolver.CanAccessCompany(ctx, companyID, userID)
	if err != nil {
		t.Fatalf("CanAccessCompany() error = %v", err)
	}
	if !ok {
		t.Errorf("CanAccessCompany() = false, want true")
	}

	// Unknown user
	ok, err = resolver.CanAccessCompany(ctx, companyID, uuid.New())
	if err != nil {
		t.Fatalf("CanAccessCompany() error = %v", err)
	}
	if ok {
		t.Errorf("CanAccessCompany() for unknown user = true, want false")
	}
}

func TestHierarchyResolver_GetEffectiveRoleLevel(t *testing.T) {
	store := newMockHierarchyStore()
	resolver := NewHierarchyResolver(store)
	ctx := context.Background()

	parentID := uuid.New()
	childID := uuid.New()
	userID := uuid.New()
	roleID := uuid.New()
	store.roles[roleID] = Role{ID: roleID, Name: "admin", Level: RoleLevelAdmin}
	store.memberships[parentID.String()+":"+userID.String()] = &Membership{
		CompanyID: parentID, UserID: userID, RoleID: roleID,
		RoleName: "admin", RoleLevel: RoleLevelAdmin,
	}

	cap := 40
	store.ancestors[childID] = []CompanyAncestor{
		{CompanyID: parentID, Generations: 1, InheritedRoleCap: &cap},
	}

	level, err := resolver.GetEffectiveRoleLevel(ctx, childID, userID)
	if err != nil {
		t.Fatalf("GetEffectiveRoleLevel() error = %v", err)
	}
	if level != 40 {
		t.Errorf("GetEffectiveRoleLevel() = %d, want 40", level)
	}
}
