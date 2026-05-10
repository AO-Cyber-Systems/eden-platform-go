package composition

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/consent"
	"github.com/google/uuid"
)

// inMemoryConsentStore mirrors the platform/consent test memStore for
// dev-time use. Append-only is enforced by callers via the consent
// service; this store only persists what it's told.
type inMemoryConsentStore struct {
	mu      sync.Mutex
	entries map[uuid.UUID]consent.Entry
}

func newInMemoryConsentStore() *inMemoryConsentStore {
	return &inMemoryConsentStore{entries: map[uuid.UUID]consent.Entry{}}
}

var _ consent.Store = (*inMemoryConsentStore)(nil)

func (s *inMemoryConsentStore) InsertEntry(_ context.Context, e consent.Entry) (consent.Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e.ID = uuid.New()
	now := time.Now().UTC()
	if e.GrantedAt.IsZero() {
		e.GrantedAt = now
	}
	e.CreatedAt = now
	s.entries[e.ID] = e
	return e, nil
}

func (s *inMemoryConsentStore) GetEntry(_ context.Context, id uuid.UUID) (consent.Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok {
		return consent.Entry{}, consent.ErrNotFound
	}
	return e, nil
}

func (s *inMemoryConsentStore) LatestForPurpose(_ context.Context, principal uuid.UUID, purpose consent.Purpose) (consent.Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var best *consent.Entry
	for i, e := range s.entries {
		if e.PrincipalMemberID != principal || e.Purpose != purpose {
			continue
		}
		if best == nil ||
			e.GrantedAt.After(best.GrantedAt) ||
			(e.GrantedAt.Equal(best.GrantedAt) && e.CreatedAt.After(best.CreatedAt)) {
			ent := s.entries[i]
			best = &ent
		}
	}
	if best == nil {
		return consent.Entry{}, consent.ErrNotFound
	}
	return *best, nil
}

func (s *inMemoryConsentStore) ListForPrincipal(_ context.Context, principal uuid.UUID, limit, offset int32) ([]consent.Entry, error) {
	return s.list(func(e consent.Entry) bool { return e.PrincipalMemberID == principal }, limit, offset), nil
}

func (s *inMemoryConsentStore) ListByPurpose(_ context.Context, purpose consent.Purpose, limit, offset int32) ([]consent.Entry, error) {
	return s.list(func(e consent.Entry) bool { return e.Purpose == purpose }, limit, offset), nil
}

func (s *inMemoryConsentStore) ListForHousehold(_ context.Context, hh uuid.UUID, limit, offset int32) ([]consent.Entry, error) {
	return s.list(func(e consent.Entry) bool { return e.HouseholdID == hh }, limit, offset), nil
}

func (s *inMemoryConsentStore) list(pred func(consent.Entry) bool, limit, offset int32) []consent.Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []consent.Entry
	for _, e := range s.entries {
		if pred(e) {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].GrantedAt.Equal(out[j].GrantedAt) {
			return out[i].GrantedAt.After(out[j].GrantedAt)
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if int(offset) >= len(out) {
		return nil
	}
	out = out[offset:]
	if int(limit) > 0 && int(limit) < len(out) {
		out = out[:limit]
	}
	return out
}
