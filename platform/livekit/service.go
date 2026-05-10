package livekit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// DefaultRingingTimeout is the default time before a ringing call transitions
// to missed if the callee hasn't answered.
const DefaultRingingTimeout = 30 * time.Second

// DefaultMaxMeetingParticipants caps the participant count for a Meeting when
// none is specified by the caller.
const DefaultMaxMeetingParticipants = 10

// MaxMeetingParticipantsCap is a hard ceiling so a misconfigured caller can't
// request a 10000-person room.
const MaxMeetingParticipantsCap = 49

// ServiceConfig holds the wired-in dependencies for a Service. All fields are
// required; NewService validates.
type ServiceConfig struct {
	Store           Store
	Rooms           Rooms
	Tokens          Tokens
	Signaler        Signaler
	Clock           Clock
	LiveKitURL      string
	RingingTimeout  time.Duration // 0 → DefaultRingingTimeout
	JoinTokenTTL    time.Duration // 0 → 1 hour
	Logger          *slog.Logger  // nil → slog.Default()
}

// Service is the call/meeting orchestration entrypoint. It is goroutine-safe;
// concurrency control is delegated to Store.
type Service struct {
	store          Store
	rooms          Rooms
	tokens         Tokens
	signal         Signaler
	clock          Clock
	livekitURL     string
	ringingTimeout time.Duration
	tokenTTL       time.Duration
	log            *slog.Logger
}

// NewService validates cfg and returns a Service.
func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("%w: Store is required", ErrConfig)
	}
	if cfg.Rooms == nil {
		return nil, fmt.Errorf("%w: Rooms is required", ErrConfig)
	}
	if cfg.Tokens == nil {
		return nil, fmt.Errorf("%w: Tokens is required", ErrConfig)
	}
	if cfg.Signaler == nil {
		cfg.Signaler = NoopSignaler{}
	}
	if cfg.Clock == nil {
		cfg.Clock = NewRealClock()
	}
	if cfg.LiveKitURL == "" {
		return nil, fmt.Errorf("%w: LiveKitURL is required", ErrConfig)
	}
	if cfg.RingingTimeout == 0 {
		cfg.RingingTimeout = DefaultRingingTimeout
	}
	if cfg.JoinTokenTTL == 0 {
		cfg.JoinTokenTTL = time.Hour
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Service{
		store:          cfg.Store,
		rooms:          cfg.Rooms,
		tokens:         cfg.Tokens,
		signal:         cfg.Signaler,
		clock:          cfg.Clock,
		livekitURL:     cfg.LiveKitURL,
		ringingTimeout: cfg.RingingTimeout,
		tokenTTL:       cfg.JoinTokenTTL,
		log:            cfg.Logger,
	}, nil
}

// LiveKitURL returns the LiveKit server URL clients connect to.
func (s *Service) LiveKitURL() string { return s.livekitURL }

// --- 1:1 Calls ------------------------------------------------------------

// InitiateCall creates a call in ringing state and notifies the callee. A
// background timer (driven by the configured Clock) transitions the call to
// missed if the callee doesn't answer within RingingTimeout.
//
// IMPORTANT: the LiveKit room is NOT created here — only after AcceptCall.
// This avoids leaking rooms when calls are declined.
func (s *Service) InitiateCall(ctx context.Context, callerID, calleeID uuid.UUID, callType CallType) (Call, error) {
	if err := callType.Validate(); err != nil {
		return Call{}, err
	}
	if callerID == calleeID {
		return Call{}, fmt.Errorf("%w: caller and callee must differ", ErrInvalidState)
	}

	if err := s.assertNoActiveCall(ctx, callerID); err != nil {
		return Call{}, err
	}
	if err := s.assertNoActiveCall(ctx, calleeID); err != nil {
		return Call{}, err
	}

	now := s.clock.Now()
	call := Call{
		ID:        uuid.New(),
		CallerID:  callerID,
		CalleeID:  calleeID,
		CallType:  callType,
		State:     StateRinging,
		StartedAt: now,
	}
	stored, err := s.store.CreateCall(ctx, call)
	if err != nil {
		return Call{}, fmt.Errorf("create call: %w", err)
	}

	_ = s.signal.SendToUser(ctx, calleeID, EventCallInvite, map[string]string{
		"call_id":   stored.ID.String(),
		"caller_id": callerID.String(),
		"call_type": string(callType),
	})

	// Schedule the ringing-timeout transition.
	callID := stored.ID
	s.clock.AfterFunc(s.ringingTimeout, func() {
		s.expireRinging(callID)
	})

	return stored, nil
}

