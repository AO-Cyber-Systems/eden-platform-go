package membership

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// Resolver resolves effective membership through the company hierarchy.
type Resolver struct {
	store MembershipStore
}

// NewResolver creates a new membership resolver.
func NewResolver(store MembershipStore) *Resolver {
	return &Resolver{store: store}
}

// ResolvedMembership is the result of membership resolution.
type ResolvedMembership struct {
	CompanyID     uuid.UUID
	UserID        uuid.UUID
	RoleLevel     int
	RoleName      string
	SourceCompany uuid.UUID
	IsDirect      bool
	CappedLevel   int
	AccessLevel   string
}

// Resolve finds a user's effective membership for a company.
func (r *Resolver) Resolve(ctx context.Context, companyID, userID uuid.UUID) (*ResolvedMembership, error) {
	// Check direct membership first
	direct, err := r.store.GetDirectMembership(ctx, companyID, userID)
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

	// Walk ancestors
	ancestors, err := r.store.GetCompanyAncestorChain(ctx, companyID)
	if err != nil {
		return nil, fmt.Errorf("get ancestors: %w", err)
	}

	for _, ancestor := range ancestors {
		m, err := r.store.GetDirectMembership(ctx, ancestor.CompanyID, userID)
		if err != nil || m == nil {
			continue
		}

		effectiveLevel := m.RoleLevel
		if ancestor.InheritRoleCap != nil && *ancestor.InheritRoleCap < effectiveLevel {
			effectiveLevel = *ancestor.InheritRoleCap
		}

		accessLevel := "full"
		if ancestor.AccessLevel != nil {
			accessLevel = *ancestor.AccessLevel
		}
		if accessLevel == "none" {
			continue
		}

		return &ResolvedMembership{
			CompanyID:     companyID,
			UserID:        userID,
			RoleLevel:     m.RoleLevel,
			RoleName:      m.RoleName,
			SourceCompany: ancestor.CompanyID,
			IsDirect:      false,
			CappedLevel:   effectiveLevel,
			AccessLevel:   accessLevel,
		}, nil
	}

	return nil, fmt.Errorf("no membership found for user %s in company %s hierarchy", userID, companyID)
}

// ListAccessibleCompanies returns all company IDs a user has membership in.
func (r *Resolver) ListAccessibleCompanies(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	return r.store.ListUserCompanyIDs(ctx, userID)
}
