package search

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

type mockSearchStore struct {
	lastLimit int
}

func (m *mockSearchStore) Search(_ context.Context, _ uuid.UUID, _ string, _ []string, limit, offset int) ([]SearchResult, int, error) {
	m.lastLimit = limit
	return nil, 0, nil
}

func TestSearchService_DefaultLimit(t *testing.T) {
	store := &mockSearchStore{}
	svc := NewService(store)

	_, _, _ = svc.Search(context.Background(), uuid.New(), "query", nil, 0, 0)
	if store.lastLimit != 20 {
		t.Errorf("Default limit = %d, want 20", store.lastLimit)
	}

	_, _, _ = svc.Search(context.Background(), uuid.New(), "query", nil, -5, 0)
	if store.lastLimit != 20 {
		t.Errorf("Negative limit = %d, want 20", store.lastLimit)
	}
}

func TestSearchService_MaxLimit(t *testing.T) {
	store := &mockSearchStore{}
	svc := NewService(store)

	_, _, _ = svc.Search(context.Background(), uuid.New(), "query", nil, 500, 0)
	if store.lastLimit != 100 {
		t.Errorf("Max limit = %d, want 100", store.lastLimit)
	}
}
