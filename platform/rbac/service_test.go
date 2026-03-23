package rbac_test

import (
	"context"
	"testing"

	"github.com/aocybersystems/eden-platform-go/platform/devstore"
	"github.com/aocybersystems/eden-platform-go/platform/rbac"
	"github.com/google/uuid"
)

func setupRBACService(t *testing.T) (*rbac.Service, *devstore.Backend, uuid.UUID, uuid.UUID) {
	t.Helper()
	backend := devstore.NewMemoryBackend()
	seedSystemRoles(backend)
	rbacStore := backend.RBACStore()

	enforcer := rbac.NewEnforcer(rbacStore, nil)
	resolver := rbac.NewHierarchyResolver(rbacStore)
	svc := rbac.NewService(rbacStore, enforcer, resolver)

	companyID := uuid.New()
	ownerID := uuid.New()
	ctx := context.Background()
	if err := rbacStore.CreateMembership(ctx, companyID, ownerID, rbac.OwnerRoleID); err != nil {
		t.Fatalf("CreateMembership(owner) error = %v", err)
	}

	return svc, backend, companyID, ownerID
}

func TestRBACService_ListRoles(t *testing.T) {
	svc, _, companyID, _ := setupRBACService(t)
	ctx := context.Background()

	roles, err := svc.ListRoles(ctx, companyID)
	if err != nil {
		t.Fatalf("ListRoles() error = %v", err)
	}
	if len(roles) < 4 {
		t.Errorf("ListRoles() returned %d roles, want at least 4 system roles", len(roles))
	}
}

func TestRBACService_CreateRole(t *testing.T) {
	svc, backend, companyID, _ := setupRBACService(t)
	ctx := context.Background()

	permID := uuid.New()
	backend.SeedRBACPermission(rbac.Permission{ID: permID, Feature: "custom", Action: "read"})

	role, err := svc.CreateRole(ctx, companyID, "custom-role", "A custom role", rbac.RoleLevelMember, []uuid.UUID{permID})
	if err != nil {
		t.Fatalf("CreateRole() error = %v", err)
	}
	if role.Name != "custom-role" {
		t.Errorf("Role.Name = %q, want %q", role.Name, "custom-role")
	}
	if role.Level != rbac.RoleLevelMember {
		t.Errorf("Role.Level = %d, want %d", role.Level, rbac.RoleLevelMember)
	}
}

func TestRBACService_AssignRole_Success(t *testing.T) {
	svc, backend, companyID, ownerID := setupRBACService(t)
	ctx := context.Background()

	userID := uuid.New()
	rbacStore := backend.RBACStore()
	if err := rbacStore.CreateMembership(ctx, companyID, userID, rbac.ViewerRoleID); err != nil {
		t.Fatalf("CreateMembership() error = %v", err)
	}

	// Owner assigns admin role to user
	err := svc.AssignRole(ctx, companyID, userID, rbac.AdminRoleID, ownerID)
	if err != nil {
		t.Fatalf("AssignRole() error = %v", err)
	}

	// Verify role was changed
	role, err := rbacStore.GetUserRole(ctx, companyID, userID)
	if err != nil {
		t.Fatalf("GetUserRole() error = %v", err)
	}
	if role.ID != rbac.AdminRoleID {
		t.Errorf("Role.ID = %v, want admin %v", role.ID, rbac.AdminRoleID)
	}
}

func TestRBACService_AssignRole_OwnerTransfer_Denied(t *testing.T) {
	svc, backend, companyID, _ := setupRBACService(t)
	ctx := context.Background()

	// Create a non-owner user
	nonOwnerID := uuid.New()
	rbacStore := backend.RBACStore()
	if err := rbacStore.CreateMembership(ctx, companyID, nonOwnerID, rbac.AdminRoleID); err != nil {
		t.Fatalf("CreateMembership() error = %v", err)
	}

	targetID := uuid.New()
	if err := rbacStore.CreateMembership(ctx, companyID, targetID, rbac.ViewerRoleID); err != nil {
		t.Fatalf("CreateMembership() error = %v", err)
	}

	// Non-owner tries to assign owner role
	err := svc.AssignRole(ctx, companyID, targetID, rbac.OwnerRoleID, nonOwnerID)
	if err == nil {
		t.Fatalf("AssignRole(owner) by non-owner expected error, got nil")
	}
}

func TestRBACService_RemoveRole_Success(t *testing.T) {
	svc, backend, companyID, _ := setupRBACService(t)
	ctx := context.Background()

	userID := uuid.New()
	rbacStore := backend.RBACStore()
	if err := rbacStore.CreateMembership(ctx, companyID, userID, rbac.AdminRoleID); err != nil {
		t.Fatalf("CreateMembership() error = %v", err)
	}

	err := svc.RemoveRole(ctx, companyID, userID, rbac.AdminRoleID)
	if err != nil {
		t.Fatalf("RemoveRole() error = %v", err)
	}

	// Verify role was reset to member
	role, err := rbacStore.GetUserRole(ctx, companyID, userID)
	if err != nil {
		t.Fatalf("GetUserRole() error = %v", err)
	}
	if role.ID != rbac.MemberRoleID {
		t.Errorf("Role.ID = %v, want member %v", role.ID, rbac.MemberRoleID)
	}
}

func TestRBACService_RemoveRole_Owner_Denied(t *testing.T) {
	svc, _, companyID, ownerID := setupRBACService(t)
	ctx := context.Background()

	err := svc.RemoveRole(ctx, companyID, ownerID, rbac.OwnerRoleID)
	if err == nil {
		t.Fatalf("RemoveRole(owner) expected error, got nil")
	}
}

func TestRBACService_GetUserPermissions(t *testing.T) {
	svc, backend, companyID, _ := setupRBACService(t)
	ctx := context.Background()

	userID := uuid.New()
	rbacStore := backend.RBACStore()
	if err := rbacStore.CreateMembership(ctx, companyID, userID, rbac.MemberRoleID); err != nil {
		t.Fatalf("CreateMembership() error = %v", err)
	}

	// Seed a permission on member role
	permID := uuid.New()
	backend.SeedRBACPermission(rbac.Permission{ID: permID, Feature: "crm", Action: "view"})
	if err := rbacStore.AddRolePermission(ctx, rbac.MemberRoleID, permID); err != nil {
		t.Fatalf("AddRolePermission() error = %v", err)
	}

	perms, role, err := svc.GetUserPermissions(ctx, companyID, userID)
	if err != nil {
		t.Fatalf("GetUserPermissions() error = %v", err)
	}
	if role.Name != "member" {
		t.Errorf("Role.Name = %q, want %q", role.Name, "member")
	}
	if len(perms) == 0 {
		t.Errorf("GetUserPermissions() returned 0 permissions, want at least 1")
	}
}