func (s *Service) assertNoActiveCall(ctx context.Context, userID uuid.UUID) error {
	_, err := s.store.GetActiveCallForUser(ctx, userID)
	if err == nil {
		return ErrUserBusy
	}
	if !errors.Is(err, ErrCallNotFound) {
		return fmt.Errorf("check active call: %w", err)
	}
	return nil
}

// expireRinging is the timer callback that marks a still-ringing call missed.
func (s *Service) expireRinging(callID uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	call, err := s.store.GetCall(ctx, callID)
	if err != nil {
		s.log.Error("expireRinging: get call", "call_id", callID, "error", err)
		return
	}
	if call.State != StateRinging {
		return
	}
	now := s.clock.Now()
	call.State = StateMissed
	call.EndedAt = &now
	call.EndReason = "timeout"
	if _, err := s.store.UpdateCall(ctx, call); err != nil {
		s.log.Error("expireRinging: update", "call_id", callID, "error", err)
		return
	}
	payload := map[string]string{"call_id": callID.String()}
	_ = s.signal.SendToUser(ctx, call.CallerID, EventCallMissed, payload)
	_ = s.signal.SendToUser(ctx, call.CalleeID, EventCallMissed, payload)
}

// AcceptCall transitions a ringing call to connecting, creates the LiveKit
// room, and returns the callee's join token. The caller is notified via the
// Signaler with their own token.
func (s *Service) AcceptCall(ctx context.Context, callID, calleeID uuid.UUID) (CallAcceptedResult, error) {
	call, err := s.store.GetCall(ctx, callID)
	if err != nil {
		return CallAcceptedResult{}, fmt.Errorf("get call: %w", err)
	}
	if call.State != StateRinging {
		return CallAcceptedResult{}, fmt.Errorf("%w: call is %s, not ringing", ErrInvalidState, call.State)
	}
	if call.CalleeID != calleeID {
		return CallAcceptedResult{}, fmt.Errorf("%w: only the callee may accept", ErrUnauthorized)
	}

	room, err := s.rooms.CreateCallRoom(ctx, callID.String())
	if err != nil {
		now := s.clock.Now()
		call.State = StateFailed
		call.EndedAt = &now
		call.EndReason = "room_creation_failed"
		_, _ = s.store.UpdateCall(ctx, call)
		return CallAcceptedResult{}, fmt.Errorf("create room: %w", err)
	}

	callerToken, err := s.tokens.IssueJoinToken(room.Name, call.CallerID.String(), call.CallerID.String(), s.tokenTTL)
	if err != nil {
		_ = s.rooms.DeleteRoom(ctx, room.Name)
		return CallAcceptedResult{}, fmt.Errorf("issue caller token: %w", err)
	}
	calleeToken, err := s.tokens.IssueJoinToken(room.Name, calleeID.String(), calleeID.String(), s.tokenTTL)
	if err != nil {
		_ = s.rooms.DeleteRoom(ctx, room.Name)
		return CallAcceptedResult{}, fmt.Errorf("issue callee token: %w", err)
	}

	now := s.clock.Now()
	call.State = StateConnecting
	call.RoomName = room.Name
	call.AnsweredAt = &now
	updated, err := s.store.UpdateCall(ctx, call)
	if err != nil {
		_ = s.rooms.DeleteRoom(ctx, room.Name)
		return CallAcceptedResult{}, fmt.Errorf("update call: %w", err)
	}

	_ = s.signal.SendToUser(ctx, call.CallerID, EventCallAccepted, map[string]string{
		"call_id":     updated.ID.String(),
		"livekit_url": s.livekitURL,
		"token":       callerToken,
	})

	return CallAcceptedResult{
		Call:        updated,
		LiveKitURL:  s.livekitURL,
		CalleeToken: calleeToken,
	}, nil
}

