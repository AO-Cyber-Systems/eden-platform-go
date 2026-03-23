package rbac_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aocybersystems/eden-platform-go/platform/devstore"
	"github.com/aocybersystems/eden-platform-go/platform/rbac"
	"github.com/google/uuid"
)

func seedSystemRoles(backend *devstore.Backend) {
	backend.SeedRBACRole(rbac.Role{ID: rbac.OwnerRoleID, Name: "owner", Level: rbac.RoleLevelOwner, IsSystem: true})
	backend.SeedRBACRole(rbac.Role{ID: rbac.AdminRoleID, Name: "admin", Level: rbac.RoleLevelAdmin, IsSystem: true})
	backend.SeedRBACRole(rbac.Role{ID: rbac.MemberRoleID, Name: "member", Level: rbac.RoleLevelMember, IsSystem: true})
	backend.SeedRBACRole(rbac.Role{ID: rbac.ViewerRoleID, Name: "viewer", Level: rbac.RoleLevelViewer, IsSystem: true})
}

func setupEnforcer(t *testing.T) (*rbac.Enforcer, *devstore.Backend, uuid.UUID, uuid.UUID) {
	t.Helper()
	backend := devstore.NewMemoryBackend()
	seedSystemRoles(backend)

	companyID := uuid.New()
	userID := uuid.New()
	rbacStore := backend.RBACStore()

	// Create membership as member
	ctx := context.Background()
	if err := rbacStore.CreateMembership(ctx, companyID, userID, rbac.MemberRoleID); err != nil {
		t.Fatalf("CreateMembership() error = %v", err)
	}

	// Seed permissions and assign to member role
	crmViewPerm := rbac.Permission{ID: uuid.New(), Feature: "crm", Action: "view"}
	crmEditPerm := rbac.Permission{ID: uuid.New(), Feature: "crm", Action: "edit"}
	backend.SeedRBACPermission(crmViewPerm)
	backend.SeedRBACPermission(crmEditPerm)
	if err := rbacStore.AddRolePermission(ctx, rbac.MemberRoleID, crmViewPerm.ID); err != nil {
		t.Fatalf("AddRolePermission(view) error = %v", err)
	}
	if err := rbacStore.AddRolePermission(ctx, rbac.MemberRoleID, crmEditPerm.ID); err != nil {
		t.Fatalf("AddRolePermission(edit) error = %v", err)
	}

	enforcer := rbac.NewEnforcer(rbacStore, nil)
	return enforcer, backend, userID, companyID
}

func TestEnforcer_HasPermission_Granted(t *testing.T) {
	enforcer, _, userID, companyID := setupEnforcer(t)
	ctx := context.Background()

	allowed, err := enforcer.HasPermission(ctx, userID, companyID, "crm:view")
	if err != nil {
		t.Fatalf("HasPermission() error = %v", err)
	}
	if !allowed {
		t.Errorf("HasPermission('crm:view') = false, want true")
	}
}

func TestEnforcer_HasPermission_Denied(t *testing.T) {
	enforcer, _, userID, companyID := setupEnforcer(t)
	ctx := context.Background()

	allowed, err := enforcer.HasPermission(ctx, userID, companyID, "crm:admin")
	if err != nil {
		t.Fatalf("HasPermission() error = %v", err)
	}
	if allowed {
		t.Errorf("HasPermission('crm:admin') = true, want false")
	}
}

func TestEnforcer_HasPermission_CacheHit(t *testing.T) {
	enforcer, _, userID, companyID := setupEnforcer(t)
	ctx := context.Background()

	// First call populates cache
	allowed1, err := enforcer.HasPermission(ctx, userID, companyID, "crm:view")
	if err != nil {
		t.Fatalf("First HasPermission() error = %v", err)
	}

	// Second call should use cache and return same result
	allowed2, err := enforcer.HasPermission(ctx, userID, companyID, "crm:view")
	if err != nil {
		t.Fatalf("Second HasPermission() error = %v", err)
	}

	if allowed1 != allowed2 {
		t.Errorf("Cache inconsistency: first=%v, second=%v", allowed1, allowed2)
	}
}

