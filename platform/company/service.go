package company

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Service provides company hierarchy operations.
type Service struct {
	store CompanyStore
}

// NewService creates a new company service.
func NewService(store CompanyStore) *Service {
	return &Service{store: store}
}

// CreateCompany creates a new company and inserts closure table entries.
// If parentID is non-nil, the company is placed under the parent in the hierarchy.
func (s *Service) CreateCompany(ctx context.Context, name, slug string, companyType CompanyType, parentID *uuid.UUID, settings json.RawMessage) (Company, error) {
	if strings.TrimSpace(name) == "" {
		return Company{}, fmt.Errorf("company name is required")
	}
	if slug == "" {
		slug = generateSlug(name)
	}
	if companyType == "" {
		companyType = CompanyTypeStandalone
	}
	if settings == nil {
		settings = json.RawMessage(`{"enabled_features": []}`)
	}

	company := Company{
		ID:              uuid.New(),
		Name:            name,
		Slug:            slug,
		ParentCompanyID: parentID,
		CompanyType:     companyType,
		Settings:        settings,
		IsActive:        true,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}

	created, err := s.store.CreateCompany(ctx, company)
	if err != nil {
		return Company{}, fmt.Errorf("create company: %w", err)
	}

	// Build closure table entries
	entries := []CompanyHierarchy{
		{AncestorID: created.ID, DescendantID: created.ID, Generations: 0}, // self-reference
	}

	if parentID != nil {
		// Get all ancestors of the parent and create entries for new child
		ancestors, err := s.store.GetAncestors(ctx, *parentID)
		if err != nil {
			return Company{}, fmt.Errorf("get parent ancestors: %w", err)
		}
		for _, a := range ancestors {
			entries = append(entries, CompanyHierarchy{
				AncestorID:   a.AncestorID,
				DescendantID: created.ID,
				Generations:  a.Generations + 1,
			})
		}
	}

	if err := s.store.InsertHierarchyEntries(ctx, entries); err != nil {
		return Company{}, fmt.Errorf("insert hierarchy entries: %w", err)
	}

	return created, nil
}

// GetCompany returns a company by ID.
func (s *Service) GetCompany(ctx context.Context, id uuid.UUID) (Company, error) {
	return s.store.GetCompany(ctx, id)
}

// UpdateCompany updates an existing company record.
func (s *Service) UpdateCompany(ctx context.Context, c Company) (Company, error) {
	if strings.TrimSpace(c.Name) == "" {
		return Company{}, fmt.Errorf("company name is required")
	}
	if strings.TrimSpace(c.Slug) == "" {
		c.Slug = generateSlug(c.Name)
	}
	return s.store.UpdateCompany(ctx, c)
}

// GetAncestors returns ancestors ordered by generations ASC (nearest first).
func (s *Service) GetAncestors(ctx context.Context, companyID uuid.UUID) ([]Company, error) {
	hierarchy, err := s.store.GetAncestors(ctx, companyID)
	if err != nil {
		return nil, fmt.Errorf("get ancestors: %w", err)
	}

	companies := make([]Company, 0, len(hierarchy))
	for _, h := range hierarchy {
		if h.AncestorID == companyID {
			continue // skip self
		}
		c, err := s.store.GetCompany(ctx, h.AncestorID)
		if err != nil {
			continue
		}
		companies = append(companies, c)
	}
	return companies, nil
}

// GetSelfAndDescendantIDs returns the company ID and all descendant IDs.
// Used for hierarchy-scoped queries.
func (s *Service) GetSelfAndDescendantIDs(ctx context.Context, companyID uuid.UUID) ([]uuid.UUID, error) {
	return s.store.GetSelfAndDescendantIDs(ctx, companyID)
}

// GetEffectiveSettings walks ancestors (nearest first) for merged settings.
// Child settings override parent settings.
func (s *Service) GetEffectiveSettings(ctx context.Context, companyID uuid.UUID) (json.RawMessage, error) {
	company, err := s.store.GetCompany(ctx, companyID)
	if err != nil {
		return nil, fmt.Errorf("get company: %w", err)
	}

	// Start with own settings
	merged := make(map[string]interface{})
	if company.Settings != nil {
		if err := json.Unmarshal(company.Settings, &merged); err != nil {
			return nil, fmt.Errorf("unmarshal company settings: %w", err)
		}
	}

	// Walk ancestors (nearest first) and fill in missing keys
	ancestors, err := s.GetAncestors(ctx, companyID)
	if err != nil {
		return nil, err
	}

	for _, ancestor := range ancestors {
		if ancestor.Settings == nil {
			continue
		}
		var ancestorSettings map[string]interface{}
		if err := json.Unmarshal(ancestor.Settings, &ancestorSettings); err != nil {
			continue
		}
		for key, val := range ancestorSettings {
			if _, exists := merged[key]; !exists {
				merged[key] = val
			}
		}
	}

	result, err := json.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("marshal merged settings: %w", err)
	}
	return result, nil
}

// IsDescendantOf checks if companyID is a descendant of ancestorID.
func (s *Service) IsDescendantOf(ctx context.Context, companyID, ancestorID uuid.UUID) (bool, error) {
	descendants, err := s.store.GetSelfAndDescendantIDs(ctx, ancestorID)
	if err != nil {
		return false, err
	}
	for _, d := range descendants {
		if d == companyID {
			return true, nil
		}
	}
	return false, nil
}

func generateSlug(name string) string {
	slug := strings.ToLower(strings.TrimSpace(name))
	slug = strings.ReplaceAll(slug, " ", "-")
	var result strings.Builder
	for _, c := range slug {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			result.WriteRune(c)
		}
	}
	s := result.String()
	if s == "" {
		s = "company"
	}
	return s + "-" + time.Now().Format("150405")
}
