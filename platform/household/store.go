package household

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ErrNotFound is returned when a lookup yields no row. Stores must translate
// persistence-specific not-found errors to this sentinel.
var ErrNotFound = errors.New("household: not found")

// ErrAccountOwnerExists is returned by the store when an insert / update would
// create a second active account_owner in the same household (the partial
// unique index rejected it).
var ErrAccountOwnerExists = errors.New("household: an account_owner already exists for this household")

// Store is the persistence interface for the household domain.
//
// Every member query is household-scoped or identity-scoped so a query issued
// for household A can never observe household B's members. Implementations must
// be transactionally consistent within a single method call; SetAccountOwner
// in particular must demote-then-promote atomically.
type Store interface {
	// Households
	CreateHousehold(ctx context.Context, h Household) (Household, error)
	// CreateHouseholdWithOwner atomically inserts a household and its first
	// member (the account_owner) in a single transaction. This is the
	// provisioning entry point that establishes the existence lower-bound
	// (exactly one account_owner, >=1 manager) at creation time. If either
	// insert fails the whole operation rolls back, so no orphan household is
	// ever persisted. owner.HouseholdID is ignored — it is set to the newly
	// created household.
	CreateHouseholdWithOwner(ctx context.Context, h Household, owner Member) (Household, Member, error)
	GetHouseholdByID(ctx context.Context, id uuid.UUID) (Household, error)
	UpdateHousehold(ctx context.Context, h Household) (Household, error)
	DeleteHousehold(ctx context.Context, id uuid.UUID) error

	// Members
	AddMember(ctx context.Context, m Member) (Member, error)
	GetMember(ctx context.Context, id uuid.UUID) (Member, error)
	// GetMemberByIdentity returns the active membership for identityID within
	// householdID. Household-scoped: never returns another household's row.
	GetMemberByIdentity(ctx context.Context, householdID, identityID uuid.UUID) (Member, error)
	UpdateMemberRole(ctx context.Context, memberID uuid.UUID, role Role, isManager, isAccountOwner bool, caps []byte) (Member, error)
	RemoveMember(ctx context.Context, memberID uuid.UUID) error
	ListMembers(ctx context.Context, householdID uuid.UUID) ([]Member, error)
	CountManagers(ctx context.Context, householdID uuid.UUID) (int, error)

	// Read path for provisioning / AOID: the household an identity belongs to,
	// plus that identity's membership (role + capabilities).
	GetHouseholdForIdentity(ctx context.Context, identityID uuid.UUID) (Household, Member, error)
	ListHouseholdsForIdentity(ctx context.Context, identityID uuid.UUID) ([]Household, error)

	// SetAccountOwner atomically transfers the account_owner capability to
	// newOwnerMemberID within householdID (demote current, promote new).
	SetAccountOwner(ctx context.Context, householdID, newOwnerMemberID uuid.UUID) (Member, error)

	// Parent-of-record
	EstablishParentOfRecord(ctx context.Context, childMemberID, parentMemberID uuid.UUID) (ParentOfRecord, error)
	RevokeParentOfRecord(ctx context.Context, id uuid.UUID) error
	ListParentsOfRecord(ctx context.Context, childMemberID uuid.UUID) ([]ParentOfRecord, error)
	ListChildrenForParent(ctx context.Context, parentMemberID uuid.UUID) ([]ParentOfRecord, error)
}