// DeclineCall marks a ringing call as declined and notifies the caller.
func (s *Service) DeclineCall(ctx context.Context, callID, calleeID uuid.UUID) error {
	call, err := s.store.GetCall(ctx, callID)
	if err != nil {
		return fmt.Errorf("get call: %w", err)
	}
	if call.State != StateRinging {
		return fmt.Errorf("%w: call is %s, not ringing", ErrInvalidState, call.State)
	}
	if call.CalleeID != calleeID {
		return fmt.Errorf("%w: only the callee may decline", ErrUnauthorized)
	}
	now := s.clock.Now()
	call.State = StateDeclined
	call.EndedAt = &now
	call.EndReason = "declined"
	if _, err := s.store.UpdateCall(ctx, call); err != nil {
		return fmt.Errorf("update call: %w", err)
	}
	_ = s.signal.SendToUser(ctx, call.CallerID, EventCallDeclined, map[string]string{
		"call_id": callID.String(),
	})
	return nil
}

// EndCall ends an active call (connecting or connected) and notifies the
// other participant.
func (s *Service) EndCall(ctx context.Context, callID, userID uuid.UUID) error {
	call, err := s.store.GetCall(ctx, callID)
	if err != nil {
		return fmt.Errorf("get call: %w", err)
	}
	if call.State != StateConnecting && call.State != StateConnected {
		return fmt.Errorf("%w: cannot end call in state %s", ErrInvalidState, call.State)
	}

	var endReason string
	var otherParty uuid.UUID
	switch userID {
	case call.CallerID:
		endReason = "caller_hangup"
		otherParty = call.CalleeID
	case call.CalleeID:
		endReason = "callee_hangup"
		otherParty = call.CallerID
	default:
		return ErrNotParticipant
	}

	now := s.clock.Now()
	if call.ConnectedAt != nil {
		call.Duration = now.Sub(*call.ConnectedAt)
	}
	call.State = StateEnded
	call.EndedAt = &now
	call.EndReason = endReason
	if _, err := s.store.UpdateCall(ctx, call); err != nil {
		return fmt.Errorf("update call: %w", err)
	}

	_ = s.signal.SendToUser(ctx, otherParty, EventCallEnded, map[string]string{
		"call_id":    callID.String(),
		"end_reason": endReason,
	})
	return nil
}

// MarkConnected transitions a connecting call to connected. Called by the
// webhook handler when both participants have joined the LiveKit room.
func (s *Service) MarkConnected(ctx context.Context, callID uuid.UUID) error {
	call, err := s.store.GetCall(ctx, callID)
	if err != nil {
		return fmt.Errorf("get call: %w", err)
	}
	if call.State != StateConnecting {
		return nil // idempotent — webhooks may fire repeatedly
	}
	now := s.clock.Now()
	call.State = StateConnected
	call.ConnectedAt = &now
	if _, err := s.store.UpdateCall(ctx, call); err != nil {
		return fmt.Errorf("update call: %w", err)
	}
	return nil
}

