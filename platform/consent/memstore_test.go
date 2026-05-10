package consent

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// memStore is an in-memory Store used by service unit tests.
type memStore struct {
	mu      sync.Mutex
	entries map[uuid.UUID]Entry
}

func newMemStore() *memStore {
	return &memStore{entries: map[uuid.UUID]Entry{}}
}

func (s *memStore) InsertEntry(_ context.Context, e Entry) (Entry, error) {
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

func (s *memStore) GetEntry(_ context.Context, id uuid.UUID) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok {
		return Entry{}, ErrNotFound
	}
	return e, nil
}

func (s *memStore) LatestForPurpose(_ context.Context, principal uuid.UUID, purpose Purpose) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var best *Entry
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
		return Entry{}, ErrNotFound
	}
	return *best, nil
}

func (s *memStore) ListForPrincipal(_ context.Context, principal uuid.UUID, limit, offset int32) ([]Entry, error) {
	return s.list(func(e Entry) bool { return e.PrincipalMemberID == principal }, limit, offset), nil
}

func (s *memStore) ListByPurpose(_ context.Context, purpose Purpose, limit, offset int32) ([]Entry, error) {
	return s.list(func(e Entry) bool { return e.Purpose == purpose }, limit, offset), nil
}

func (s *memStore) ListForHousehold(_ context.Context, hh uuid.UUID, limit, offset int32) ([]Entry, error) {
	return s.list(func(e Entry) bool { return e.HouseholdID == hh }, limit, offset), nil
}

func (s *memStore) list(pred func(Entry) bool, limit, offset int32) []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Entry
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
