package membership

import (
	"context"

	"github.com/google/uuid"
)

// MembershipStore defines database operations for membership resolution.
type MembershipStore interface {
	GetDirectMembership(ctx context.Context, companyID, userID uuid.UUID) (*MembershipRecord, error)
	GetCompanyAncestorChain(ctx context.Context, companyID uuid.UUID) ([]AncestorInfo, error)
	ListUserCompanyIDs(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)
}

// MembershipRecord represents a company membership record.
type MembershipRecord struct {
	CompanyID uuid.UUID
	UserID    uuid.UUID
	RoleID    uuid.UUID
	RoleName  string
	RoleLevel int
}

// AncestorInfo holds ancestor company info for hierarchy resolution.
type AncestorInfo struct {
	CompanyID      uuid.UUID
	Generations    int
	InheritRoleCap *int
	AccessLevel    *string
}