// EndCallByRoom is the webhook entrypoint for ending a call due to
// participant_left or room_finished. It infers the call from roomName.
func (s *Service) EndCallByRoom(ctx context.Context, roomName, reason string) error {
	callID, err := CallIDFromRoomName(roomName)
	if err != nil {
		return err
	}
	call, err := s.store.GetCall(ctx, callID)
	if err != nil {
		return fmt.Errorf("get call: %w", err)
	}
	if call.State != StateConnecting && call.State != StateConnected {
		return nil // already terminal
	}
	now := s.clock.Now()
	if call.ConnectedAt != nil {
		call.Duration = now.Sub(*call.ConnectedAt)
	}
	call.State = StateEnded
	call.EndedAt = &now
	call.EndReason = reason
	if _, err := s.store.UpdateCall(ctx, call); err != nil {
		return fmt.Errorf("update call: %w", err)
	}
	payload := map[string]string{"call_id": callID.String(), "end_reason": reason}
	_ = s.signal.SendToUser(ctx, call.CallerID, EventCallEnded, payload)
	_ = s.signal.SendToUser(ctx, call.CalleeID, EventCallEnded, payload)
	return nil
}

// ListCallsForUser returns paginated call history for a user.
func (s *Service) ListCallsForUser(ctx context.Context, userID uuid.UUID, limit, offset int) ([]Call, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return s.store.ListCallsForUser(ctx, userID, limit, offset)
}

// --- Multi-party Meetings -------------------------------------------------

// CreateMeetingInput captures the per-meeting tunables a caller specifies.
type CreateMeetingInput struct {
	Title            string
	CreatorID        uuid.UUID
	MaxParticipants  int
	RecordingEnabled bool
}

// CreateMeeting creates a meeting in scheduled state. The LiveKit room is
// only created when the first participant joins. E2EE and recording are
// mutually exclusive — recording disables E2EE.
func (s *Service) CreateMeeting(ctx context.Context, in CreateMeetingInput) (Meeting, error) {
	if in.Title == "" {
		return Meeting{}, fmt.Errorf("%w: title is required", ErrInvalidState)
	}
	maxP := in.MaxParticipants
	if maxP <= 0 {
		maxP = DefaultMaxMeetingParticipants
	}
	if maxP > MaxMeetingParticipantsCap {
		maxP = MaxMeetingParticipantsCap
	}
	now := s.clock.Now()
	id := uuid.New()
	m := Meeting{
		ID:               id,
		RoomName:         "meeting-" + id.String(),
		Title:            in.Title,
		CreatorID:        in.CreatorID,
		MaxParticipants:  maxP,
		RecordingEnabled: in.RecordingEnabled,
		E2EEEnabled:      !in.RecordingEnabled,
		State:            MeetingStateScheduled,
		CreatedAt:        now,
	}
	stored, err := s.store.CreateMeeting(ctx, m)
	if err != nil {
		return Meeting{}, fmt.Errorf("create meeting: %w", err)
	}
	return stored, nil
}

// JoinMeeting adds a participant and returns connection info. If the meeting
// is still scheduled, it activates by creating the LiveKit room.
func (s *Service) JoinMeeting(ctx context.Context, meetingID, userID uuid.UUID) (MeetingJoinResult, error) {
	meeting, err := s.store.GetMeeting(ctx, meetingID)
	if err != nil {
		return MeetingJoinResult{}, fmt.Errorf("get meeting: %w", err)
	}
	if meeting.State == MeetingStateEnded {
		return MeetingJoinResult{}, fmt.Errorf("%w: meeting has ended", ErrInvalidState)
	}
	count, err := s.store.CountActiveParticipants(ctx, meetingID)
	if err != nil {
		return MeetingJoinResult{}, fmt.Errorf("count participants: %w", err)
	}
	if count >= meeting.MaxParticipants {
		return MeetingJoinResult{}, ErrMeetingFull
	}

	if meeting.State == MeetingStateScheduled {
		if _, err := s.rooms.CreateMeetingRoom(ctx, meetingID.String(), meeting.MaxParticipants); err != nil {
			return MeetingJoinResult{}, fmt.Errorf("create meeting room: %w", err)
		}
		now := s.clock.Now()
		meeting.State = MeetingStateActive
		meeting.StartedAt = &now
		updated, err := s.store.UpdateMeeting(ctx, meeting)
		if err != nil {
			return MeetingJoinResult{}, fmt.Errorf("activate meeting: %w", err)
		}
		meeting = updated
	}

	if err := s.store.AddParticipant(ctx, Participant{
		MeetingID: meetingID,
		UserID:    userID,
		JoinedAt:  s.clock.Now(),
	}); err != nil {
		return MeetingJoinResult{}, fmt.Errorf("add participant: %w", err)
	}

	token, err := s.tokens.IssueJoinToken(meeting.RoomName, userID.String(), userID.String(), s.tokenTTL)
	if err != nil {
		return MeetingJoinResult{}, fmt.Errorf("issue token: %w", err)
	}

	return MeetingJoinResult{
		Meeting:     meeting,
		LiveKitURL:  s.livekitURL,
		Token:       token,
		E2EEEnabled: meeting.E2EEEnabled,
	}, nil
}

