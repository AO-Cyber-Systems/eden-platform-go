package livekit

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/livekit/protocol/auth"
	livekitproto "github.com/livekit/protocol/livekit"
	"github.com/livekit/protocol/webhook"
)

// LiveKit webhook event names. The official set is broader; these are the
// ones the platform handles built-in.
const (
	WebhookEventParticipantJoined = "participant_joined"
	WebhookEventParticipantLeft   = "participant_left"
	WebhookEventRoomFinished      = "room_finished"
	WebhookEventEgressStarted     = "egress_started"
	WebhookEventEgressEnded       = "egress_ended"
)

// WebhookSubscriber is invoked for events of a specific type after the
// platform's built-in handlers run. Subscribers receive the raw LiveKit event
// so they can read fields the platform doesn't surface.
type WebhookSubscriber func(ctx context.Context, event *livekitproto.WebhookEvent)

// WebhookHandler is an http.Handler that verifies LiveKit signatures,
// dispatches built-in handlers (call/meeting state transitions, recording
// finalisation), and notifies consumer-registered subscribers.
type WebhookHandler struct {
	service     *Service
	recordings  *RecordingService
	keyProvider auth.KeyProvider
	timeout     time.Duration
	log         *slog.Logger

	mu          sync.RWMutex
	subscribers map[string][]WebhookSubscriber
}

// WebhookConfig configures a WebhookHandler.
type WebhookConfig struct {
	Service          *Service
	RecordingService *RecordingService
	KeyProvider      auth.KeyProvider // typically LiveKitAdapter.KeyProvider()
	Timeout          time.Duration    // 0 → 10s
	Logger           *slog.Logger     // nil → slog.Default()
}

