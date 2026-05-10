package livekit

import (
	"context"
	"time"
)

// Rooms creates and deletes LiveKit rooms. The shipped LiveKitAdapter
// implements this interface; tests typically substitute a fake.
type Rooms interface {
	// CreateCallRoom creates a 1:1 call room ("call-{callID}", 2 participants,
	// 30s empty timeout).
	CreateCallRoom(ctx context.Context, callID string) (RoomInfo, error)
	// CreateMeetingRoom creates a multi-party room ("meeting-{name}",
	// configurable max, 5min empty timeout).
	CreateMeetingRoom(ctx context.Context, name string, maxParticipants int) (RoomInfo, error)
	// DeleteRoom force-closes a room. Used for cleanup on failed setup.
	DeleteRoom(ctx context.Context, roomName string) error
}

// Tokens issues LiveKit JWTs for room joins.
type Tokens interface {
	// IssueJoinToken returns a signed JWT granting the identity permission
	// to join roomName, valid for ttl. Empty ttl uses the implementation's
	// default (1h is recommended).
	IssueJoinToken(roomName, identity, displayName string, ttl time.Duration) (string, error)
}
