package livekit

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeRooms records every CreateCallRoom / CreateMeetingRoom / DeleteRoom
// call and lets tests inject errors.
type fakeRooms struct {
	mu              sync.Mutex
	createdCalls    []string
	createdMeetings []struct {
		Name string
		Max  int
	}
	deleted        []string
	createCallErr  error
	createMeetErr  error
}

func (r *fakeRooms) CreateCallRoom(_ context.Context, callID string) (RoomInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createCallErr != nil {
		return RoomInfo{}, r.createCallErr
	}
	name := "call-" + callID
	r.createdCalls = append(r.createdCalls, name)
	return RoomInfo{Name: name, MaxParticipants: 2, EmptyTimeoutSecs: 30}, nil
}

func (r *fakeRooms) CreateMeetingRoom(_ context.Context, name string, max int) (RoomInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.createMeetErr != nil {
		return RoomInfo{}, r.createMeetErr
	}
	full := "meeting-" + name
	r.createdMeetings = append(r.createdMeetings, struct {
		Name string
		Max  int
	}{full, max})
	return RoomInfo{Name: full, MaxParticipants: uint32(max), EmptyTimeoutSecs: 300}, nil
}

func (r *fakeRooms) DeleteRoom(_ context.Context, roomName string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleted = append(r.deleted, roomName)
	return nil
}

// fakeTokens issues deterministic, easily-asserted strings.
type fakeTokens struct {
	issued []string
	err    error
}

func (t *fakeTokens) IssueJoinToken(roomName, identity, displayName string, ttl time.Duration) (string, error) {
	if t.err != nil {
		return "", t.err
	}
	tok := "tok:" + roomName + ":" + identity
	t.issued = append(t.issued, tok)
	return tok, nil
}