// NewWebhookHandler validates cfg and returns an http.Handler.
//
// RecordingService is optional — if nil, egress events are still routed to
// subscribers but no built-in finalisation happens.
func NewWebhookHandler(cfg WebhookConfig) (*WebhookHandler, error) {
	if cfg.Service == nil {
		return nil, errConfig("Service is required")
	}
	if cfg.KeyProvider == nil {
		return nil, errConfig("KeyProvider is required")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &WebhookHandler{
		service:     cfg.Service,
		recordings:  cfg.RecordingService,
		keyProvider: cfg.KeyProvider,
		timeout:     timeout,
		log:         log,
		subscribers: make(map[string][]WebhookSubscriber),
	}, nil
}

// Subscribe registers a subscriber for the given event type. Subscribers run
// after the built-in handlers, sequentially in registration order.
func (h *WebhookHandler) Subscribe(eventType string, sub WebhookSubscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.subscribers[eventType] = append(h.subscribers[eventType], sub)
}

// ServeHTTP verifies the signature, dispatches the event, and replies.
//
// Failed signature verification returns 401. Internal errors are logged but
// the response is always 200 — LiveKit retries 5xx, which would replay
// already-applied state changes.
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	event, err := webhook.ReceiveWebhookEvent(r, h.keyProvider)
	if err != nil {
		h.log.Error("webhook: signature verification failed", "error", err)
		http.Error(w, "invalid webhook", http.StatusUnauthorized)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	roomName := event.GetRoom().GetName()
	eventType := event.GetEvent()

	h.log.Debug("webhook: received",
		"event", eventType, "room", roomName, "id", event.GetId())

	switch eventType {
	case WebhookEventParticipantJoined:
		h.dispatchParticipantJoined(ctx, event, roomName)
	case WebhookEventParticipantLeft:
		h.dispatchParticipantLeft(ctx, event, roomName)
	case WebhookEventRoomFinished:
		h.dispatchRoomFinished(ctx, event, roomName)
	case WebhookEventEgressEnded:
		h.dispatchEgressEnded(ctx, event)
	case WebhookEventEgressStarted:
		// nothing built-in; subscribers may handle.
	}

	h.runSubscribers(ctx, eventType, event)

	w.WriteHeader(http.StatusOK)
}

// dispatchParticipantJoined transitions a 1:1 call to connected when the
// second participant joins. Meeting rooms are no-op here (joins are
// driven by the JoinMeeting RPC).
func (h *WebhookHandler) dispatchParticipantJoined(ctx context.Context, event *livekitproto.WebhookEvent, roomName string) {
	if !IsCallRoom(roomName) {
		return
	}
	if event.GetRoom().GetNumParticipants() < 2 {
		return
	}
	callID, err := CallIDFromRoomName(roomName)
	if err != nil {
		h.log.Error("webhook: invalid call room", "room", roomName, "error", err)
		return
	}
	if err := h.service.MarkConnected(ctx, callID); err != nil {
		h.log.Error("webhook: MarkConnected", "call_id", callID, "error", err)
	}
}

// dispatchParticipantLeft ends a 1:1 call (single hangup ends it) and
// reduces meeting active-participant count via Service.LeaveMeeting.
func (h *WebhookHandler) dispatchParticipantLeft(ctx context.Context, event *livekitproto.WebhookEvent, roomName string) {
	switch {
	case IsCallRoom(roomName):
		if err := h.service.EndCallByRoom(ctx, roomName, "participant_left"); err != nil {
			h.log.Error("webhook: EndCallByRoom", "room", roomName, "error", err)
		}
	case IsMeetingRoom(roomName):
		identity := event.GetParticipant().GetIdentity()
		if identity == "" {
			h.log.Warn("webhook: participant_left with empty identity", "room", roomName)
			return
		}
		userID, err := parseUUID(identity)
		if err != nil {
			h.log.Error("webhook: invalid participant identity", "identity", identity, "error", err)
			return
		}
		meetingID, err := MeetingIDFromRoomName(roomName)
		if err != nil {
			h.log.Error("webhook: invalid meeting room", "room", roomName, "error", err)
			return
		}
		if err := h.service.LeaveMeeting(ctx, meetingID, userID); err != nil {
			h.log.Error("webhook: LeaveMeeting", "room", roomName, "error", err)
		}
	}
}

// dispatchRoomFinished is the safety-net cleanup for orphaned rooms.
func (h *WebhookHandler) dispatchRoomFinished(ctx context.Context, _ *livekitproto.WebhookEvent, roomName string) {
	switch {
	case IsCallRoom(roomName):
		if err := h.service.EndCallByRoom(ctx, roomName, "room_finished"); err != nil {
			h.log.Error("webhook: EndCallByRoom (room_finished)", "room", roomName, "error", err)
		}
	case IsMeetingRoom(roomName):
		if err := h.service.EndMeetingByRoom(ctx, roomName); err != nil {
			h.log.Error("webhook: EndMeetingByRoom", "room", roomName, "error", err)
		}
	}
}

// dispatchEgressEnded finalises a recording's metadata.
func (h *WebhookHandler) dispatchEgressEnded(ctx context.Context, event *livekitproto.WebhookEvent) {
	if h.recordings == nil {
		return
	}
	info := event.GetEgressInfo()
	if info == nil {
		return
	}
	failed := info.GetStatus() == livekitproto.EgressStatus_EGRESS_FAILED
	var filePath string
	var fileSize int64
	var durationNs int64
	if results := info.GetFileResults(); len(results) > 0 {
		filePath = results[0].GetFilename()
		fileSize = results[0].GetSize()
		durationNs = results[0].GetDuration()
	}
	if err := h.recordings.HandleEgressEnded(ctx, info.GetEgressId(), filePath, fileSize, durationNs, failed); err != nil {
		h.log.Error("webhook: HandleEgressEnded", "egress_id", info.GetEgressId(), "error", err)
	}
}

func (h *WebhookHandler) runSubscribers(ctx context.Context, eventType string, event *livekitproto.WebhookEvent) {
	h.mu.RLock()
	subs := append([]WebhookSubscriber(nil), h.subscribers[eventType]...)
	h.mu.RUnlock()
	for _, s := range subs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					h.log.Error("webhook subscriber panic", "event", eventType, "recover", r)
				}
			}()
			s(ctx, event)
		}()
	}
}
