package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aocybersystems/eden-platform-go/internal/db"
	"github.com/aocybersystems/eden-platform-go/platform/consent"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var _ consent.Store = (*ConsentStore)(nil)

// ConsentStore implements consent.Store backed by PostgreSQL via pgx + sqlc.
//
// The underlying table is append-only; UPDATE / DELETE on existing rows are
// rejected by row-level triggers. Truncate-style cleanup must use TRUNCATE
// (which bypasses row-level triggers).
type ConsentStore struct {
	pool *pgxpool.Pool
}

// NewConsentStore returns a new pgstore-backed consent store.
func NewConsentStore(pool *pgxpool.Pool) *ConsentStore {
	return &ConsentStore{pool: pool}
}

func (s *ConsentStore) queries() *db.Queries {
	return db.New(s.pool)
}

func (s *ConsentStore) InsertEntry(ctx context.Context, e consent.Entry) (consent.Entry, error) {
	scope := e.Scope
	if len(scope) == 0 {
		scope = json.RawMessage("{}")
	}
	evidence := e.Evidence
	if len(evidence) == 0 {
		evidence = json.RawMessage("{}")
	}
	revokesID := pgtype.UUID{Valid: false}
	if e.RevokesID != nil {
		revokesID = pgtype.UUID{Bytes: *e.RevokesID, Valid: true}
	}
	row, err := s.queries().InsertConsentEntry(ctx, db.InsertConsentEntryParams{
		HouseholdID:        e.HouseholdID,
		PrincipalMemberID:  e.PrincipalMemberID,
		ConsenterMemberID:  e.ConsenterMemberID,
		Purpose:            string(e.Purpose),
		Scope:              scope,
		ConsentTextVersion: e.ConsentTextVersion,
		Evidence:           evidence,
		RevokesID:          revokesID,
	})
	if err != nil {
		return consent.Entry{}, fmt.Errorf("insert consent entry: %w", err)
	}
	return dbConsentToDomain(row), nil
}

func (s *ConsentStore) GetEntry(ctx context.Context, id uuid.UUID) (consent.Entry, error) {
	row, err := s.queries().GetConsentEntry(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return consent.Entry{}, consent.ErrNotFound
		}
		return consent.Entry{}, fmt.Errorf("get consent entry: %w", err)
	}
	return dbConsentToDomain(row), nil
}

func (s *ConsentStore) LatestForPurpose(ctx context.Context, principalMemberID uuid.UUID, purpose consent.Purpose) (consent.Entry, error) {
	row, err := s.queries().LatestConsentForPurpose(ctx, db.LatestConsentForPurposeParams{
		PrincipalMemberID: principalMemberID,
		Purpose:           string(purpose),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return consent.Entry{}, consent.ErrNotFound
		}
		return consent.Entry{}, fmt.Errorf("latest consent for purpose: %w", err)
	}
	return dbConsentToDomain(row), nil
}

func (s *ConsentStore) ListForPrincipal(ctx context.Context, principalMemberID uuid.UUID, limit, offset int32) ([]consent.Entry, error) {
	rows, err := s.queries().ListConsentEntriesForPrincipal(ctx, db.ListConsentEntriesForPrincipalParams{
		PrincipalMemberID: principalMemberID,
		Limit:             limit,
		Offset:            offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list consent for principal: %w", err)
	}
	out := make([]consent.Entry, 0, len(rows))
	for _, r := range rows {
		out = append(out, dbConsentToDomain(r))
	}
	return out, nil
}

func (s *ConsentStore) ListByPurpose(ctx context.Context, purpose consent.Purpose, limit, offset int32) ([]consent.Entry, error) {
	rows, err := s.queries().ListConsentEntriesByPurpose(ctx, db.ListConsentEntriesByPurposeParams{
		Purpose: string(purpose),
		Limit:   limit,
		Offset:  offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list consent by purpose: %w", err)
	}
	out := make([]consent.Entry, 0, len(rows))
	for _, r := range rows {
		out = append(out, dbConsentToDomain(r))
	}
	return out, nil
}

func (s *ConsentStore) ListForHousehold(ctx context.Context, householdID uuid.UUID, limit, offset int32) ([]consent.Entry, error) {
	rows, err := s.queries().ListConsentEntriesForHousehold(ctx, db.ListConsentEntriesForHouseholdParams{
		HouseholdID: householdID,
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list consent for household: %w", err)
	}
	out := make([]consent.Entry, 0, len(rows))
	for _, r := range rows {
		out = append(out, dbConsentToDomain(r))
	}
	return out, nil
}

func dbConsentToDomain(row db.ConsentLedger) consent.Entry {
	e := consent.Entry{
		ID:                 row.ID,
		HouseholdID:        row.HouseholdID,
		PrincipalMemberID:  row.PrincipalMemberID,
		ConsenterMemberID:  row.ConsenterMemberID,
		Purpose:            consent.Purpose(row.Purpose),
		Scope:              row.Scope,
		ConsentTextVersion: row.ConsentTextVersion,
		Evidence:           row.Evidence,
		GrantedAt:          row.GrantedAt,
		CreatedAt:          row.CreatedAt,
	}
	if row.RevokesID.Valid {
		id := uuid.UUID(row.RevokesID.Bytes)
		e.RevokesID = &id
	}
	return e
}
