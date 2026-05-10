package livekit

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

const (
	callRoomPrefix    = "call-"
	meetingRoomPrefix = "meeting-"
)

// IsCallRoom reports whether roomName looks like a 1:1 call room.
func IsCallRoom(roomName string) bool { return strings.HasPrefix(roomName, callRoomPrefix) }

// IsMeetingRoom reports whether roomName looks like a multi-party meeting room.
func IsMeetingRoom(roomName string) bool { return strings.HasPrefix(roomName, meetingRoomPrefix) }

// CallIDFromRoomName extracts the call UUID from a room name of the form
// "call-{uuid}".
func CallIDFromRoomName(roomName string) (uuid.UUID, error) {
	if !IsCallRoom(roomName) {
		return uuid.Nil, fmt.Errorf("room %q does not have %q prefix", roomName, callRoomPrefix)
	}
	return uuid.Parse(strings.TrimPrefix(roomName, callRoomPrefix))
}

// MeetingIDFromRoomName extracts the meeting UUID from a room name of the
// form "meeting-{uuid}".
func MeetingIDFromRoomName(roomName string) (uuid.UUID, error) {
	if !IsMeetingRoom(roomName) {
		return uuid.Nil, fmt.Errorf("room %q does not have %q prefix", roomName, meetingRoomPrefix)
	}
	return uuid.Parse(strings.TrimPrefix(roomName, meetingRoomPrefix))
}
