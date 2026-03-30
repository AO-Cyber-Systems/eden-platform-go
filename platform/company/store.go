package company

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// CompanyType defines the type of company in the hierarchy.
type CompanyType string

const (
	CompanyTypeHolding    CompanyType = "holding"
	CompanyTypeSubsidiary CompanyType = "subsidiary"
	CompanyTypeBrand      CompanyType = "brand"
	CompanyTypeStandalone CompanyType = "standalone"
	CompanyTypePersonal   CompanyType = "personal"
)

// Company represents a company in the hierarchy.
type Company struct {
	ID                 uuid.UUID
	Name               string
	Slug               string
	ParentCompanyID    *uuid.UUID
	CompanyType        CompanyType
	InheritedRoleCap   *int
	InheritedAccessLvl *string
	Settings           json.RawMessage
	IsActive           bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// CompanyHierarchy represents a row in the closure table.
type CompanyHierarchy struct {
	AncestorID   uuid.UUID
	DescendantID uuid.UUID
	Generations  int
}

// CompanyStore defines database operations for company hierarchy.
type CompanyStore interface {
	// Company CRUD
	CreateCompany(ctx context.Context, c Company) (Company, error)
	GetCompany(ctx context.Context, id uuid.UUID) (Company, error)
	UpdateCompany(ctx context.Context, c Company) (Company, error)
	ListCompanies(ctx context.Context) ([]Company, error)

	// Hierarchy closure table operations
	InsertHierarchyEntries(ctx context.Context, entries []CompanyHierarchy) error
	GetAncestors(ctx context.Context, companyID uuid.UUID) ([]CompanyHierarchy, error)
	GetDescendants(ctx context.Context, companyID uuid.UUID) ([]CompanyHierarchy, error)
	GetSelfAndDescendantIDs(ctx context.Context, companyID uuid.UUID) ([]uuid.UUID, error)
	DeleteHierarchyEntries(ctx context.Context, descendantID uuid.UUID) error
}