// LeaveMeeting marks a participant as left. If the meeting becomes empty, it
// transitions to ended.
func (s *Service) LeaveMeeting(ctx context.Context, meetingID, userID uuid.UUID) error {
	now := s.clock.Now()
	if err := s.store.RemoveParticipant(ctx, meetingID, userID, now); err != nil {
		return fmt.Errorf("remove participant: %w", err)
	}
	count, err := s.store.CountActiveParticipants(ctx, meetingID)
	if err != nil {
		return fmt.Errorf("count participants: %w", err)
	}
	if count > 0 {
		return nil
	}
	meeting, err := s.store.GetMeeting(ctx, meetingID)
	if err != nil {
		return fmt.Errorf("get meeting: %w", err)
	}
	if meeting.State == MeetingStateEnded {
		return nil
	}
	meeting.State = MeetingStateEnded
	meeting.EndedAt = &now
	if _, err := s.store.UpdateMeeting(ctx, meeting); err != nil {
		return fmt.Errorf("end meeting: %w", err)
	}
	return nil
}

// EndMeeting ends a meeting. Only the creator may invoke this.
func (s *Service) EndMeeting(ctx context.Context, meetingID, userID uuid.UUID) error {
	meeting, err := s.store.GetMeeting(ctx, meetingID)
	if err != nil {
		return fmt.Errorf("get meeting: %w", err)
	}
	if meeting.CreatorID != userID {
		return ErrUnauthorized
	}
	if meeting.State == MeetingStateEnded {
		return nil
	}
	now := s.clock.Now()
	meeting.State = MeetingStateEnded
	meeting.EndedAt = &now
	if _, err := s.store.UpdateMeeting(ctx, meeting); err != nil {
		return fmt.Errorf("end meeting: %w", err)
	}
	if err := s.rooms.DeleteRoom(ctx, meeting.RoomName); err != nil {
		s.log.Warn("EndMeeting: delete room", "room", meeting.RoomName, "error", err)
	}
	return nil
}

// EndMeetingByRoom is the webhook hook for room_finished events on meeting
// rooms. It is a safety net — normal lifecycle ends through LeaveMeeting/EndMeeting.
func (s *Service) EndMeetingByRoom(ctx context.Context, roomName string) error {
	meetingID, err := MeetingIDFromRoomName(roomName)
	if err != nil {
		return err
	}
	meeting, err := s.store.GetMeeting(ctx, meetingID)
	if err != nil {
		return fmt.Errorf("get meeting: %w", err)
	}
	if meeting.State == MeetingStateEnded {
		return nil
	}
	now := s.clock.Now()
	meeting.State = MeetingStateEnded
	meeting.EndedAt = &now
	if _, err := s.store.UpdateMeeting(ctx, meeting); err != nil {
		return fmt.Errorf("end meeting: %w", err)
	}
	return nil
}

// ListMeetingsForCreator returns meetings created by the user (paginated).
func (s *Service) ListMeetingsForCreator(ctx context.Context, userID uuid.UUID, limit, offset int) ([]Meeting, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return s.store.ListMeetingsForCreator(ctx, userID, limit, offset)
}

// Store exposes the wired-in Store. Callers may use it for read-only queries
// not exposed by Service (e.g. listing recordings inline with meeting metadata).
func (s *Service) Store() Store { return s.store }
