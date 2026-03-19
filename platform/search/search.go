package search

import (
	"context"

	"github.com/google/uuid"
)

// SearchStore defines database operations for full-text search.
type SearchStore interface {
	Search(ctx context.Context, companyID uuid.UUID, query string, scopes []string, limit, offset int) ([]SearchResult, int, error)
}

// SearchResult represents a search hit.
type SearchResult struct {
	ID         string  `json:"id"`
	Type       string  `json:"type"`
	Title      string  `json:"title"`
	Snippet    string  `json:"snippet"`
	Score      float64 `json:"score"`
	ResourceID string  `json:"resource_id"`
}

// Service provides search operations.
type Service struct {
	store SearchStore
}

// NewService creates a new search service.
func NewService(store SearchStore) *Service {
	return &Service{store: store}
}

// Search performs a full-text search across registered scopes.
func (s *Service) Search(ctx context.Context, companyID uuid.UUID, query string, scopes []string, limit, offset int) ([]SearchResult, int, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return s.store.Search(ctx, companyID, query, scopes, limit, offset)
}
