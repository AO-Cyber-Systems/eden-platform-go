package livekit

import (
	"time"

	"github.com/google/uuid"
)

// CallType is "voice" or "video".
type CallType string

// Supported CallType values.
const (
	CallTypeVoice CallType = "voice"
	CallTypeVideo CallType = "video"
)

// Validate reports whether the value is a recognised CallType.
func (t CallType) Validate() error {
	if t != CallTypeVoice && t != CallTypeVideo {
		return ErrInvalidCallType
	}
	return nil
}

// Call is a 1:1 call between two subjects.
type Call struct {
	ID          uuid.UUID
	RoomName    string
	CallerID    uuid.UUID
	CalleeID    uuid.UUID
	CallType    CallType
	State       CallState
	StartedAt   time.Time
	AnsweredAt  *time.Time
	ConnectedAt *time.Time
	EndedAt     *time.Time
	EndReason   string
	Duration    time.Duration
}

// Meeting is a multi-party room.
type Meeting struct {
	ID               uuid.UUID
	RoomName         string
	Title            string
	CreatorID        uuid.UUID
	MaxParticipants  int
	RecordingEnabled bool
	E2EEEnabled      bool
	State            MeetingState
	StartedAt        *time.Time
	EndedAt          *time.Time
	CreatedAt        time.Time
}

// Participant is a (subject, meeting) join record.
type Participant struct {
	MeetingID uuid.UUID
	UserID    uuid.UUID
	JoinedAt  time.Time
	LeftAt    *time.Time
}

// Recording captures egress lifecycle metadata.
type Recording struct {
	ID        uuid.UUID
	MeetingID uuid.UUID
	EgressID  string
	State     RecordingState
	FilePath  string
	FileSize  int64
	Duration  time.Duration
	StartedAt time.Time
	EndedAt   *time.Time
}

// CallAcceptedResult is what AcceptCall returns: the LiveKit URL and the
// callee's join token. The caller's notification token is sent via the
// Signaler.
type CallAcceptedResult struct {
	Call         Call
	LiveKitURL   string
	CalleeToken  string
}

// MeetingJoinResult is what JoinMeeting returns: connection info plus the
// meeting metadata.
type MeetingJoinResult struct {
	Meeting     Meeting
	LiveKitURL  string
	Token       string
	E2EEEnabled bool
}

// RoomInfo is the minimal subset of LiveKit room metadata the platform
// surfaces to consumers.
type RoomInfo struct {
	Name             string
	MaxParticipants  uint32
	NumParticipants  uint32
	EmptyTimeoutSecs uint32
}

// Event types published via the Signaler.
const (
	EventCallInvite   = "call.invite"
	EventCallAccepted = "call.accepted"
	EventCallDeclined = "call.declined"
	EventCallEnded    = "call.ended"
	EventCallMissed   = "call.missed"
)