func TestEnforcer_HasPermission_Override_Grant(t *testing.T) {
	backend := devstore.NewMemoryBackend()
	seedSystemRoles(backend)
	rbacStore := backend.RBACStore()
	ctx := context.Background()

	companyID := uuid.New()
	userID := uuid.New()

	// Create membership with override granting crm:delete
	if err := rbacStore.CreateMembership(ctx, companyID, userID, rbac.MemberRoleID); err != nil {
		t.Fatalf("CreateMembership() error = %v", err)
	}

	// Need to set override via the backend state directly
	// The rbacMemberships map has the membership; update it with overrides
	overrides, _ := json.Marshal(map[string]bool{"crm:delete": true})
	key := companyID.String() + ":" + userID.String()
	backend.SetRBACMembershipOverrides(key, overrides)

	enforcer := rbac.NewEnforcer(rbacStore, nil)

	allowed, err := enforcer.HasPermission(ctx, userID, companyID, "crm:delete")
	if err != nil {
		t.Fatalf("HasPermission() error = %v", err)
	}
	if !allowed {
		t.Errorf("HasPermission('crm:delete') with grant override = false, want true")
	}
}

func TestEnforcer_HasPermission_Override_Deny(t *testing.T) {
	backend := devstore.NewMemoryBackend()
	seedSystemRoles(backend)
	rbacStore := backend.RBACStore()
	ctx := context.Background()

	companyID := uuid.New()
	userID := uuid.New()

	if err := rbacStore.CreateMembership(ctx, companyID, userID, rbac.MemberRoleID); err != nil {
		t.Fatalf("CreateMembership() error = %v", err)
	}

	// Seed crm:view permission on member role
	perm := rbac.Permission{ID: uuid.New(), Feature: "crm", Action: "view"}
	backend.SeedRBACPermission(perm)
	if err := rbacStore.AddRolePermission(ctx, rbac.MemberRoleID, perm.ID); err != nil {
		t.Fatalf("AddRolePermission() error = %v", err)
	}

	// Override: deny crm:view
	overrides, _ := json.Marshal(map[string]bool{"crm:view": false})
	key := companyID.String() + ":" + userID.String()
	backend.SetRBACMembershipOverrides(key, overrides)

	enforcer := rbac.NewEnforcer(rbacStore, nil)

	allowed, err := enforcer.HasPermission(ctx, userID, companyID, "crm:view")
	if err != nil {
		t.Fatalf("HasPermission() error = %v", err)
	}
	if allowed {
		t.Errorf("HasPermission('crm:view') with deny override = true, want false")
	}
}

func TestEnforcer_HasMinimumRole_Owner(t *testing.T) {
	backend := devstore.NewMemoryBackend()
	seedSystemRoles(backend)
	rbacStore := backend.RBACStore()
	ctx := context.Background()

	companyID := uuid.New()
	userID := uuid.New()
	if err := rbacStore.CreateMembership(ctx, companyID, userID, rbac.OwnerRoleID); err != nil {
		t.Fatalf("CreateMembership() error = %v", err)
	}

	enforcer := rbac.NewEnforcer(rbacStore, nil)
	ok, err := enforcer.HasMinimumRole(ctx, userID, companyID, rbac.RoleLevelAdmin)
	if err != nil {
		t.Fatalf("HasMinimumRole() error = %v", err)
	}
	if !ok {
		t.Errorf("HasMinimumRole(owner, admin) = false, want true")
	}
}

func TestEnforcer_HasMinimumRole_Viewer(t *testing.T) {
	backend := devstore.NewMemoryBackend()
	seedSystemRoles(backend)
	rbacStore := backend.RBACStore()
	ctx := context.Background()

	companyID := uuid.New()
	userID := uuid.New()
	if err := rbacStore.CreateMembership(ctx, companyID, userID, rbac.ViewerRoleID); err != nil {
		t.Fatalf("CreateMembership() error = %v", err)
	}

	enforcer := rbac.NewEnforcer(rbacStore, nil)
	ok, err := enforcer.HasMinimumRole(ctx, userID, companyID, rbac.RoleLevelMember)
	if err != nil {
		t.Fatalf("HasMinimumRole() error = %v", err)
	}
	if ok {
		t.Errorf("HasMinimumRole(viewer, member) = true, want false")
	}
}

