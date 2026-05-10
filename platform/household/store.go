package household

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ErrNotFound is returned when a lookup yields no row. Stores must wrap or
// translate persistence-specific not-found errors to this sentinel.
var ErrNotFound = errors.New("household: not found")

// Store is the persistence interface for the household domain.
//
// Implementations must be transactionally consistent within a single method
// call. Cross-method atomicity (if needed) is the caller's responsibility.
type Store interface {
	// Households
	CreateHousehold(ctx context.Context, h Household) (Household, error)
	GetHousehold(ctx context.Context, id uuid.UUID) (Household, error)
	UpdateHousehold(ctx context.Context, h Household) (Household, error)
	DeleteHousehold(ctx context.Context, id uuid.UUID) error

	// Members
	AddMember(ctx context.Context, m Member) (Member, error)
	GetMember(ctx context.Context, id uuid.UUID) (Member, error)
	UpdateMemberRole(ctx context.Context, memberID uuid.UUID, role Role, caps Capabilities) (Member, error)
	RemoveMember(ctx context.Context, memberID uuid.UUID) error
	ListMembers(ctx context.Context, householdID uuid.UUID) ([]Member, error)
	ListHouseholdsForUser(ctx context.Context, userID uuid.UUID) ([]Household, error)

	// Parent-of-record
	EstablishParentOfRecord(ctx context.Context, childMemberID, parentMemberID uuid.UUID) (ParentOfRecord, error)
	RevokeParentOfRecord(ctx context.Context, id uuid.UUID) error
	ListParentsOfRecord(ctx context.Context, childMemberID uuid.UUID) ([]ParentOfRecord, error)
	ListChildrenForParent(ctx context.Context, parentMemberID uuid.UUID) ([]ParentOfRecord, error)
}
