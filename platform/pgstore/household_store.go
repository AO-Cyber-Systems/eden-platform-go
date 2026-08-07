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
	"github.com/jackc/pgx/v5/pgconn"
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

// isAccountOwnerConflict reports whether err is the partial-unique-index
// violation on the one-account-owner-per-household index.
func isAccountOwnerConflict(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505" && pgErr.ConstraintName == "uq_platform_household_one_account_owner"
	}
	return false
}

// ---- Households ----

func (s *HouseholdStore) CreateHousehold(ctx context.Context, h household.Household) (household.Household, error) {
	metadata := h.Metadata
	if len(metadata) == 0 {
		metadata = json.RawMessage("{}")
	}
	row, err := s.queries().CreateHousehold(ctx, db.CreateHouseholdParams{
		PrimaryContactIdentityID: h.PrimaryContactIdentityID,
		DisplayName:              h.DisplayName,
		Metadata:                 metadata,
	})
	if err != nil {
		return household.Household{}, fmt.Errorf("create household: %w", err)
	}
	return dbHouseholdToDomain(row), nil
}

func (s *HouseholdStore) GetHouseholdByID(ctx context.Context, id uuid.UUID) (household.Household, error) {
	row, err := s.queries().GetHouseholdByID(ctx, id)
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
	caps := m.Capabilities
	if len(caps) == 0 {
		caps = json.RawMessage("{}")
	}
	status := m.Status
	if status == "" {
		status = household.StatusActive
	}
	row, err := s.queries().AddHouseholdMember(ctx, db.AddHouseholdMemberParams{
		HouseholdID:    m.HouseholdID,
		IdentityID:     m.IdentityID,
		Role:           string(m.Role),
		Status:         string(status),
		Birthdate:      timeToPgDate(m.Birthdate),
		IsManager:      m.IsManager,
		IsAccountOwner: m.IsAccountOwner,
		Capabilities:   caps,
	})
	if err != nil {
		if isAccountOwnerConflict(err) {
			return household.Member{}, household.ErrAccountOwnerExists
		}
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

func (s *HouseholdStore) GetMemberByIdentity(ctx context.Context, householdID, identityID uuid.UUID) (household.Member, error) {
	row, err := s.queries().GetMemberByHouseholdAndIdentity(ctx, db.GetMemberByHouseholdAndIdentityParams{
		HouseholdID: householdID,
		IdentityID:  identityID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return household.Member{}, household.ErrNotFound
		}
		return household.Member{}, fmt.Errorf("get member by identity: %w", err)
	}
	return dbMemberToDomain(row)
}

func (s *HouseholdStore) UpdateMemberRole(ctx context.Context, memberID uuid.UUID, role household.Role, isManager, isAccountOwner bool, caps []byte) (household.Member, error) {
	if len(caps) == 0 {
		caps = json.RawMessage("{}")
	}
	row, err := s.queries().UpdateHouseholdMemberRole(ctx, db.UpdateHouseholdMemberRoleParams{
		ID:             memberID,
		Role:           string(role),
		IsManager:      isManager,
		IsAccountOwner: isAccountOwner,
		Capabilities:   caps,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return household.Member{}, household.ErrNotFound
		}
		if isAccountOwnerConflict(err) {
			return household.Member{}, household.ErrAccountOwnerExists
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

func (s *HouseholdStore) CountManagers(ctx context.Context, householdID uuid.UUID) (int, error) {
	n, err := s.queries().CountHouseholdManagers(ctx, householdID)
	if err != nil {
		return 0, fmt.Errorf("count household managers: %w", err)
	}
	return int(n), nil
}

func (s *HouseholdStore) GetHouseholdForIdentity(ctx context.Context, identityID uuid.UUID) (household.Household, household.Member, error) {
	memberRow, err := s.queries().GetActiveMembershipForIdentity(ctx, identityID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return household.Household{}, household.Member{}, household.ErrNotFound
		}
		return household.Household{}, household.Member{}, fmt.Errorf("get membership for identity: %w", err)
	}
	member, err := dbMemberToDomain(memberRow)
	if err != nil {
		return household.Household{}, household.Member{}, err
	}
	hhRow, err := s.queries().GetHouseholdByID(ctx, member.HouseholdID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return household.Household{}, household.Member{}, household.ErrNotFound
		}
		return household.Household{}, household.Member{}, fmt.Errorf("get household for identity: %w", err)
	}
	return dbHouseholdToDomain(hhRow), member, nil
}

func (s *HouseholdStore) ListHouseholdsForIdentity(ctx context.Context, identityID uuid.UUID) ([]household.Household, error) {
	rows, err := s.queries().ListHouseholdsForIdentity(ctx, identityID)
	if err != nil {
		return nil, fmt.Errorf("list households for identity: %w", err)
	}
	out := make([]household.Household, 0, len(rows))
	for _, r := range rows {
		out = append(out, dbHouseholdToDomain(r))
	}
	return out, nil
}

// SetAccountOwner atomically demotes the current account_owner and promotes
// newOwnerMemberID within householdID, in a single transaction so the partial
// unique index never sees two owners.
func (s *HouseholdStore) SetAccountOwner(ctx context.Context, householdID, newOwnerMemberID uuid.UUID) (household.Member, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return household.Member{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	q := db.New(tx)
	if err := q.ClearHouseholdAccountOwner(ctx, householdID); err != nil {
		return household.Member{}, fmt.Errorf("clear account owner: %w", err)
	}
	row, err := q.SetHouseholdAccountOwner(ctx, db.SetHouseholdAccountOwnerParams{
		HouseholdID: householdID,
		ID:          newOwnerMemberID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return household.Member{}, household.ErrNotFound
		}
		return household.Member{}, fmt.Errorf("set account owner: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return household.Member{}, fmt.Errorf("commit tx: %w", err)
	}
	return dbMemberToDomain(row)
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

func dbHouseholdToDomain(row db.PlatformHousehold) household.Household {
	return household.Household{
		ID:                       row.ID,
		PrimaryContactIdentityID: row.PrimaryContactIdentityID,
		DisplayName:              row.DisplayName,
		Metadata:                 row.Metadata,
		CreatedAt:                row.CreatedAt,
		UpdatedAt:                row.UpdatedAt,
	}
}

func dbMemberToDomain(row db.PlatformHouseholdMember) (household.Member, error) {
	m := household.Member{
		ID:             row.ID,
		HouseholdID:    row.HouseholdID,
		IdentityID:     row.IdentityID,
		Role:           household.Role(row.Role),
		Status:         household.Status(row.Status),
		IsManager:      row.IsManager,
		IsAccountOwner: row.IsAccountOwner,
		Capabilities:   row.Capabilities,
		AddedAt:        row.AddedAt,
		Birthdate:      pgDateToTime(row.Birthdate),
	}
	if row.RemovedAt.Valid {
		t := row.RemovedAt.Time
		m.RemovedAt = &t
	}
	return m, nil
}

func dbPORToDomain(row db.PlatformParentOfRecord) household.ParentOfRecord {
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
