package household

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// memStore is an in-memory Store implementation used by service unit tests.
//
// It is intentionally simple: maps + a mutex. Production code uses
// pgstore.HouseholdStore; this exists to keep household_test.go fast and
// dependency-free.
type memStore struct {
	mu          sync.Mutex
	households  map[uuid.UUID]Household
	members     map[uuid.UUID]Member
	parentLinks map[uuid.UUID]ParentOfRecord
}

func newMemStore() *memStore {
	return &memStore{
		households:  map[uuid.UUID]Household{},
		members:     map[uuid.UUID]Member{},
		parentLinks: map[uuid.UUID]ParentOfRecord{},
	}
}

func (s *memStore) CreateHousehold(_ context.Context, h Household) (Household, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h.ID = uuid.New()
	now := time.Now().UTC()
	h.CreatedAt = now
	h.UpdatedAt = now
	s.households[h.ID] = h
	return h, nil
}

func (s *memStore) GetHousehold(_ context.Context, id uuid.UUID) (Household, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.households[id]
	if !ok {
		return Household{}, ErrNotFound
	}
	return h, nil
}

func (s *memStore) UpdateHousehold(_ context.Context, h Household) (Household, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.households[h.ID]
	if !ok {
		return Household{}, ErrNotFound
	}
	existing.DisplayName = h.DisplayName
	existing.Metadata = h.Metadata
	existing.UpdatedAt = time.Now().UTC()
	s.households[h.ID] = existing
	return existing, nil
}

func (s *memStore) DeleteHousehold(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.households, id)
	for mid, m := range s.members {
		if m.HouseholdID == id {
			delete(s.members, mid)
		}
	}
	return nil
}

func (s *memStore) AddMember(_ context.Context, m Member) (Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m.ID = uuid.New()
	m.AddedAt = time.Now().UTC()
	s.members[m.ID] = m
	return m, nil
}

func (s *memStore) GetMember(_ context.Context, id uuid.UUID) (Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.members[id]
	if !ok {
		return Member{}, ErrNotFound
	}
	return m, nil
}

func (s *memStore) UpdateMemberRole(_ context.Context, memberID uuid.UUID, role Role, caps Capabilities) (Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.members[memberID]
	if !ok {
		return Member{}, ErrNotFound
	}
	m.Role = role
	m.Capabilities = caps
	s.members[memberID] = m
	return m, nil
}

func (s *memStore) RemoveMember(_ context.Context, memberID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.members[memberID]
	if !ok {
		return ErrNotFound
	}
	now := time.Now().UTC()
	m.Status = StatusRemoved
	m.RemovedAt = &now
	s.members[memberID] = m
	return nil
}

func (s *memStore) ListMembers(_ context.Context, householdID uuid.UUID) ([]Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Member
	for _, m := range s.members {
		if m.HouseholdID == householdID && m.Status != StatusRemoved {
			out = append(out, m)
		}
	}
	return out, nil
}

func (s *memStore) ListHouseholdsForUser(_ context.Context, userID uuid.UUID) ([]Household, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[uuid.UUID]bool{}
	var out []Household
	for _, m := range s.members {
		if m.UserID == userID && m.Status != StatusRemoved && !seen[m.HouseholdID] {
			if h, ok := s.households[m.HouseholdID]; ok {
				out = append(out, h)
				seen[m.HouseholdID] = true
			}
		}
	}
	return out, nil
}

func (s *memStore) EstablishParentOfRecord(_ context.Context, childMemberID, parentMemberID uuid.UUID) (ParentOfRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	por := ParentOfRecord{
		ID:             uuid.New(),
		ChildMemberID:  childMemberID,
		ParentMemberID: parentMemberID,
		EstablishedAt:  time.Now().UTC(),
	}
	s.parentLinks[por.ID] = por
	return por, nil
}

func (s *memStore) RevokeParentOfRecord(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	por, ok := s.parentLinks[id]
	if !ok {
		return ErrNotFound
	}
	now := time.Now().UTC()
	por.RevokedAt = &now
	s.parentLinks[id] = por
	return nil
}

func (s *memStore) ListParentsOfRecord(_ context.Context, childMemberID uuid.UUID) ([]ParentOfRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []ParentOfRecord
	for _, p := range s.parentLinks {
		if p.ChildMemberID == childMemberID && p.RevokedAt == nil {
			out = append(out, p)
		}
	}
	return out, nil
}

func (s *memStore) ListChildrenForParent(_ context.Context, parentMemberID uuid.UUID) ([]ParentOfRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []ParentOfRecord
	for _, p := range s.parentLinks {
		if p.ParentMemberID == parentMemberID && p.RevokedAt == nil {
			out = append(out, p)
		}
	}
	return out, nil
}

