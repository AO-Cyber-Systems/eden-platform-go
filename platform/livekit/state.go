package livekit

// CallState is the lifecycle state of a 1:1 call.
type CallState string

// Call lifecycle states.
const (
	StateRinging    CallState = "ringing"
	StateConnecting CallState = "connecting"
	StateConnected  CallState = "connected"
	StateDeclined   CallState = "declined"
	StateMissed     CallState = "missed"
	StateFailed     CallState = "failed"
	StateEnded      CallState = "ended"
)

// IsTerminal reports whether the call cannot transition further.
func (s CallState) IsTerminal() bool {
	switch s {
	case StateDeclined, StateMissed, StateFailed, StateEnded:
		return true
	default:
		return false
	}
}

// callTransitions enumerates valid Call state transitions.
var callTransitions = map[CallState]map[CallState]struct{}{
	StateRinging: {
		StateConnecting: {}, StateDeclined: {}, StateMissed: {}, StateFailed: {},
	},
	StateConnecting: {
		StateConnected: {}, StateFailed: {}, StateEnded: {},
	},
	StateConnected: {
		StateEnded: {},
	},
}

// ValidCallTransition reports whether transitioning from one Call state to
// another is allowed. Terminal states have no outgoing transitions.
func ValidCallTransition(from, to CallState) bool {
	allowed, ok := callTransitions[from]
	if !ok {
		return false
	}
	_, ok = allowed[to]
	return ok
}

// MeetingState is the lifecycle state of a multi-party meeting.
type MeetingState string

// Meeting lifecycle states.
const (
	MeetingStateScheduled MeetingState = "scheduled"
	MeetingStateActive    MeetingState = "active"
	MeetingStateEnded     MeetingState = "ended"
)

// IsTerminal reports whether the meeting has reached a terminal state.
func (s MeetingState) IsTerminal() bool {
	return s == MeetingStateEnded
}

// meetingTransitions enumerates valid Meeting state transitions.
var meetingTransitions = map[MeetingState]map[MeetingState]struct{}{
	MeetingStateScheduled: {MeetingStateActive: {}, MeetingStateEnded: {}},
	MeetingStateActive:    {MeetingStateEnded: {}},
}

// ValidMeetingTransition reports whether transitioning from one Meeting state
// to another is allowed.
func ValidMeetingTransition(from, to MeetingState) bool {
	allowed, ok := meetingTransitions[from]
	if !ok {
		return false
	}
	_, ok = allowed[to]
	return ok
}

// RecordingState is the lifecycle state of an egress recording.
type RecordingState string

// Recording lifecycle states.
const (
	RecordingStateRecording  RecordingState = "recording"
	RecordingStateProcessing RecordingState = "processing"
	RecordingStateCompleted  RecordingState = "completed"
	RecordingStateFailed     RecordingState = "failed"
)
