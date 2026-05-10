package composition

import (
	"context"
	"sync"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/household"
	"github.com/google/uuid"
)

// inMemoryHouseholdStore is a thread-safe map-backed household.Store for
// dev / smoke-test runs. The platform/household package's own memStore is
// unexported (test-only) and would force composition_test to import test
// files; this is the public dev twin.
//
// Behaviour mirrors platform/pgstore.HouseholdStore for the operations
// the service uses; anything not currently exercised is a TODO marker so
// production composers spot the gap.
type inMemoryHouseholdStore struct {
	mu          sync.Mutex
	households  map[uuid.UUID]household.Household
	members     map[uuid.UUID]household.Member
	parentLinks map[uuid.UUID]household.ParentOfRecord
}

func newInMemoryHouseholdStore() *inMemoryHouseholdStore {
	return &inMemoryHouseholdStore{
		households:  map[uuid.UUID]household.Household{},
		members:     map[uuid.UUID]household.Member{},
		parentLinks: map[uuid.UUID]household.ParentOfRecord{},
	}
}

var _ household.Store = (*inMemoryHouseholdStore)(nil)

func (s *inMemoryHouseholdStore) CreateHousehold(_ context.Context, h household.Household) (household.Household, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h.ID = uuid.New()
	now := time.Now().UTC()
	h.CreatedAt = now
	h.UpdatedAt = now
	s.households[h.ID] = h
	return h, nil
}

func (s *inMemoryHouseholdStore) GetHousehold(_ context.Context, id uuid.UUID) (household.Household, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.households[id]
	if !ok {
		return household.Household{}, household.ErrNotFound
	}
	return h, nil
}

func (s *inMemoryHouseholdStore) UpdateHousehold(_ context.Context, h household.Household) (household.Household, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.households[h.ID]
	if !ok {
		return household.Household{}, household.ErrNotFound
	}
	existing.DisplayName = h.DisplayName
	existing.Metadata = h.Metadata
	existing.UpdatedAt = time.Now().UTC()
	s.households[h.ID] = existing
	return existing, nil
}

func (s *inMemoryHouseholdStore) DeleteHousehold(_ context.Context, id uuid.UUID) error {
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

func (s *inMemoryHouseholdStore) AddMember(_ context.Context, m household.Member) (household.Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m.ID = uuid.New()
	m.AddedAt = time.Now().UTC()
	s.members[m.ID] = m
	return m, nil
}

func (s *inMemoryHouseholdStore) GetMember(_ context.Context, id uuid.UUID) (household.Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.members[id]
	if !ok {
		return household.Member{}, household.ErrNotFound
	}
	return m, nil
}

func (s *inMemoryHouseholdStore) UpdateMemberRole(_ context.Context, memberID uuid.UUID, role household.Role, caps household.Capabilities) (household.Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.members[memberID]
	if !ok {
		return household.Member{}, household.ErrNotFound
	}
	m.Role = role
	m.Capabilities = caps
	s.members[memberID] = m
	return m, nil
}

func (s *inMemoryHouseholdStore) RemoveMember(_ context.Context, memberID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.members[memberID]
	if !ok {
		return household.ErrNotFound
	}
	now := time.Now().UTC()
	m.Status = household.StatusRemoved
	m.RemovedAt = &now
	s.members[memberID] = m
	return nil
}

func (s *inMemoryHouseholdStore) ListMembers(_ context.Context, householdID uuid.UUID) ([]household.Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []household.Member
	for _, m := range s.members {
		if m.HouseholdID == householdID && m.Status != household.StatusRemoved {
			out = append(out, m)
		}
	}
	return out, nil
}

func (s *inMemoryHouseholdStore) ListHouseholdsForUser(_ context.Context, userID uuid.UUID) ([]household.Household, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[uuid.UUID]bool{}
	var out []household.Household
	for _, m := range s.members {
		if m.UserID == userID && m.Status != household.StatusRemoved && !seen[m.HouseholdID] {
			if h, ok := s.households[m.HouseholdID]; ok {
				out = append(out, h)
				seen[m.HouseholdID] = true
			}
		}
	}
	return out, nil
}

func (s *inMemoryHouseholdStore) EstablishParentOfRecord(_ context.Context, childMemberID, parentMemberID uuid.UUID) (household.ParentOfRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	por := household.ParentOfRecord{
		ID:             uuid.New(),
		ChildMemberID:  childMemberID,
		ParentMemberID: parentMemberID,
		EstablishedAt:  time.Now().UTC(),
	}
	s.parentLinks[por.ID] = por
	return por, nil
}

func (s *inMemoryHouseholdStore) RevokeParentOfRecord(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	por, ok := s.parentLinks[id]
	if !ok {
		return household.ErrNotFound
	}
	now := time.Now().UTC()
	por.RevokedAt = &now
	s.parentLinks[id] = por
	return nil
}

func (s *inMemoryHouseholdStore) ListParentsOfRecord(_ context.Context, childMemberID uuid.UUID) ([]household.ParentOfRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []household.ParentOfRecord
	for _, p := range s.parentLinks {
		if p.ChildMemberID == childMemberID && p.RevokedAt == nil {
			out = append(out, p)
		}
	}
	return out, nil
}

func (s *inMemoryHouseholdStore) ListChildrenForParent(_ context.Context, parentMemberID uuid.UUID) ([]household.ParentOfRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []household.ParentOfRecord
	for _, p := range s.parentLinks {
		if p.ParentMemberID == parentMemberID && p.RevokedAt == nil {
			out = append(out, p)
		}
	}
	return out, nil
}
