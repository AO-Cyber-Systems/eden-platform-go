package household

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// memStore is an in-memory Store implementation used by service unit tests.
// It mirrors the invariant-relevant behaviour of pgstore.HouseholdStore
// (at-most-one account_owner, household-scoped lookups) so service guards are
// exercised without a database.
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

var _ Store = (*memStore)(nil)

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

func (s *memStore) CreateHouseholdWithOwner(_ context.Context, h Household, owner Member) (Household, Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	h.ID = uuid.New()
	h.CreatedAt = now
	h.UpdatedAt = now
	owner.ID = uuid.New()
	owner.HouseholdID = h.ID
	owner.AddedAt = now
	if owner.Status == "" {
		owner.Status = StatusActive
	}
	if len(owner.Capabilities) == 0 {
		owner.Capabilities = json.RawMessage("{}")
	}
	// Mirrors the pg single-tx insert: household + owner commit together, or
	// neither is persisted. A fresh household can never collide with an existing
	// account_owner; the guard documents the invariant and keeps partial state
	// from ever being written.
	if owner.IsAccountOwner {
		for _, e := range s.members {
			if e.HouseholdID == h.ID && e.IsAccountOwner && e.Status != StatusRemoved {
				return Household{}, Member{}, ErrAccountOwnerExists
			}
		}
	}
	s.households[h.ID] = h
	s.members[owner.ID] = owner
	return h, owner, nil
}

func (s *memStore) GetHouseholdByID(_ context.Context, id uuid.UUID) (Household, error) {
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
	if m.IsAccountOwner {
		for _, e := range s.members {
			if e.HouseholdID == m.HouseholdID && e.IsAccountOwner && e.Status != StatusRemoved {
				return Member{}, ErrAccountOwnerExists
			}
		}
	}
	m.ID = uuid.New()
	m.AddedAt = time.Now().UTC()
	if len(m.Capabilities) == 0 {
		m.Capabilities = json.RawMessage("{}")
	}
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

func (s *memStore) GetMemberByIdentity(_ context.Context, householdID, identityID uuid.UUID) (Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.members {
		if m.HouseholdID == householdID && m.IdentityID == identityID && m.Status != StatusRemoved {
			return m, nil
		}
	}
	return Member{}, ErrNotFound
}

func (s *memStore) UpdateMemberRole(_ context.Context, memberID uuid.UUID, role Role, isManager, isAccountOwner bool, caps []byte) (Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.members[memberID]
	if !ok {
		return Member{}, ErrNotFound
	}
	if isAccountOwner && !m.IsAccountOwner {
		for id, e := range s.members {
			if id != memberID && e.HouseholdID == m.HouseholdID && e.IsAccountOwner && e.Status != StatusRemoved {
				return Member{}, ErrAccountOwnerExists
			}
		}
	}
	m.Role = role
	m.IsManager = isManager
	m.IsAccountOwner = isAccountOwner
	if len(caps) > 0 {
		m.Capabilities = json.RawMessage(caps)
	}
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
	sort.Slice(out, func(i, j int) bool { return out[i].AddedAt.Before(out[j].AddedAt) })
	return out, nil
}

func (s *memStore) CountManagers(_ context.Context, householdID uuid.UUID) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, m := range s.members {
		if m.HouseholdID == householdID && m.IsManager && m.Status != StatusRemoved {
			n++
		}
	}
	return n, nil
}

func (s *memStore) GetHouseholdForIdentity(_ context.Context, identityID uuid.UUID) (Household, Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var found *Member
	for _, m := range s.members {
		mm := m
		if mm.IdentityID == identityID && mm.Status != StatusRemoved {
			if found == nil || mm.AddedAt.Before(found.AddedAt) {
				found = &mm
			}
		}
	}
	if found == nil {
		return Household{}, Member{}, ErrNotFound
	}
	h, ok := s.households[found.HouseholdID]
	if !ok {
		return Household{}, Member{}, ErrNotFound
	}
	return h, *found, nil
}

func (s *memStore) ListHouseholdsForIdentity(_ context.Context, identityID uuid.UUID) ([]Household, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[uuid.UUID]bool{}
	var out []Household
	for _, m := range s.members {
		if m.IdentityID == identityID && m.Status != StatusRemoved && !seen[m.HouseholdID] {
			if h, ok := s.households[m.HouseholdID]; ok {
				out = append(out, h)
				seen[m.HouseholdID] = true
			}
		}
	}
	return out, nil
}

func (s *memStore) SetAccountOwner(_ context.Context, householdID, newOwnerMemberID uuid.UUID) (Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	target, ok := s.members[newOwnerMemberID]
	if !ok || target.HouseholdID != householdID || target.Status == StatusRemoved {
		return Member{}, ErrNotFound
	}
	for id, m := range s.members {
		if m.HouseholdID == householdID && m.IsAccountOwner {
			m.IsAccountOwner = false
			s.members[id] = m
		}
	}
	target.IsAccountOwner = true
	s.members[newOwnerMemberID] = target
	return target, nil
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
