package rbac

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// HierarchyResolver resolves effective permissions through the company hierarchy.
// Resolution order: direct membership -> ancestor walk with role cap + access level enforcement.
type HierarchyResolver struct {
	store RBACStore
}

// NewHierarchyResolver creates a new hierarchy resolver.
func NewHierarchyResolver(store RBACStore) *HierarchyResolver {
	return &HierarchyResolver{store: store}
}

// ResolvedMembership represents the effective membership after hierarchy resolution.
type ResolvedMembership struct {
	CompanyID     uuid.UUID
	UserID        uuid.UUID
	RoleLevel     RoleLevel
	RoleName      string
	SourceCompany uuid.UUID // the company where the membership was actually found
	IsDirect      bool      // true if membership is on this company directly
	CappedLevel   RoleLevel // effective level after role cap from hierarchy
	AccessLevel   string    // "full", "read_only", "none"
}

// ResolveMembership resolves a user's effective membership for a company,
// walking up the hierarchy if no direct membership exists.
func (r *HierarchyResolver) ResolveMembership(ctx context.Context, companyID, userID uuid.UUID) (*ResolvedMembership, error) {
	// Step 1: Check direct membership
	direct, err := r.store.GetMembershipInCompany(ctx, companyID, userID)
	if err == nil && direct != nil {
		return &ResolvedMembership{
			CompanyID:     companyID,
			UserID:        userID,
			RoleLevel:     direct.RoleLevel,
			RoleName:      direct.RoleName,
			SourceCompany: companyID,
			IsDirect:      true,
			CappedLevel:   direct.RoleLevel,
			AccessLevel:   "full",
		}, nil
	}

	// Step 2: Walk ancestors (ordered by generations ASC = nearest first)
	ancestors, err := r.store.GetCompanyAncestors(ctx, companyID)
	if err != nil {
		return nil, fmt.Errorf("get company ancestors: %w", err)
	}

	for _, ancestor := range ancestors {
		membership, err := r.store.GetMembershipInCompany(ctx, ancestor.CompanyID, userID)
		if err != nil || membership == nil {
			continue
		}

		// Apply role cap from hierarchy
		effectiveLevel := membership.RoleLevel
		if ancestor.InheritedRoleCap != nil && RoleLevel(*ancestor.InheritedRoleCap) < effectiveLevel {
			effectiveLevel = RoleLevel(*ancestor.InheritedRoleCap)
		}

		// Determine access level
		accessLevel := "full"
		if ancestor.AccessLevel != nil {
			accessLevel = *ancestor.AccessLevel
		}

		// If access is "none", skip this ancestor
		if accessLevel == "none" {
			continue
		}

		return &ResolvedMembership{
			CompanyID:     companyID,
			UserID:        userID,
			RoleLevel:     membership.RoleLevel,
			RoleName:      membership.RoleName,
			SourceCompany: ancestor.CompanyID,
			IsDirect:      false,
			CappedLevel:   effectiveLevel,
			AccessLevel:   accessLevel,
		}, nil
	}

	return nil, fmt.Errorf("user %s has no membership for company %s or its ancestors", userID, companyID)
}

// CanAccessCompany checks if a user has any level of access to a company.
func (r *HierarchyResolver) CanAccessCompany(ctx context.Context, companyID, userID uuid.UUID) (bool, error) {
	resolved, err := r.ResolveMembership(ctx, companyID, userID)
	if err != nil {
		return false, nil // no membership = no access
	}
	return resolved.AccessLevel != "none", nil
}

// GetEffectiveRoleLevel returns the user's effective role level for a company
// after hierarchy caps are applied.
func (r *HierarchyResolver) GetEffectiveRoleLevel(ctx context.Context, companyID, userID uuid.UUID) (RoleLevel, error) {
	resolved, err := r.ResolveMembership(ctx, companyID, userID)
	if err != nil {
		return 0, err
	}
	return resolved.CappedLevel, nil
}
