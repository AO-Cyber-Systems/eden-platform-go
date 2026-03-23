package pgstore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aocybersystems/eden-platform-go/internal/db"
	"github.com/aocybersystems/eden-platform-go/platform/rbac"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var _ rbac.RBACStore = (*RBACStore)(nil)

// RBACStore implements rbac.RBACStore backed by PostgreSQL via pgx and sqlc.
type RBACStore struct {
	pool *pgxpool.Pool
}

// NewRBACStore creates a new PostgreSQL-backed RBAC store.
func NewRBACStore(pool *pgxpool.Pool) *RBACStore {
	return &RBACStore{pool: pool}
}

func (s *RBACStore) queries() *db.Queries {
	return db.New(s.pool)
}

func (s *RBACStore) GetRoleByID(ctx context.Context, id uuid.UUID) (rbac.Role, error) {
	row, err := s.queries().GetRoleByID(ctx, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return rbac.Role{}, fmt.Errorf("rbac role not found: %s", id)
		}
		return rbac.Role{}, fmt.Errorf("get role: %w", err)
	}
	return dbRoleToRBAC(row), nil
}

func (s *RBACStore) ListRolesByCompany(ctx context.Context, companyID uuid.UUID) ([]rbac.Role, error) {
	rows, err := s.queries().ListRolesByCompany(ctx, uuidToPgtype(&companyID))
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}
	roles := make([]rbac.Role, len(rows))
	for i, row := range rows {
		roles[i] = dbRoleToRBAC(row)
	}
	return roles, nil
}

func (s *RBACStore) CreateRole(ctx context.Context, companyID uuid.UUID, name, description string, level rbac.RoleLevel) (rbac.Role, error) {
	row, err := s.queries().CreateRole(ctx, db.CreateRoleParams{
		CompanyID:   uuidToPgtype(&companyID),
		Name:        name,
		Description: description,
		Level:       int32(level),
	})
	if err != nil {
		return rbac.Role{}, fmt.Errorf("create role: %w", err)
	}
	return dbRoleToRBAC(row), nil
}

func (s *RBACStore) ListPermissionsByRole(ctx context.Context, roleID uuid.UUID) ([]rbac.Permission, error) {
	rows, err := s.queries().ListPermissionsByRole(ctx, roleID)
	if err != nil {
		return nil, fmt.Errorf("list permissions by role: %w", err)
	}
	perms := make([]rbac.Permission, len(rows))
	for i, row := range rows {
		perms[i] = dbPermToRBAC(row)
	}
	return perms, nil
}

func (s *RBACStore) ListAllPermissions(ctx context.Context) ([]rbac.Permission, error) {
	rows, err := s.queries().ListAllPermissions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list all permissions: %w", err)
	}
	perms := make([]rbac.Permission, len(rows))
	for i, row := range rows {
		perms[i] = dbPermToRBAC(row)
	}
	return perms, nil
}

func (s *RBACStore) AddRolePermission(ctx context.Context, roleID, permissionID uuid.UUID) error {
	return s.queries().AddRolePermission(ctx, db.AddRolePermissionParams{
		RoleID:       roleID,
		PermissionID: permissionID,
	})
}

func (s *RBACStore) GetUserRole(ctx context.Context, companyID, userID uuid.UUID) (rbac.Role, error) {
	row, err := s.queries().GetUserRole(ctx, db.GetUserRoleParams{
		CompanyID: companyID,
		UserID:    userID,
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			return rbac.Role{}, fmt.Errorf("rbac role not found for user %s in company %s", userID, companyID)
		}
		return rbac.Role{}, fmt.Errorf("get user role: %w", err)
	}
	return dbRoleToRBAC(row), nil
}

func (s *RBACStore) GetMembership(ctx context.Context, companyID, userID uuid.UUID) (rbac.Membership, error) {
	row, err := s.queries().GetMembership(ctx, db.GetMembershipParams{
		CompanyID: companyID,
		UserID:    userID,
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			return rbac.Membership{}, fmt.Errorf("membership not found for user %s in company %s", userID, companyID)
		}
		return rbac.Membership{}, fmt.Errorf("get membership: %w", err)
	}
	return rbac.Membership{
		ID:                  row.ID,
		CompanyID:           row.CompanyID,
		UserID:              row.UserID,
		RoleID:              row.RoleID,
		RoleName:            row.RoleName,
		RoleLevel:           rbac.RoleLevel(row.RoleLevel),
		PermissionOverrides: json.RawMessage(row.PermissionOverrides),
	}, nil
}

func (s *RBACStore) GetUserPermissions(ctx context.Context, companyID, userID uuid.UUID) ([]rbac.Permission, error) {
	rows, err := s.queries().GetUserPermissions(ctx, db.GetUserPermissionsParams{
		CompanyID: companyID,
		UserID:    userID,
	})
	if err != nil {
		return nil, fmt.Errorf("get user permissions: %w", err)
	}
	perms := make([]rbac.Permission, len(rows))
	for i, row := range rows {
		perms[i] = dbPermToRBAC(row)
	}
	return perms, nil
}

func (s *RBACStore) AssignRoleToUser(ctx context.Context, companyID, userID, roleID uuid.UUID) error {
	return s.queries().AssignRoleToUser(ctx, db.AssignRoleToUserParams{
		CompanyID: companyID,
		UserID:    userID,
		RoleID:    roleID,
	})
}

func (s *RBACStore) CreateMembership(ctx context.Context, companyID, userID, roleID uuid.UUID) error {
	return s.queries().CreateMembership(ctx, db.CreateMembershipParams{
		CompanyID: companyID,
		UserID:    userID,
		RoleID:    roleID,
	})
}

func (s *RBACStore) GetCompanyAncestors(ctx context.Context, companyID uuid.UUID) ([]rbac.CompanyAncestor, error) {
	rows, err := s.queries().GetCompanyAncestors(ctx, companyID)
	if err != nil {
		return nil, fmt.Errorf("get company ancestors: %w", err)
	}
	ancestors := make([]rbac.CompanyAncestor, len(rows))
	for i, row := range rows {
		var inheritedRoleCap *int
		if row.InheritedRoleCap != nil {
			v := int(*row.InheritedRoleCap)
			inheritedRoleCap = &v
		}
		ancestors[i] = rbac.CompanyAncestor{
			CompanyID:        row.CompanyID,
			Generations:      int(row.Generations),
			InheritedRoleCap: inheritedRoleCap,
			AccessLevel:      row.AccessLevel,
		}
	}
	return ancestors, nil
}

func (s *RBACStore) GetMembershipInCompany(ctx context.Context, companyID, userID uuid.UUID) (*rbac.Membership, error) {
	membership, err := s.GetMembership(ctx, companyID, userID)
	if err != nil {
		// Not found returns nil, nil (not an error)
		return nil, nil
	}
	return &membership, nil
}

// -- Type conversion helpers --

func dbRoleToRBAC(r db.Role) rbac.Role {
	return rbac.Role{
		ID:          r.ID,
		CompanyID:   pgtypeUUID(r.CompanyID),
		Name:        r.Name,
		Description: r.Description,
		Level:       rbac.RoleLevel(r.Level),
		IsSystem:    r.IsSystem,
	}
}

func dbPermToRBAC(p db.Permission) rbac.Permission {
	return rbac.Permission{
		ID:       p.ID,
		Feature:  p.Feature,
		Action:   p.Action,
		Resource: p.Resource,
	}
}
