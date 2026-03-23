package pgstore

import (
	"context"
	"fmt"

	"github.com/aocybersystems/eden-platform-go/internal/db"
	"github.com/aocybersystems/eden-platform-go/platform/company"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var _ company.CompanyStore = (*CompanyStore)(nil)

// CompanyStore implements company.CompanyStore backed by PostgreSQL via pgx and sqlc.
// It also satisfies the connectapi.userCompanyLister interface.
type CompanyStore struct {
	pool *pgxpool.Pool
}

// NewCompanyStore creates a new PostgreSQL-backed company store.
func NewCompanyStore(pool *pgxpool.Pool) *CompanyStore {
	return &CompanyStore{pool: pool}
}

func (s *CompanyStore) queries() *db.Queries {
	return db.New(s.pool)
}

func (s *CompanyStore) CreateCompany(ctx context.Context, c company.Company) (company.Company, error) {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	if c.CompanyType == "" {
		c.CompanyType = company.CompanyTypeStandalone
	}
	if c.Settings == nil {
		c.Settings = []byte(`{"enabled_features":[]}`)
	}

	var inheritedRoleCap *int32
	if c.InheritedRoleCap != nil {
		v := int32(*c.InheritedRoleCap)
		inheritedRoleCap = &v
	}

	row, err := s.queries().CreateCompany(ctx, db.CreateCompanyParams{
		ID:                   c.ID,
		Name:                 c.Name,
		Slug:                 c.Slug,
		ParentCompanyID:      uuidToPgtype(c.ParentCompanyID),
		CompanyType:          string(c.CompanyType),
		InheritedRoleCap:     inheritedRoleCap,
		InheritedAccessLevel: c.InheritedAccessLvl,
		Settings:             c.Settings,
	})
	if err != nil {
		return company.Company{}, fmt.Errorf("create company: %w", err)
	}
	return dbCompanyToDomain(row), nil
}

func (s *CompanyStore) GetCompany(ctx context.Context, id uuid.UUID) (company.Company, error) {
	row, err := s.queries().GetCompany(ctx, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return company.Company{}, fmt.Errorf("company not found")
		}
		return company.Company{}, fmt.Errorf("get company: %w", err)
	}
	return dbCompanyToDomain(row), nil
}

func (s *CompanyStore) UpdateCompany(ctx context.Context, c company.Company) (company.Company, error) {
	var inheritedRoleCap *int32
	if c.InheritedRoleCap != nil {
		v := int32(*c.InheritedRoleCap)
		inheritedRoleCap = &v
	}

	row, err := s.queries().UpdateCompany(ctx, db.UpdateCompanyParams{
		ID:                   c.ID,
		Name:                 c.Name,
		Slug:                 c.Slug,
		CompanyType:          string(c.CompanyType),
		InheritedRoleCap:     inheritedRoleCap,
		InheritedAccessLevel: c.InheritedAccessLvl,
		Settings:             c.Settings,
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			return company.Company{}, fmt.Errorf("company not found")
		}
		return company.Company{}, fmt.Errorf("update company: %w", err)
	}
	return dbCompanyToDomain(row), nil
}

func (s *CompanyStore) ListCompanies(ctx context.Context) ([]company.Company, error) {
	rows, err := s.queries().ListCompanies(ctx)
	if err != nil {
		return nil, fmt.Errorf("list companies: %w", err)
	}
	companies := make([]company.Company, len(rows))
	for i, row := range rows {
		companies[i] = dbCompanyToDomain(row)
	}
	return companies, nil
}

// -- Hierarchy operations --

func (s *CompanyStore) InsertHierarchyEntries(ctx context.Context, entries []company.CompanyHierarchy) error {
	q := s.queries()
	for _, entry := range entries {
		if err := q.InsertHierarchyEntry(ctx, db.InsertHierarchyEntryParams{
			AncestorID:   entry.AncestorID,
			DescendantID: entry.DescendantID,
			Generations:  int32(entry.Generations),
		}); err != nil {
			return fmt.Errorf("insert hierarchy entry: %w", err)
		}
	}
	return nil
}

func (s *CompanyStore) GetAncestors(ctx context.Context, companyID uuid.UUID) ([]company.CompanyHierarchy, error) {
	rows, err := s.queries().GetAncestors(ctx, companyID)
	if err != nil {
		return nil, fmt.Errorf("get ancestors: %w", err)
	}
	entries := make([]company.CompanyHierarchy, len(rows))
	for i, row := range rows {
		entries[i] = company.CompanyHierarchy{
			AncestorID:   row.AncestorID,
			DescendantID: row.DescendantID,
			Generations:  int(row.Generations),
		}
	}
	return entries, nil
}

func (s *CompanyStore) GetDescendants(ctx context.Context, companyID uuid.UUID) ([]company.CompanyHierarchy, error) {
	rows, err := s.queries().GetDescendants(ctx, companyID)
	if err != nil {
		return nil, fmt.Errorf("get descendants: %w", err)
	}
	entries := make([]company.CompanyHierarchy, len(rows))
	for i, row := range rows {
		entries[i] = company.CompanyHierarchy{
			AncestorID:   row.AncestorID,
			DescendantID: row.DescendantID,
			Generations:  int(row.Generations),
		}
	}
	return entries, nil
}

func (s *CompanyStore) GetSelfAndDescendantIDs(ctx context.Context, companyID uuid.UUID) ([]uuid.UUID, error) {
	ids, err := s.queries().GetSelfAndDescendantIDs(ctx, companyID)
	if err != nil {
		return nil, fmt.Errorf("get descendant ids: %w", err)
	}
	return ids, nil
}

func (s *CompanyStore) DeleteHierarchyEntries(ctx context.Context, descendantID uuid.UUID) error {
	return s.queries().DeleteHierarchyEntries(ctx, descendantID)
}

// ListCompaniesForUser satisfies the connectapi.userCompanyLister interface.
func (s *CompanyStore) ListCompaniesForUser(ctx context.Context, userID uuid.UUID) ([]company.Company, error) {
	rows, err := s.queries().ListCompaniesForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list companies for user: %w", err)
	}
	companies := make([]company.Company, len(rows))
	for i, row := range rows {
		companies[i] = dbCompanyToDomain(row)
	}
	return companies, nil
}

// -- Type conversion --

func dbCompanyToDomain(c db.Company) company.Company {
	var inheritedRoleCap *int
	if c.InheritedRoleCap != nil {
		v := int(*c.InheritedRoleCap)
		inheritedRoleCap = &v
	}

	return company.Company{
		ID:                 c.ID,
		Name:               c.Name,
		Slug:               c.Slug,
		ParentCompanyID:    pgtypeUUID(c.ParentCompanyID),
		CompanyType:        company.CompanyType(c.CompanyType),
		InheritedRoleCap:   inheritedRoleCap,
		InheritedAccessLvl: c.InheritedAccessLevel,
		Settings:           c.Settings,
		IsActive:           c.IsActive,
		CreatedAt:          c.CreatedAt,
		UpdatedAt:          c.UpdatedAt,
	}
}
