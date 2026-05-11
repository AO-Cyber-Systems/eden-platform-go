package integration

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/audit"
	"github.com/aocybersystems/eden-platform-go/platform/consent"
	"github.com/aocybersystems/eden-platform-go/platform/household"
	"github.com/google/uuid"
)

// memHouseholdStore is a thread-safe in-memory household.Store mirroring the
// public dev twin used by internal/aoid/composition. It cannot be imported
// from there (internal/) so it is duplicated here.
type memHouseholdStore struct {
	mu          sync.Mutex
	households  map[uuid.UUID]household.Household
	members     map[uuid.UUID]household.Member
	parentLinks map[uuid.UUID]household.ParentOfRecord
}

func newMemHouseholdStore() *memHouseholdStore {
	return &memHouseholdStore{
		households:  map[uuid.UUID]household.Household{},
		members:     map[uuid.UUID]household.Member{},
		parentLinks: map[uuid.UUID]household.ParentOfRecord{},
	}
}

var _ household.Store = (*memHouseholdStore)(nil)

func (s *memHouseholdStore) CreateHousehold(_ context.Context, h household.Household) (household.Household, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h.ID = uuid.New()
	now := time.Now().UTC()
	h.CreatedAt = now
	h.UpdatedAt = now
	s.households[h.ID] = h
	return h, nil
}

func (s *memHouseholdStore) GetHousehold(_ context.Context, id uuid.UUID) (household.Household, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.households[id]
	if !ok {
		return household.Household{}, household.ErrNotFound
	}
	return h, nil
}

func (s *memHouseholdStore) UpdateHousehold(_ context.Context, h household.Household) (household.Household, error) {
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

func (s *memHouseholdStore) DeleteHousehold(_ context.Context, id uuid.UUID) error {
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

func (s *memHouseholdStore) AddMember(_ context.Context, m household.Member) (household.Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m.ID = uuid.New()
	m.AddedAt = time.Now().UTC()
	s.members[m.ID] = m
	return m, nil
}

func (s *memHouseholdStore) GetMember(_ context.Context, id uuid.UUID) (household.Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.members[id]
	if !ok {
		return household.Member{}, household.ErrNotFound
	}
	return m, nil
}

func (s *memHouseholdStore) UpdateMemberRole(_ context.Context, memberID uuid.UUID, role household.Role, caps household.Capabilities) (household.Member, error) {
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

func (s *memHouseholdStore) RemoveMember(_ context.Context, memberID uuid.UUID) error {
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

func (s *memHouseholdStore) ListMembers(_ context.Context, householdID uuid.UUID) ([]household.Member, error) {
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

func (s *memHouseholdStore) ListHouseholdsForUser(_ context.Context, userID uuid.UUID) ([]household.Household, error) {
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

func (s *memHouseholdStore) EstablishParentOfRecord(_ context.Context, childMemberID, parentMemberID uuid.UUID) (household.ParentOfRecord, error) {
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

func (s *memHouseholdStore) RevokeParentOfRecord(_ context.Context, id uuid.UUID) error {
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

func (s *memHouseholdStore) ListParentsOfRecord(_ context.Context, childMemberID uuid.UUID) ([]household.ParentOfRecord, error) {
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

func (s *memHouseholdStore) ListChildrenForParent(_ context.Context, parentMemberID uuid.UUID) ([]household.ParentOfRecord, error) {
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

// memConsentStore mirrors platform/consent's unexported memStore for use here.
type memConsentStore struct {
	mu      sync.Mutex
	entries map[uuid.UUID]consent.Entry
}

func newMemConsentStore() *memConsentStore {
	return &memConsentStore{entries: map[uuid.UUID]consent.Entry{}}
}

var _ consent.Store = (*memConsentStore)(nil)

func (s *memConsentStore) InsertEntry(_ context.Context, e consent.Entry) (consent.Entry, error) {
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

func (s *memConsentStore) GetEntry(_ context.Context, id uuid.UUID) (consent.Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok {
		return consent.Entry{}, consent.ErrNotFound
	}
	return e, nil
}

func (s *memConsentStore) LatestForPurpose(_ context.Context, principal uuid.UUID, purpose consent.Purpose) (consent.Entry, error) {
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

func (s *memConsentStore) ListForPrincipal(_ context.Context, principal uuid.UUID, limit, offset int32) ([]consent.Entry, error) {
	return s.list(func(e consent.Entry) bool { return e.PrincipalMemberID == principal }, limit, offset), nil
}

func (s *memConsentStore) ListByPurpose(_ context.Context, purpose consent.Purpose, limit, offset int32) ([]consent.Entry, error) {
	return s.list(func(e consent.Entry) bool { return e.Purpose == purpose }, limit, offset), nil
}

func (s *memConsentStore) ListForHousehold(_ context.Context, hh uuid.UUID, limit, offset int32) ([]consent.Entry, error) {
	return s.list(func(e consent.Entry) bool { return e.HouseholdID == hh }, limit, offset), nil
}

func (s *memConsentStore) list(pred func(consent.Entry) bool, limit, offset int32) []consent.Entry {
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

// recordingAuditStore captures every audit event into a slice for assertions.
// It satisfies platform/audit.AuditStore.
type recordingAuditStore struct {
	mu     sync.Mutex
	events []audit.Event
}

func newRecordingAuditStore() *recordingAuditStore {
	return &recordingAuditStore{}
}

func (s *recordingAuditStore) CreateAuditLog(_ context.Context, companyID, actorID uuid.UUID, action, resource, resourceID, ipAddress string, details []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, audit.Event{
		CompanyID:  companyID.String(),
		ActorID:    actorID.String(),
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		IPAddress:  ipAddress,
		// Details is the raw JSON; we don't re-decode it for tests.
	})
	return nil
}

func (s *recordingAuditStore) Snapshot() []audit.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]audit.Event, len(s.events))
	copy(out, s.events)
	return out
}

func (s *recordingAuditStore) ActionCount(action string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, e := range s.events {
		if e.Action == action {
			n++
		}
	}
	return n
}
