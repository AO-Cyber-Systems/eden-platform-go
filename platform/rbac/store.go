package rbac

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

// RoleLevel defines the numeric hierarchy of roles.
type RoleLevel int

const (
	RoleLevelViewer     RoleLevel = 20
	RoleLevelMember     RoleLevel = 40
	RoleLevelManager    RoleLevel = 60
	RoleLevelAdmin      RoleLevel = 80
	RoleLevelOwner      RoleLevel = 90
	RoleLevelSuperAdmin RoleLevel = 100
)

// Feature represents a feature with per-action permission matrices.
type Feature string

// Action-level minimum role requirements per feature.
// Format: feature -> action -> minimum role level.
type PermissionMatrix map[Feature]map[string]RoleLevel

// Role represents a role in the RBAC system.
type Role struct {
	ID          uuid.UUID
	CompanyID   *uuid.UUID // nil for system roles
	Name        string
	Description string
	Level       RoleLevel
	IsSystem    bool
}

// Permission represents a specific permission.
type Permission struct {
	ID       uuid.UUID
	Feature  string
	Action   string
	Resource string
}

// Membership represents a company membership with optional overrides.
type Membership struct {
	ID                  uuid.UUID
	CompanyID           uuid.UUID
	UserID              uuid.UUID
	RoleID              uuid.UUID
	RoleName            string
	RoleLevel           RoleLevel
	PermissionOverrides json.RawMessage // JSONB: {"feature:action": true/false}
}

// RBACStore defines database operations for RBAC.
type RBACStore interface {
	// Role operations
	GetRoleByID(ctx context.Context, id uuid.UUID) (Role, error)
	ListRolesByCompany(ctx context.Context, companyID uuid.UUID) ([]Role, error)
	CreateRole(ctx context.Context, companyID uuid.UUID, name, description string, level RoleLevel) (Role, error)

	// Permission operations
	ListPermissionsByRole(ctx context.Context, roleID uuid.UUID) ([]Permission, error)
	ListAllPermissions(ctx context.Context) ([]Permission, error)
	AddRolePermission(ctx context.Context, roleID, permissionID uuid.UUID) error

	// Membership operations
	GetUserRole(ctx context.Context, companyID, userID uuid.UUID) (Role, error)
	GetMembership(ctx context.Context, companyID, userID uuid.UUID) (Membership, error)
	GetUserPermissions(ctx context.Context, companyID, userID uuid.UUID) ([]Permission, error)
	AssignRoleToUser(ctx context.Context, companyID, userID, roleID uuid.UUID) error
	CreateMembership(ctx context.Context, companyID, userID, roleID uuid.UUID) error

	// Hierarchy operations (for hierarchy-aware resolution)
	GetCompanyAncestors(ctx context.Context, companyID uuid.UUID) ([]CompanyAncestor, error)
	GetMembershipInCompany(ctx context.Context, companyID, userID uuid.UUID) (*Membership, error)
}

// CompanyAncestor represents a company in the hierarchy chain.
type CompanyAncestor struct {
	CompanyID        uuid.UUID
	Generations      int
	InheritedRoleCap *int    // max role level from parent
	AccessLevel      *string // "full", "read_only", "none"
}
