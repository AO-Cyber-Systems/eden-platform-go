package devstore

import (
	"context"
	"fmt"

	"github.com/aocybersystems/eden-platform-go/platform/rbac"
	"github.com/google/uuid"
)

var _ rbac.RBACStore = (*RBACStore)(nil)

// RBACStore implements rbac.RBACStore using the in-memory devstore backend.
type RBACStore struct {
	backend *Backend
}

func (s *RBACStore) GetRoleByID(_ context.Context, id uuid.UUID) (rbac.Role, error) {
	s.backend.mu.RLock()
	defer s.backend.mu.RUnlock()

	role, ok := s.backend.state.rbacRoles[id]
	if !ok {
		return rbac.Role{}, fmt.Errorf("rbac role not found: %s", id)
	}
	return role, nil
}

func (s *RBACStore) ListRolesByCompany(_ context.Context, companyID uuid.UUID) ([]rbac.Role, error) {
	s.backend.mu.RLock()
	defer s.backend.mu.RUnlock()

	var roles []rbac.Role
	for _, role := range s.backend.state.rbacRoles {
		if role.IsSystem || (role.CompanyID != nil && *role.CompanyID == companyID) {
			roles = append(roles, role)
		}
	}
	return roles, nil
}

func (s *RBACStore) CreateRole(_ context.Context, companyID uuid.UUID, name, description string, level rbac.RoleLevel) (rbac.Role, error) {
	s.backend.mu.Lock()
	defer s.backend.mu.Unlock()

	role := rbac.Role{
		ID:          uuid.New(),
		CompanyID:   &companyID,
		Name:        name,
		Description: description,
		Level:       level,
		IsSystem:    false,
	}
	s.backend.state.rbacRoles[role.ID] = role
	return role, nil
}

func (s *RBACStore) ListPermissionsByRole(_ context.Context, roleID uuid.UUID) ([]rbac.Permission, error) {
	s.backend.mu.RLock()
	defer s.backend.mu.RUnlock()

	permIDs := s.backend.state.rolePermissions[roleID]
	perms := make([]rbac.Permission, 0, len(permIDs))
	for _, pid := range permIDs {
		if perm, ok := s.backend.state.rbacPermissions[pid]; ok {
			perms = append(perms, perm)
		}
	}
	return perms, nil
}

func (s *RBACStore) ListAllPermissions(_ context.Context) ([]rbac.Permission, error) {
	s.backend.mu.RLock()
	defer s.backend.mu.RUnlock()

	perms := make([]rbac.Permission, 0, len(s.backend.state.rbacPermissions))
	for _, perm := range s.backend.state.rbacPermissions {
		perms = append(perms, perm)
	}
	return perms, nil
}

func (s *RBACStore) AddRolePermission(_ context.Context, roleID, permissionID uuid.UUID) error {
	s.backend.mu.Lock()
	defer s.backend.mu.Unlock()

	s.backend.state.rolePermissions[roleID] = append(s.backend.state.rolePermissions[roleID], permissionID)
	return nil
}

func (s *RBACStore) GetUserRole(_ context.Context, companyID, userID uuid.UUID) (rbac.Role, error) {
	s.backend.mu.RLock()
	defer s.backend.mu.RUnlock()

	key := companyID.String() + ":" + userID.String()
	if membership, ok := s.backend.state.rbacMemberships[key]; ok {
		if role, ok := s.backend.state.rbacRoles[membership.RoleID]; ok {
			return role, nil
		}
	}

	// Fall back to auth memberships and convert auth.Role -> rbac.Role
	for _, m := range s.backend.state.memberships[userID] {
		if m.CompanyID == companyID {
			if authRole, ok := s.backend.state.roles[m.RoleID]; ok {
				return rbac.Role{
					ID:       authRole.ID,
					Name:     authRole.Name,
					Level:    rbac.RoleLevel(authRole.RoleLevel),
					IsSystem: true,
				}, nil
			}
		}
	}

	return rbac.Role{}, fmt.Errorf("rbac role not found for user %s in company %s", userID, companyID)
}

func (s *RBACStore) GetMembership(_ context.Context, companyID, userID uuid.UUID) (rbac.Membership, error) {
	s.backend.mu.RLock()
	defer s.backend.mu.RUnlock()

	key := companyID.String() + ":" + userID.String()
	if membership, ok := s.backend.state.rbacMemberships[key]; ok {
		return membership, nil
	}

	// Fall back to auth memberships
	for _, m := range s.backend.state.memberships[userID] {
		if m.CompanyID == companyID {
			if authRole, ok := s.backend.state.roles[m.RoleID]; ok {
				return rbac.Membership{
					ID:        uuid.New(),
					CompanyID: companyID,
					UserID:    userID,
					RoleID:    m.RoleID,
					RoleName:  authRole.Name,
					RoleLevel: rbac.RoleLevel(authRole.RoleLevel),
				}, nil
			}
		}
	}

	return rbac.Membership{}, fmt.Errorf("membership not found for user %s in company %s", userID, companyID)
}

func (s *RBACStore) GetUserPermissions(ctx context.Context, companyID, userID uuid.UUID) ([]rbac.Permission, error) {
	role, err := s.GetUserRole(ctx, companyID, userID)
	if err != nil {
		return nil, err
	}
	return s.ListPermissionsByRole(ctx, role.ID)
}

func (s *RBACStore) AssignRoleToUser(_ context.Context, companyID, userID, roleID uuid.UUID) error {
	s.backend.mu.Lock()
	defer s.backend.mu.Unlock()

	key := companyID.String() + ":" + userID.String()
	role, ok := s.backend.state.rbacRoles[roleID]
	if !ok {
		return fmt.Errorf("rbac role not found: %s", roleID)
	}

	s.backend.state.rbacMemberships[key] = rbac.Membership{
		ID:        uuid.New(),
		CompanyID: companyID,
		UserID:    userID,
		RoleID:    roleID,
		RoleName:  role.Name,
		RoleLevel: role.Level,
	}
	return nil
}

func (s *RBACStore) CreateMembership(_ context.Context, companyID, userID, roleID uuid.UUID) error {
	s.backend.mu.Lock()
	defer s.backend.mu.Unlock()

	key := companyID.String() + ":" + userID.String()
	role, ok := s.backend.state.rbacRoles[roleID]
	if !ok {
		return fmt.Errorf("rbac role not found: %s", roleID)
	}

	s.backend.state.rbacMemberships[key] = rbac.Membership{
		ID:        uuid.New(),
		CompanyID: companyID,
		UserID:    userID,
		RoleID:    roleID,
		RoleName:  role.Name,
		RoleLevel: role.Level,
	}
	return nil
}

func (s *RBACStore) GetCompanyAncestors(_ context.Context, companyID uuid.UUID) ([]rbac.CompanyAncestor, error) {
	return []rbac.CompanyAncestor{
		{CompanyID: companyID, Generations: 0},
	}, nil
}

func (s *RBACStore) GetMembershipInCompany(ctx context.Context, companyID, userID uuid.UUID) (*rbac.Membership, error) {
	membership, err := s.GetMembership(ctx, companyID, userID)
	if err != nil {
		return nil, err
	}
	return &membership, nil
}
