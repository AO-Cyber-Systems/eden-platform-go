package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aocybersystems/eden-platform-go/internal/db"
	"github.com/aocybersystems/eden-platform-go/platform/household"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var _ household.Store = (*HouseholdStore)(nil)

// HouseholdStore implements household.Store backed by PostgreSQL via pgx + sqlc.
type HouseholdStore struct {
	pool *pgxpool.Pool
}

// NewHouseholdStore returns a new pgstore-backed household store.
func NewHouseholdStore(pool *pgxpool.Pool) *HouseholdStore {
	return &HouseholdStore{pool: pool}
}

func (s *HouseholdStore) queries() *db.Queries {
	return db.New(s.pool)
}

// ---- Households ----

func (s *HouseholdStore) CreateHousehold(ctx context.Context, h household.Household) (household.Household, error) {
	metadata := h.Metadata
	if len(metadata) == 0 {
		metadata = json.RawMessage("{}")
	}
	row, err := s.queries().CreateHousehold(ctx, db.CreateHouseholdParams{
		PrimaryContactUserID: h.PrimaryContactUserID,
		DisplayName:          h.DisplayName,
		Metadata:             metadata,
	})
	if err != nil {
		return household.Household{}, fmt.Errorf("create household: %w", err)
	}
	return dbHouseholdToDomain(row), nil
}

func (s *HouseholdStore) GetHousehold(ctx context.Context, id uuid.UUID) (household.Household, error) {
	row, err := s.queries().GetHousehold(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return household.Household{}, household.ErrNotFound
		}
		return household.Household{}, fmt.Errorf("get household: %w", err)
	}
	return dbHouseholdToDomain(row), nil
}

func (s *HouseholdStore) UpdateHousehold(ctx context.Context, h household.Household) (household.Household, error) {
	metadata := h.Metadata
	if len(metadata) == 0 {
		metadata = json.RawMessage("{}")
	}
	row, err := s.queries().UpdateHousehold(ctx, db.UpdateHouseholdParams{
		ID:          h.ID,
		DisplayName: h.DisplayName,
		Metadata:    metadata,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return household.Household{}, household.ErrNotFound
		}
		return household.Household{}, fmt.Errorf("update household: %w", err)
	}
	return dbHouseholdToDomain(row), nil
}

func (s *HouseholdStore) DeleteHousehold(ctx context.Context, id uuid.UUID) error {
	if err := s.queries().DeleteHousehold(ctx, id); err != nil {
		return fmt.Errorf("delete household: %w", err)
	}
	return nil
}

// ---- Members ----

func (s *HouseholdStore) AddMember(ctx context.Context, m household.Member) (household.Member, error) {
	caps, err := json.Marshal(m.Capabilities)
	if err != nil {
		return household.Member{}, fmt.Errorf("marshal capabilities: %w", err)
	}
	status := m.Status
	if status == "" {
		status = household.StatusActive
	}
	row, err := s.queries().AddHouseholdMember(ctx, db.AddHouseholdMemberParams{
		HouseholdID:  m.HouseholdID,
		UserID:       m.UserID,
		Role:         string(m.Role),
		Status:       string(status),
		Birthdate:    timeToPgDate(m.Birthdate),
		Capabilities: caps,
	})
	if err != nil {
		return household.Member{}, fmt.Errorf("add household member: %w", err)
	}
	return dbMemberToDomain(row)
}

func (s *HouseholdStore) GetMember(ctx context.Context, id uuid.UUID) (household.Member, error) {
	row, err := s.queries().GetHouseholdMember(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return household.Member{}, household.ErrNotFound
		}
		return household.Member{}, fmt.Errorf("get household member: %w", err)
	}
	return dbMemberToDomain(row)
}

func (s *HouseholdStore) UpdateMemberRole(ctx context.Context, memberID uuid.UUID, role household.Role, caps household.Capabilities) (household.Member, error) {
	capsJSON, err := json.Marshal(caps)
	if err != nil {
		return household.Member{}, fmt.Errorf("marshal capabilities: %w", err)
	}
	row, err := s.queries().UpdateHouseholdMemberRole(ctx, db.UpdateHouseholdMemberRoleParams{
		ID:           memberID,
		Role:         string(role),
		Capabilities: capsJSON,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return household.Member{}, household.ErrNotFound
		}
		return household.Member{}, fmt.Errorf("update household member role: %w", err)
	}
	return dbMemberToDomain(row)
}

func (s *HouseholdStore) RemoveMember(ctx context.Context, memberID uuid.UUID) error {
	if err := s.queries().RemoveHouseholdMember(ctx, memberID); err != nil {
		return fmt.Errorf("remove household member: %w", err)
	}
	return nil
}

