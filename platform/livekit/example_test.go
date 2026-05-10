package livekit_test

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/aocybersystems/eden-platform-go/platform/livekit"
)

// roomsStub returns deterministic RoomInfo for runnable example output.
type roomsStub struct{}

func (roomsStub) CreateCallRoom(_ context.Context, callID string) (livekit.RoomInfo, error) {
	return livekit.RoomInfo{Name: "call-" + callID, MaxParticipants: 2}, nil
}

func (roomsStub) CreateMeetingRoom(_ context.Context, name string, max int) (livekit.RoomInfo, error) {
	return livekit.RoomInfo{Name: "meeting-" + name, MaxParticipants: uint32(max)}, nil
}

func (roomsStub) DeleteRoom(_ context.Context, _ string) error { return nil }

// stubTokens implements livekit.Tokens with deterministic output suitable
// for runnable examples.
type stubTokens struct{}

func (stubTokens) IssueJoinToken(roomName, identity, _ string, _ time.Duration) (string, error) {
	return "tok:" + roomName + ":" + identity, nil
}

// ExampleService_InitiateCall demonstrates initiating a 1:1 call.
func ExampleService_InitiateCall() {
	ctx := context.Background()
	svc, _ := livekit.NewService(livekit.ServiceConfig{
		Store:      livekit.NewInMemoryStore(),
		Rooms:      roomsStub{},
		Tokens:     stubTokens{},
		LiveKitURL: "wss://lk.example.com",
	})

	caller := uuid.MustParse("00000000-0000-0000-0000-00000000aaaa")
	callee := uuid.MustParse("00000000-0000-0000-0000-00000000bbbb")
	call, err := svc.InitiateCall(ctx, caller, callee, livekit.CallTypeVideo)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("state:", call.State)
	fmt.Println("call_type:", call.CallType)
	// Output:
	// state: ringing
	// call_type: video
}

// ExampleService_CreateMeeting shows creating a multi-party meeting and
// having the creator join as the first participant.
func ExampleService_CreateMeeting() {
	ctx := context.Background()
	svc, _ := livekit.NewService(livekit.ServiceConfig{
		Store:      livekit.NewInMemoryStore(),
		Rooms:      roomsStub{},
		Tokens:     stubTokens{},
		LiveKitURL: "wss://lk.example.com",
	})

	creator := uuid.MustParse("00000000-0000-0000-0000-00000000cccc")
	meeting, err := svc.CreateMeeting(ctx, livekit.CreateMeetingInput{
		Title:           "Family Huddle",
		CreatorID:       creator,
		MaxParticipants: 8,
	})
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("title:", meeting.Title)
	fmt.Println("max:", meeting.MaxParticipants)
	fmt.Println("e2ee:", meeting.E2EEEnabled)

	join, err := svc.JoinMeeting(ctx, meeting.ID, creator)
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("state_after_join:", join.Meeting.State)
	// Output:
	// title: Family Huddle
	// max: 8
	// e2ee: true
	// state_after_join: active
}
