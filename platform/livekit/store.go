package livekit

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Store is the persistence interface for the calling stack. Implementations
// are responsible for serialising concurrent writes to a given record.
type Store interface {
	// Calls
	CreateCall(ctx context.Context, call Call) (Call, error)
	GetCall(ctx context.Context, id uuid.UUID) (Call, error)
	UpdateCall(ctx context.Context, call Call) (Call, error)
	GetActiveCallForUser(ctx context.Context, userID uuid.UUID) (Call, error)
	ListCallsForUser(ctx context.Context, userID uuid.UUID, limit, offset int) ([]Call, error)

	// Meetings
	CreateMeeting(ctx context.Context, m Meeting) (Meeting, error)
	GetMeeting(ctx context.Context, id uuid.UUID) (Meeting, error)
	UpdateMeeting(ctx context.Context, m Meeting) (Meeting, error)
	ListMeetingsForCreator(ctx context.Context, userID uuid.UUID, limit, offset int) ([]Meeting, error)

	// Participants
	AddParticipant(ctx context.Context, p Participant) error
	RemoveParticipant(ctx context.Context, meetingID, userID uuid.UUID, leftAt time.Time) error
	CountActiveParticipants(ctx context.Context, meetingID uuid.UUID) (int, error)
	ListParticipants(ctx context.Context, meetingID uuid.UUID) ([]Participant, error)

	// Recordings
	CreateRecording(ctx context.Context, r Recording) (Recording, error)
	GetRecording(ctx context.Context, id uuid.UUID) (Recording, error)
	GetRecordingByEgressID(ctx context.Context, egressID string) (Recording, error)
	UpdateRecording(ctx context.Context, r Recording) (Recording, error)
	ListRecordingsForMeeting(ctx context.Context, meetingID uuid.UUID) ([]Recording, error)
}

// InMemoryStore is a goroutine-safe Store implementation suitable for tests
// and dev environments. Production callers should provide a database-backed
// implementation.
type InMemoryStore struct {
	mu           sync.RWMutex
	calls        map[uuid.UUID]Call
	meetings     map[uuid.UUID]Meeting
	participants map[uuid.UUID][]Participant // keyed by meetingID
	recordings   map[uuid.UUID]Recording
}

// NewInMemoryStore returns an empty in-memory Store.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		calls:        make(map[uuid.UUID]Call),
		meetings:     make(map[uuid.UUID]Meeting),
		participants: make(map[uuid.UUID][]Participant),
		recordings:   make(map[uuid.UUID]Recording),
	}
}

// --- Calls -----------------------------------------------------------------

// CreateCall stores a new call.
func (s *InMemoryStore) CreateCall(_ context.Context, call Call) (Call, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if call.ID == uuid.Nil {
		call.ID = uuid.New()
	}
	s.calls[call.ID] = call
	return call, nil
}

// GetCall fetches a call by ID.
func (s *InMemoryStore) GetCall(_ context.Context, id uuid.UUID) (Call, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.calls[id]
	if !ok {
		return Call{}, ErrCallNotFound
	}
	return c, nil
}

// UpdateCall replaces a stored call.
func (s *InMemoryStore) UpdateCall(_ context.Context, call Call) (Call, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.calls[call.ID]; !ok {
		return Call{}, ErrCallNotFound
	}
	s.calls[call.ID] = call
	return call, nil
}

// GetActiveCallForUser returns the user's active (non-terminal) call.
func (s *InMemoryStore) GetActiveCallForUser(_ context.Context, userID uuid.UUID) (Call, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.calls {
		if (c.CallerID == userID || c.CalleeID == userID) && !c.State.IsTerminal() {
			return c, nil
		}
	}
	return Call{}, ErrCallNotFound
}