func (s *HouseholdStore) ListMembers(ctx context.Context, householdID uuid.UUID) ([]household.Member, error) {
	rows, err := s.queries().ListHouseholdMembers(ctx, householdID)
	if err != nil {
		return nil, fmt.Errorf("list household members: %w", err)
	}
	out := make([]household.Member, 0, len(rows))
	for _, r := range rows {
		m, err := dbMemberToDomain(r)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func (s *HouseholdStore) ListHouseholdsForUser(ctx context.Context, userID uuid.UUID) ([]household.Household, error) {
	rows, err := s.queries().ListHouseholdsForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list households for user: %w", err)
	}
	out := make([]household.Household, 0, len(rows))
	for _, r := range rows {
		out = append(out, dbHouseholdToDomain(r))
	}
	return out, nil
}

// ---- Parent of Record ----

func (s *HouseholdStore) EstablishParentOfRecord(ctx context.Context, childMemberID, parentMemberID uuid.UUID) (household.ParentOfRecord, error) {
	row, err := s.queries().EstablishParentOfRecord(ctx, db.EstablishParentOfRecordParams{
		ChildMemberID:  childMemberID,
		ParentMemberID: parentMemberID,
	})
	if err != nil {
		return household.ParentOfRecord{}, fmt.Errorf("establish parent_of_record: %w", err)
	}
	return dbPORToDomain(row), nil
}

func (s *HouseholdStore) RevokeParentOfRecord(ctx context.Context, id uuid.UUID) error {
	if err := s.queries().RevokeParentOfRecord(ctx, id); err != nil {
		return fmt.Errorf("revoke parent_of_record: %w", err)
	}
	return nil
}

func (s *HouseholdStore) ListParentsOfRecord(ctx context.Context, childMemberID uuid.UUID) ([]household.ParentOfRecord, error) {
	rows, err := s.queries().ListParentsOfRecord(ctx, childMemberID)
	if err != nil {
		return nil, fmt.Errorf("list parents_of_record: %w", err)
	}
	out := make([]household.ParentOfRecord, 0, len(rows))
	for _, r := range rows {
		out = append(out, dbPORToDomain(r))
	}
	return out, nil
}

func (s *HouseholdStore) ListChildrenForParent(ctx context.Context, parentMemberID uuid.UUID) ([]household.ParentOfRecord, error) {
	rows, err := s.queries().ListChildrenForParent(ctx, parentMemberID)
	if err != nil {
		return nil, fmt.Errorf("list children for parent: %w", err)
	}
	out := make([]household.ParentOfRecord, 0, len(rows))
	for _, r := range rows {
		out = append(out, dbPORToDomain(r))
	}
	return out, nil
}

// ---- Mappers ----

func dbHouseholdToDomain(row db.Household) household.Household {
	return household.Household{
		ID:                   row.ID,
		PrimaryContactUserID: row.PrimaryContactUserID,
		DisplayName:          row.DisplayName,
		Metadata:             row.Metadata,
		CreatedAt:            row.CreatedAt,
		UpdatedAt:            row.UpdatedAt,
	}
}

func dbMemberToDomain(row db.HouseholdMember) (household.Member, error) {
	var caps household.Capabilities
	if len(row.Capabilities) > 0 {
		if err := json.Unmarshal(row.Capabilities, &caps); err != nil {
			return household.Member{}, fmt.Errorf("unmarshal capabilities: %w", err)
		}
	}
	m := household.Member{
		ID:           row.ID,
		HouseholdID:  row.HouseholdID,
		UserID:       row.UserID,
		Role:         household.Role(row.Role),
		Status:       household.Status(row.Status),
		Capabilities: caps,
		AddedAt:      row.AddedAt,
		Birthdate:    pgDateToTime(row.Birthdate),
	}
	if row.RemovedAt.Valid {
		t := row.RemovedAt.Time
		m.RemovedAt = &t
	}
	return m, nil
}

func dbPORToDomain(row db.ParentOfRecord) household.ParentOfRecord {
	por := household.ParentOfRecord{
		ID:             row.ID,
		ChildMemberID:  row.ChildMemberID,
		ParentMemberID: row.ParentMemberID,
		EstablishedAt:  row.EstablishedAt,
	}
	if row.RevokedAt.Valid {
		t := row.RevokedAt.Time
		por.RevokedAt = &t
	}
	return por
}

func timeToPgDate(t *time.Time) pgtype.Date {
	if t == nil {
		return pgtype.Date{Valid: false}
	}
	return pgtype.Date{Time: t.UTC(), Valid: true}
}

func pgDateToTime(d pgtype.Date) *time.Time {
	if !d.Valid {
		return nil
	}
	t := d.Time
	return &t
}