func newTestService(t *testing.T, clock Clock) (*Service, *InMemoryStore, *fakeRooms, *fakeTokens, *ChannelSignaler) {
	t.Helper()
	store := NewInMemoryStore()
	rooms := &fakeRooms{}
	tokens := &fakeTokens{}
	sig := NewChannelSignaler()
	if clock == nil {
		clock = NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	}
	svc, err := NewService(ServiceConfig{
		Store:      store,
		Rooms:      rooms,
		Tokens:     tokens,
		Signaler:   sig,
		Clock:      clock,
		LiveKitURL: "wss://lk.test",
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc, store, rooms, tokens, sig
}

func TestNewService_RequiresStore(t *testing.T) {
	_, err := NewService(ServiceConfig{
		Rooms:      &fakeRooms{},
		Tokens:     &fakeTokens{},
		LiveKitURL: "wss://lk",
	})
	if !errors.Is(err, ErrConfig) {
		t.Errorf("want ErrConfig, got %v", err)
	}
}

func TestNewService_RequiresRooms(t *testing.T) {
	_, err := NewService(ServiceConfig{
		Store:      NewInMemoryStore(),
		Tokens:     &fakeTokens{},
		LiveKitURL: "wss://lk",
	})
	if !errors.Is(err, ErrConfig) {
		t.Errorf("want ErrConfig, got %v", err)
	}
}

func TestNewService_RequiresLiveKitURL(t *testing.T) {
	_, err := NewService(ServiceConfig{
		Store:  NewInMemoryStore(),
		Rooms:  &fakeRooms{},
		Tokens: &fakeTokens{},
	})
	if !errors.Is(err, ErrConfig) {
		t.Errorf("want ErrConfig, got %v", err)
	}
}

func TestInitiateCall_HappyPath(t *testing.T) {
	svc, _, _, _, sig := newTestService(t, nil)
	ctx := context.Background()
	caller, callee := uuid.New(), uuid.New()

	call, err := svc.InitiateCall(ctx, caller, callee, CallTypeVideo)
	if err != nil {
		t.Fatalf("InitiateCall: %v", err)
	}
	if call.State != StateRinging {
		t.Errorf("state = %s, want ringing", call.State)
	}
	if call.CallerID != caller || call.CalleeID != callee {
		t.Errorf("caller/callee mismatch")
	}
	if call.RoomName != "" {
		t.Error("room should NOT be created on initiate")
	}

	events := sig.EventsFor(callee)
	if len(events) != 1 || events[0].EventType != EventCallInvite {
		t.Errorf("expected invite to callee, got %+v", events)
	}
}

func TestInitiateCall_RejectsSelfCall(t *testing.T) {
	svc, _, _, _, _ := newTestService(t, nil)
	u := uuid.New()
	if _, err := svc.InitiateCall(context.Background(), u, u, CallTypeVoice); err == nil {
		t.Error("expected error for self-call")
	}
}

func TestInitiateCall_RejectsBusyCaller(t *testing.T) {
	svc, _, _, _, _ := newTestService(t, nil)
	caller, callee := uuid.New(), uuid.New()
	if _, err := svc.InitiateCall(context.Background(), caller, callee, CallTypeVoice); err != nil {
		t.Fatal(err)
	}
	_, err := svc.InitiateCall(context.Background(), caller, uuid.New(), CallTypeVoice)
	if !errors.Is(err, ErrUserBusy) {
		t.Errorf("want ErrUserBusy, got %v", err)
	}
}

func TestInitiateCall_RejectsInvalidCallType(t *testing.T) {
	svc, _, _, _, _ := newTestService(t, nil)
	_, err := svc.InitiateCall(context.Background(), uuid.New(), uuid.New(), CallType("walkie-talkie"))
	if !errors.Is(err, ErrInvalidCallType) {
		t.Errorf("want ErrInvalidCallType, got %v", err)
	}
}

func TestAcceptCall_HappyPath(t *testing.T) {
	svc, store, rooms, _, sig := newTestService(t, nil)
	ctx := context.Background()
	caller, callee := uuid.New(), uuid.New()
	call, _ := svc.InitiateCall(ctx, caller, callee, CallTypeVideo)

	res, err := svc.AcceptCall(ctx, call.ID, callee)
	if err != nil {
		t.Fatalf("AcceptCall: %v", err)
	}
	if res.Call.State != StateConnecting {
		t.Errorf("state = %s, want connecting", res.Call.State)
	}
	if res.LiveKitURL != "wss://lk.test" {
		t.Errorf("LiveKitURL = %q", res.LiveKitURL)
	}
	if res.CalleeToken == "" {
		t.Error("CalleeToken empty")
	}
	if len(rooms.createdCalls) != 1 {
		t.Errorf("expected 1 created call room, got %d", len(rooms.createdCalls))
	}

	// Caller should have received call.accepted with their token.
	callerEvents := sig.EventsFor(caller)
	var sawAccepted bool
	for _, e := range callerEvents {
		if e.EventType == EventCallAccepted {
			sawAccepted = true
		}
	}
	if !sawAccepted {
		t.Errorf("caller did not receive call.accepted, got: %+v", callerEvents)
	}

	stored, _ := store.GetCall(ctx, call.ID)
	if stored.RoomName == "" {
		t.Error("RoomName not persisted")
	}
}

func TestAcceptCall_RejectsCaller(t *testing.T) {
	svc, _, _, _, _ := newTestService(t, nil)
	ctx := context.Background()
	caller, callee := uuid.New(), uuid.New()
	call, _ := svc.InitiateCall(ctx, caller, callee, CallTypeVideo)
	_, err := svc.AcceptCall(ctx, call.ID, caller)
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("want ErrUnauthorized, got %v", err)
	}
}

func TestDeclineCall(t *testing.T) {
	svc, store, _, _, sig := newTestService(t, nil)
	ctx := context.Background()
	caller, callee := uuid.New(), uuid.New()
	call, _ := svc.InitiateCall(ctx, caller, callee, CallTypeVideo)

	if err := svc.DeclineCall(ctx, call.ID, callee); err != nil {
		t.Fatalf("DeclineCall: %v", err)
	}
	stored, _ := store.GetCall(ctx, call.ID)
	if stored.State != StateDeclined {
		t.Errorf("state = %s, want declined", stored.State)
	}
	if stored.EndReason != "declined" {
		t.Errorf("end_reason = %q", stored.EndReason)
	}
	declined := false
	for _, e := range sig.EventsFor(caller) {
		if e.EventType == EventCallDeclined {
			declined = true
		}
	}
	if !declined {
		t.Error("caller did not receive call.declined")
	}
}

func TestRingingTimeout_TransitionsToMissed(t *testing.T) {
	clock := NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	svc, store, _, _, sig := newTestService(t, clock)
	ctx := context.Background()
	caller, callee := uuid.New(), uuid.New()
	call, _ := svc.InitiateCall(ctx, caller, callee, CallTypeVideo)

	// Advance past the ringing timeout.
	clock.Advance(DefaultRingingTimeout + time.Second)

	stored, _ := store.GetCall(ctx, call.ID)
	if stored.State != StateMissed {
		t.Errorf("state = %s, want missed", stored.State)
	}
	if stored.EndReason != "timeout" {
		t.Errorf("end_reason = %q", stored.EndReason)
	}
	// Both parties should have been notified.
	if got := len(sig.EventsFor(caller)); got < 1 {
		t.Errorf("caller should have received call.missed")
	}
}

func TestRingingTimeout_NoOpAfterAccept(t *testing.T) {
	clock := NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	svc, store, _, _, _ := newTestService(t, clock)
	ctx := context.Background()
	caller, callee := uuid.New(), uuid.New()
	call, _ := svc.InitiateCall(ctx, caller, callee, CallTypeVideo)
	if _, err := svc.AcceptCall(ctx, call.ID, callee); err != nil {
		t.Fatal(err)
	}
	clock.Advance(DefaultRingingTimeout + time.Second)

	stored, _ := store.GetCall(ctx, call.ID)
	if stored.State != StateConnecting {
		t.Errorf("state = %s, want connecting (timeout shouldn't override accept)", stored.State)
	}
}

func TestEndCall_BothPartiesCanHangup(t *testing.T) {
	for _, hanger := range []string{"caller", "callee"} {
		t.Run(hanger, func(t *testing.T) {
			clock := NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
			svc, store, _, _, _ := newTestService(t, clock)
			ctx := context.Background()
			caller, callee := uuid.New(), uuid.New()
			call, _ := svc.InitiateCall(ctx, caller, callee, CallTypeVideo)
			_, _ = svc.AcceptCall(ctx, call.ID, callee)
			_ = svc.MarkConnected(ctx, call.ID)
			clock.Advance(2 * time.Minute)

			var who uuid.UUID
			var wantReason string
			if hanger == "caller" {
				who, wantReason = caller, "caller_hangup"
			} else {
				who, wantReason = callee, "callee_hangup"
			}
			if err := svc.EndCall(ctx, call.ID, who); err != nil {
				t.Fatal(err)
			}
			stored, _ := store.GetCall(ctx, call.ID)
			if stored.State != StateEnded {
				t.Errorf("state = %s", stored.State)
			}
			if stored.EndReason != wantReason {
				t.Errorf("end_reason = %q, want %q", stored.EndReason, wantReason)
			}
			if stored.Duration <= 0 {
				t.Errorf("duration = %v, want > 0", stored.Duration)
			}
		})
	}
}

func TestMarkConnected_Idempotent(t *testing.T) {
	svc, store, _, _, _ := newTestService(t, nil)
	ctx := context.Background()
	caller, callee := uuid.New(), uuid.New()
	call, _ := svc.InitiateCall(ctx, caller, callee, CallTypeVideo)
	_, _ = svc.AcceptCall(ctx, call.ID, callee)
	if err := svc.MarkConnected(ctx, call.ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.MarkConnected(ctx, call.ID); err != nil {
		t.Fatalf("second MarkConnected: %v", err)
	}
	stored, _ := store.GetCall(ctx, call.ID)
	if stored.State != StateConnected {
		t.Errorf("state = %s", stored.State)
	}
}

func TestEndCallByRoom(t *testing.T) {
	svc, store, _, _, _ := newTestService(t, nil)
	ctx := context.Background()
	caller, callee := uuid.New(), uuid.New()
	call, _ := svc.InitiateCall(ctx, caller, callee, CallTypeVideo)
	_, _ = svc.AcceptCall(ctx, call.ID, callee)

	roomName := "call-" + call.ID.String()
	if err := svc.EndCallByRoom(ctx, roomName, "participant_left"); err != nil {
		t.Fatal(err)
	}
	stored, _ := store.GetCall(ctx, call.ID)
	if stored.State != StateEnded {
		t.Errorf("state = %s", stored.State)
	}
	if stored.EndReason != "participant_left" {
		t.Errorf("end_reason = %q", stored.EndReason)
	}
}

func TestCreateMeeting_DefaultsAndCaps(t *testing.T) {
	svc, _, _, _, _ := newTestService(t, nil)
	creator := uuid.New()

	// Default participant count.
	m, err := svc.CreateMeeting(context.Background(), CreateMeetingInput{
		Title:     "Standup",
		CreatorID: creator,
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.MaxParticipants != DefaultMaxMeetingParticipants {
		t.Errorf("MaxParticipants = %d, want %d", m.MaxParticipants, DefaultMaxMeetingParticipants)
	}
	if !m.E2EEEnabled {
		t.Error("E2EE should default to true when recording disabled")
	}

	// Cap enforcement.
	m, _ = svc.CreateMeeting(context.Background(), CreateMeetingInput{
		Title:           "Big",
		CreatorID:       creator,
		MaxParticipants: 1000,
	})
	if m.MaxParticipants != MaxMeetingParticipantsCap {
		t.Errorf("MaxParticipants = %d, want capped %d", m.MaxParticipants, MaxMeetingParticipantsCap)
	}

	// Recording disables E2EE.
	m, _ = svc.CreateMeeting(context.Background(), CreateMeetingInput{
		Title:            "Recorded",
		CreatorID:        creator,
		RecordingEnabled: true,
	})
	if m.E2EEEnabled {
		t.Error("E2EE should be false when recording enabled")
	}
}

func TestJoinMeeting_ActivatesAndAdds(t *testing.T) {
	svc, store, rooms, _, _ := newTestService(t, nil)
	ctx := context.Background()
	creator := uuid.New()
	m, _ := svc.CreateMeeting(ctx, CreateMeetingInput{Title: "T", CreatorID: creator, MaxParticipants: 5})

	res, err := svc.JoinMeeting(ctx, m.ID, creator)
	if err != nil {
		t.Fatal(err)
	}
	if res.Meeting.State != MeetingStateActive {
		t.Errorf("state = %s", res.Meeting.State)
	}
	if res.Token == "" {
		t.Error("token empty")
	}
	if len(rooms.createdMeetings) != 1 {
		t.Errorf("expected 1 created meeting room")
	}
	count, _ := store.CountActiveParticipants(ctx, m.ID)
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}

	// Second join is idempotent in store but also fine.
	if _, err := svc.JoinMeeting(ctx, m.ID, uuid.New()); err != nil {
		t.Fatal(err)
	}
	count, _ = store.CountActiveParticipants(ctx, m.ID)
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestJoinMeeting_RejectsFull(t *testing.T) {
	svc, _, _, _, _ := newTestService(t, nil)
	ctx := context.Background()
	creator := uuid.New()
	m, _ := svc.CreateMeeting(ctx, CreateMeetingInput{Title: "T", CreatorID: creator, MaxParticipants: 1})
	if _, err := svc.JoinMeeting(ctx, m.ID, creator); err != nil {
		t.Fatal(err)
	}
	_, err := svc.JoinMeeting(ctx, m.ID, uuid.New())
	if !errors.Is(err, ErrMeetingFull) {
		t.Errorf("want ErrMeetingFull, got %v", err)
	}
}

func TestLeaveMeeting_LastLeaverEnds(t *testing.T) {
	svc, store, _, _, _ := newTestService(t, nil)
	ctx := context.Background()
	creator := uuid.New()
	m, _ := svc.CreateMeeting(ctx, CreateMeetingInput{Title: "T", CreatorID: creator, MaxParticipants: 5})
	user := uuid.New()
	_, _ = svc.JoinMeeting(ctx, m.ID, user)

	if err := svc.LeaveMeeting(ctx, m.ID, user); err != nil {
		t.Fatal(err)
	}
	stored, _ := store.GetMeeting(ctx, m.ID)
	if stored.State != MeetingStateEnded {
		t.Errorf("state = %s, want ended", stored.State)
	}
}

func TestEndMeeting_OnlyCreator(t *testing.T) {
	svc, _, _, _, _ := newTestService(t, nil)
	ctx := context.Background()
	creator := uuid.New()
	m, _ := svc.CreateMeeting(ctx, CreateMeetingInput{Title: "T", CreatorID: creator})
	if err := svc.EndMeeting(ctx, m.ID, uuid.New()); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("want ErrUnauthorized, got %v", err)
	}
	if err := svc.EndMeeting(ctx, m.ID, creator); err != nil {
		t.Fatal(err)
	}
}

func TestRoomNameRoundtrip(t *testing.T) {
	id := uuid.New()
	callRoom := callRoomPrefix + id.String()
	if !IsCallRoom(callRoom) {
		t.Error("IsCallRoom failed")
	}
	got, err := CallIDFromRoomName(callRoom)
	if err != nil || got != id {
		t.Errorf("CallIDFromRoomName: %v %v", got, err)
	}
	meetingRoom := meetingRoomPrefix + id.String()
	if !IsMeetingRoom(meetingRoom) {
		t.Error("IsMeetingRoom failed")
	}
	got, err = MeetingIDFromRoomName(meetingRoom)
	if err != nil || got != id {
		t.Errorf("MeetingIDFromRoomName: %v %v", got, err)
	}
	if _, err := CallIDFromRoomName("foo"); err == nil {
		t.Error("expected error on non-call room")
	}
}

func TestInMemoryStore_ListCallsForUser_OrdersDesc(t *testing.T) {
	store := NewInMemoryStore()
	ctx := context.Background()
	user := uuid.New()
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		_, _ = store.CreateCall(ctx, Call{
			CallerID:  user,
			CalleeID:  uuid.New(),
			CallType:  CallTypeVoice,
			State:     StateEnded,
			StartedAt: t1.Add(time.Duration(i) * time.Hour),
		})
	}
	calls, err := store.ListCallsForUser(ctx, user, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 {
		t.Fatalf("want 3, got %d", len(calls))
	}
	for i := 1; i < len(calls); i++ {
		if !calls[i-1].StartedAt.After(calls[i].StartedAt) {
			t.Errorf("not ordered desc")
		}
	}
}