func TestEnforcer_CheckFeatureAction(t *testing.T) {
	enforcer, _, userID, companyID := setupEnforcer(t)
	ctx := context.Background()

	// Member (level 40) should pass CRM view (requires RoleLevelViewer=20)
	ok, err := enforcer.CheckFeatureAction(ctx, userID, companyID, rbac.FeatureCRM, "view")
	if err != nil {
		t.Fatalf("CheckFeatureAction(crm:view) error = %v", err)
	}
	if !ok {
		t.Errorf("CheckFeatureAction(crm:view) as member = false, want true")
	}

	// Member (level 40) should fail CRM admin (requires RoleLevelAdmin=80)
	ok, err = enforcer.CheckFeatureAction(ctx, userID, companyID, rbac.FeatureCRM, "admin")
	if err != nil {
		t.Fatalf("CheckFeatureAction(crm:admin) error = %v", err)
	}
	if ok {
		t.Errorf("CheckFeatureAction(crm:admin) as member = true, want false")
	}
}

func TestEnforcer_InvalidateCache(t *testing.T) {
	enforcer, _, userID, companyID := setupEnforcer(t)
	ctx := context.Background()

	// Populate cache
	_, err := enforcer.HasPermission(ctx, userID, companyID, "crm:view")
	if err != nil {
		t.Fatalf("HasPermission() error = %v", err)
	}

	// Invalidate
	enforcer.InvalidateCache(userID, companyID)

	// Next call should still work (re-fetch from store)
	allowed, err := enforcer.HasPermission(ctx, userID, companyID, "crm:view")
	if err != nil {
		t.Fatalf("HasPermission() after invalidate error = %v", err)
	}
	if !allowed {
		t.Errorf("HasPermission('crm:view') after invalidate = false, want true")
	}
}

func TestEnforcer_InvalidateAll(t *testing.T) {
	enforcer, _, userID, companyID := setupEnforcer(t)
	ctx := context.Background()

	// Populate cache
	_, err := enforcer.HasPermission(ctx, userID, companyID, "crm:view")
	if err != nil {
		t.Fatalf("HasPermission() error = %v", err)
	}

	enforcer.InvalidateAll()

	// Should still work
	allowed, err := enforcer.HasPermission(ctx, userID, companyID, "crm:view")
	if err != nil {
		t.Fatalf("HasPermission() after invalidateAll error = %v", err)
	}
	if !allowed {
		t.Errorf("HasPermission('crm:view') after invalidateAll = false, want true")
	}
}

func TestDefaultPermissionMatrix(t *testing.T) {
	matrix := rbac.DefaultPermissionMatrix()

	// Verify CRM feature exists
	crmActions, ok := matrix[rbac.FeatureCRM]
	if !ok {
		t.Fatalf("DefaultPermissionMatrix() missing CRM feature")
	}

	// Verify CRM view requires viewer level
	viewLevel, ok := crmActions["view"]
	if !ok {
		t.Fatalf("DefaultPermissionMatrix() CRM missing 'view' action")
	}
	if viewLevel != rbac.RoleLevelViewer {
		t.Errorf("CRM view level = %d, want %d", viewLevel, rbac.RoleLevelViewer)
	}

	// Verify CRM admin requires admin level
	adminLevel, ok := crmActions["admin"]
	if !ok {
		t.Fatalf("DefaultPermissionMatrix() CRM missing 'admin' action")
	}
	if adminLevel != rbac.RoleLevelAdmin {
		t.Errorf("CRM admin level = %d, want %d", adminLevel, rbac.RoleLevelAdmin)
	}

	// Verify reasonable number of features
	if len(matrix) < 10 {
		t.Errorf("DefaultPermissionMatrix() has %d features, expected at least 10", len(matrix))
	}
}
