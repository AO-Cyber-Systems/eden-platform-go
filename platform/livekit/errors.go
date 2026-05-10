package livekit

import "errors"

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
