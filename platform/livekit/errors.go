package livekit

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// Domain errors. Consumers can errors.Is against these for stable handling.
var (
	ErrInvalidCallType   = errors.New("livekit: invalid call type")
	ErrCallNotFound      = errors.New("livekit: call not found")
	ErrMeetingNotFound   = errors.New("livekit: meeting not found")
	ErrRecordingNotFound = errors.New("livekit: recording not found")
	ErrInvalidState      = errors.New("livekit: invalid state for operation")
	ErrUserBusy          = errors.New("livekit: user has an active call")
	ErrNotParticipant    = errors.New("livekit: user is not a participant")
	ErrMeetingFull       = errors.New("livekit: meeting is at capacity")
	ErrUnauthorized      = errors.New("livekit: not authorized for this operation")
	ErrConfig            = errors.New("livekit: invalid configuration")
)

// errConfig wraps ErrConfig with a contextual message.
func errConfig(msg string) error { return fmt.Errorf("%w: %s", ErrConfig, msg) }

// parseUUID is a thin wrapper around uuid.Parse that returns a typed error.
func parseUUID(s string) (uuid.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse uuid %q: %w", s, err)
	}
	return id, nil
}
