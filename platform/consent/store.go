package consent

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// ErrNotFound is returned by lookups that yield no row.
var ErrNotFound = errors.New("consent: not found")

// Store is the persistence interface for the consent ledger.
//
// All implementations must guarantee append-only semantics: InsertEntry adds
// new rows; existing rows MUST NOT be modified or deleted. The PostgreSQL
// implementation enforces this via row-level triggers as a defense in depth.
type Store interface {
	InsertEntry(ctx context.Context, e Entry) (Entry, error)
	GetEntry(ctx context.Context, id uuid.UUID) (Entry, error)

	// LatestForPurpose returns the most recent entry for (principal, purpose),
	// regardless of whether it is a grant or revocation. Service.IsValid
	// interprets the result.
	LatestForPurpose(ctx context.Context, principalMemberID uuid.UUID, purpose Purpose) (Entry, error)

	ListForPrincipal(ctx context.Context, principalMemberID uuid.UUID, limit, offset int32) ([]Entry, error)
	ListByPurpose(ctx context.Context, purpose Purpose, limit, offset int32) ([]Entry, error)
	ListForHousehold(ctx context.Context, householdID uuid.UUID, limit, offset int32) ([]Entry, error)
}