// ListCallsForUser returns the user's calls ordered by StartedAt descending.
func (s *InMemoryStore) ListCallsForUser(_ context.Context, userID uuid.UUID, limit, offset int) ([]Call, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Call, 0)
	for _, c := range s.calls {
		if c.CallerID == userID || c.CalleeID == userID {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	if offset > len(out) {
		return nil, nil
	}
	out = out[offset:]
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// --- Meetings --------------------------------------------------------------

// CreateMeeting stores a new meeting.
func (s *InMemoryStore) CreateMeeting(_ context.Context, m Meeting) (Meeting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	s.meetings[m.ID] = m
	return m, nil
}

// GetMeeting fetches a meeting by ID.
func (s *InMemoryStore) GetMeeting(_ context.Context, id uuid.UUID) (Meeting, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.meetings[id]
	if !ok {
		return Meeting{}, ErrMeetingNotFound
	}
	return m, nil
}

// UpdateMeeting replaces a stored meeting.
func (s *InMemoryStore) UpdateMeeting(_ context.Context, m Meeting) (Meeting, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.meetings[m.ID]; !ok {
		return Meeting{}, ErrMeetingNotFound
	}
	s.meetings[m.ID] = m
	return m, nil
}

// ListMeetingsForCreator returns the user's meetings (as creator) ordered by CreatedAt desc.
func (s *InMemoryStore) ListMeetingsForCreator(_ context.Context, userID uuid.UUID, limit, offset int) ([]Meeting, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Meeting, 0)
	for _, m := range s.meetings {
		if m.CreatorID == userID {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if offset > len(out) {
		return nil, nil
	}
	out = out[offset:]
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// --- Participants ---------------------------------------------------------

// AddParticipant records a join.
func (s *InMemoryStore) AddParticipant(_ context.Context, p Participant) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.meetings[p.MeetingID]; !ok {
		return ErrMeetingNotFound
	}
	// Idempotent: if already active, do nothing.
	for _, existing := range s.participants[p.MeetingID] {
		if existing.UserID == p.UserID && existing.LeftAt == nil {
			return nil
		}
	}
	s.participants[p.MeetingID] = append(s.participants[p.MeetingID], p)
	return nil
}

// RemoveParticipant marks the user as having left.
func (s *InMemoryStore) RemoveParticipant(_ context.Context, meetingID, userID uuid.UUID, leftAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	parts := s.participants[meetingID]
	for i := range parts {
		if parts[i].UserID == userID && parts[i].LeftAt == nil {
			t := leftAt
			parts[i].LeftAt = &t
			s.participants[meetingID] = parts
			return nil
		}
	}
	return ErrNotParticipant
}

// CountActiveParticipants counts joins without a LeftAt.
func (s *InMemoryStore) CountActiveParticipants(_ context.Context, meetingID uuid.UUID) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, p := range s.participants[meetingID] {
		if p.LeftAt == nil {
			count++
		}
	}
	return count, nil
}

// ListParticipants returns all participant join records for a meeting.
func (s *InMemoryStore) ListParticipants(_ context.Context, meetingID uuid.UUID) ([]Participant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	parts := s.participants[meetingID]
	out := make([]Participant, len(parts))
	copy(out, parts)
	return out, nil
}

// --- Recordings -----------------------------------------------------------

// CreateRecording stores a new recording.
func (s *InMemoryStore) CreateRecording(_ context.Context, r Recording) (Recording, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	s.recordings[r.ID] = r
	return r, nil
}

// GetRecording fetches a recording by ID.
func (s *InMemoryStore) GetRecording(_ context.Context, id uuid.UUID) (Recording, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.recordings[id]
	if !ok {
		return Recording{}, ErrRecordingNotFound
	}
	return r, nil
}

// GetRecordingByEgressID looks up a recording by its LiveKit egress ID.
func (s *InMemoryStore) GetRecordingByEgressID(_ context.Context, egressID string) (Recording, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.recordings {
		if r.EgressID == egressID {
			return r, nil
		}
	}
	return Recording{}, ErrRecordingNotFound
}

// UpdateRecording replaces a stored recording.
func (s *InMemoryStore) UpdateRecording(_ context.Context, r Recording) (Recording, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.recordings[r.ID]; !ok {
		return Recording{}, ErrRecordingNotFound
	}
	s.recordings[r.ID] = r
	return r, nil
}

// ListRecordingsForMeeting returns recordings for a meeting ordered by StartedAt desc.
func (s *InMemoryStore) ListRecordingsForMeeting(_ context.Context, meetingID uuid.UUID) ([]Recording, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Recording, 0)
	for _, r := range s.recordings {
		if r.MeetingID == meetingID {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out, nil
}

// Compile-time check.
var _ Store = (*InMemoryStore)(nil)

// errors used only inside this file for cross-method boundary clarity.
var _ = errors.New
