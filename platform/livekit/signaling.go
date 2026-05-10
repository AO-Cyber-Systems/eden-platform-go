package livekit

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

// Signaler delivers call lifecycle events to a specific user. Implementations
// typically wrap NATS, a WebSocket gateway, or a push-notification service.
//
// SendToUser must be non-blocking and best-effort — if the user is offline,
// the implementation should drop the event or queue it. Errors returned are
// logged by the platform but do not fail the surrounding operation.
type Signaler interface {
	SendToUser(ctx context.Context, userID uuid.UUID, eventType string, payload any) error
}

// NoopSignaler discards every event. Useful in tests when signaling isn't
// the unit under test.
type NoopSignaler struct{}

// SendToUser implements Signaler.
func (NoopSignaler) SendToUser(_ context.Context, _ uuid.UUID, _ string, _ any) error {
	return nil
}

// CapturedEvent is a Signaler event captured by ChannelSignaler.
type CapturedEvent struct {
	UserID    uuid.UUID
	EventType string
	Payload   any
}

// ChannelSignaler captures every event into an in-memory slice. Useful for
// table-driven tests that assert on the events produced by a Service call.
//
// All methods are goroutine-safe.
type ChannelSignaler struct {
	mu     sync.Mutex
	events []CapturedEvent
}

// NewChannelSignaler returns an empty ChannelSignaler.
func NewChannelSignaler() *ChannelSignaler {
	return &ChannelSignaler{}
}

// SendToUser implements Signaler.
func (s *ChannelSignaler) SendToUser(_ context.Context, userID uuid.UUID, eventType string, payload any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, CapturedEvent{UserID: userID, EventType: eventType, Payload: payload})
	return nil
}

// Events returns a snapshot of captured events.
func (s *ChannelSignaler) Events() []CapturedEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]CapturedEvent, len(s.events))
	copy(out, s.events)
	return out
}

// Reset clears captured events.
func (s *ChannelSignaler) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = nil
}

// EventsFor returns captured events whose UserID matches.
func (s *ChannelSignaler) EventsFor(userID uuid.UUID) []CapturedEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]CapturedEvent, 0, len(s.events))
	for _, e := range s.events {
		if e.UserID == userID {
			out = append(out, e)
		}
	}
	return out
}
