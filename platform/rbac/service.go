package rbac

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// Well-known role IDs
var (
	SuperAdminRoleID = uuid.MustParse("10000000-0000-0000-0000-000000000000")
	OwnerRoleID      = uuid.MustParse("10000000-0000-0000-0000-000000000001")
	AdminRoleID      = uuid.MustParse("10000000-0000-0000-0000-000000000002")
	MemberRoleID     = uuid.MustParse("10000000-0000-0000-0000-000000000003")
	ViewerRoleID     = uuid.MustParse("10000000-0000-0000-0000-000000000004")
	ManagerRoleID    = uuid.MustParse("10000000-0000-0000-0000-000000000005")
)

// Service implements RBAC business logic.
type Service struct {
	store    RBACStore
	enforcer *Enforcer
	resolver *HierarchyResolver
}

// NewService creates a new RBAC service.
func NewService(store RBACStore, enforcer *Enforcer, resolver *HierarchyResolver) *Service {
	return &Service{
		store:    store,
		enforcer: enforcer,
		resolver: resolver,
	}
}

// Enforcer returns the underlying enforcer.
func (s *Service) Enforcer() *Enforcer {
	return s.enforcer
}

// Resolver returns the underlying hierarchy resolver.
func (s *Service) Resolver() *HierarchyResolver {
	return s.resolver
}

// ListRoles returns all roles available to a company (system + custom).
func (s *Service) ListRoles(ctx context.Context, companyID uuid.UUID) ([]Role, error) {
	return s.store.ListRolesByCompany(ctx, companyID)
}

// CreateRole creates a new custom role for a company.
func (s *Service) CreateRole(ctx context.Context, companyID uuid.UUID, name, description string, level RoleLevel, permissionIDs []uuid.UUID) (Role, error) {
	role, err := s.store.CreateRole(ctx, companyID, name, description, level)
	if err != nil {
		return Role{}, fmt.Errorf("create role: %w", err)
	}

	for _, pid := range permissionIDs {
		_ = s.store.AddRolePermission(ctx, role.ID, pid)
	}

	return role, nil
}

// AssignRole assigns a role to a user. Prevents assigning owner unless current user is owner.
func (s *Service) AssignRole(ctx context.Context, companyID, userID, roleID, currentUserID uuid.UUID) error {
	if roleID == OwnerRoleID {
		currentRole, err := s.store.GetUserRole(ctx, companyID, currentUserID)
		if err != nil {
			return fmt.Errorf("get current user role: %w", err)
		}
		if currentRole.ID != OwnerRoleID && currentRole.Level < RoleLevelOwner {
			return fmt.Errorf("only the owner can transfer ownership")
		}
	}

	if err := s.store.AssignRoleToUser(ctx, companyID, userID, roleID); err != nil {
		return fmt.Errorf("assign role: %w", err)
	}

	s.enforcer.InvalidateAll()
	return nil
}

// RemoveRole resets a user's role to member.
func (s *Service) RemoveRole(ctx context.Context, companyID, userID uuid.UUID, roleID uuid.UUID) error {
	if roleID == OwnerRoleID {
		return fmt.Errorf("cannot remove the owner role")
	}

	if err := s.store.AssignRoleToUser(ctx, companyID, userID, MemberRoleID); err != nil {
		return fmt.Errorf("reset to member role: %w", err)
	}

	s.enforcer.InvalidateAll()
	return nil
}

// ListPermissions returns all available permissions.
func (s *Service) ListPermissions(ctx context.Context) ([]Permission, error) {
	return s.store.ListAllPermissions(ctx)
}

// GetUserPermissions returns the permissions for a user in a company.
func (s *Service) GetUserPermissions(ctx context.Context, companyID, userID uuid.UUID) ([]Permission, Role, error) {
	perms, err := s.store.GetUserPermissions(ctx, companyID, userID)
	if err != nil {
		return nil, Role{}, fmt.Errorf("get user permissions: %w", err)
	}

	role, err := s.store.GetUserRole(ctx, companyID, userID)
	if err != nil {
		return nil, Role{}, fmt.Errorf("get user role: %w", err)
	}

	return perms, role, nil
}
