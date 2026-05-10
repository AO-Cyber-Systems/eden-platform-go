package livekit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/livekit/protocol/auth"
	livekitproto "github.com/livekit/protocol/livekit"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	webhookTestKey    = "APItestkeyforwebhookXX"
	webhookTestSecret = "test-secret-with-a-bunch-of-entropy-1234567890"
)

// signLiveKitWebhook builds a signed http.Request matching what the LiveKit
// notifier produces. The handshake is documented in protocol/webhook/verifier.go:
// the body is JSON, the SHA-256 of the body is embedded in the JWT's `sha256`
// claim, and the JWT is sent via the Authorization header.
func signLiveKitWebhook(t *testing.T, event *livekitproto.WebhookEvent) *http.Request {
	t.Helper()
	body, err := protojson.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	sha := sha256.Sum256(body)
	hash := base64.StdEncoding.EncodeToString(sha[:])

	tok := auth.NewAccessToken(webhookTestKey, webhookTestSecret)
	tok.SetValidFor(5 * time.Minute).SetSha256(hash)
	jwt, err := tok.ToJWT()
	if err != nil {
		t.Fatalf("ToJWT: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", jwt)
	return req
}

func newTestWebhookHandler(t *testing.T) (*WebhookHandler, *Service, *RecordingService, *InMemoryStore, *fakeRooms, *fakeEgress) {
	t.Helper()
	store := NewInMemoryStore()
	rooms := &fakeRooms{}
	tokens := &fakeTokens{}
	clock := NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	svc, err := NewService(ServiceConfig{
		Store:      store,
		Rooms:      rooms,
		Tokens:     tokens,
		Signaler:   NoopSignaler{},
		Clock:      clock,
		LiveKitURL: "wss://lk.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	egress := &fakeEgress{}
	rs, err := NewRecordingService(store, egress, RecordingConfig{S3: validS3()}, clock)
	if err != nil {
		t.Fatal(err)
	}
	kp := auth.NewSimpleKeyProvider(webhookTestKey, webhookTestSecret)
	h, err := NewWebhookHandler(WebhookConfig{
		Service:          svc,
		RecordingService: rs,
		KeyProvider:      kp,
	})
	if err != nil {
		t.Fatal(err)
	}
	return h, svc, rs, store, rooms, egress
}

func TestWebhook_RejectsInvalidSignature(t *testing.T) {
	h, _, _, _, _, _ := newTestWebhookHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader([]byte(`{"event":"room_finished"}`)))
	req.Header.Set("Authorization", "obviously-not-a-valid-jwt")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestWebhook_RejectsTamperedBody(t *testing.T) {
	h, _, _, _, _, _ := newTestWebhookHandler(t)
	event := &livekitproto.WebhookEvent{Event: "room_finished", Room: &livekitproto.Room{Name: "call-x"}}
	req := signLiveKitWebhook(t, event)

	// Tamper with the body by replacing it.
	tampered := []byte(`{"event":"egress_ended"}`)
	req.Body = http.NoBody
	req.Body = newBodyReader(tampered)
	req.ContentLength = int64(len(tampered))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("tampered body should be rejected, status = %d", w.Code)
	}
}

// newBodyReader returns an http.Body-compatible reader for raw bytes.
func newBodyReader(b []byte) *readCloser {
	return &readCloser{bytes.NewReader(b)}
}

type readCloser struct{ *bytes.Reader }

func (r *readCloser) Close() error { return nil }

func TestWebhook_ParticipantJoined_MarksConnected(t *testing.T) {
	h, svc, _, store, _, _ := newTestWebhookHandler(t)
	ctx := context.Background()

	caller, callee := uuid.New(), uuid.New()
	call, _ := svc.InitiateCall(ctx, caller, callee, CallTypeVideo)
	if _, err := svc.AcceptCall(ctx, call.ID, callee); err != nil {
		t.Fatal(err)
	}
	roomName := "call-" + call.ID.String()

	event := &livekitproto.WebhookEvent{
		Event: WebhookEventParticipantJoined,
		Room:  &livekitproto.Room{Name: roomName, NumParticipants: 2},
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, signLiveKitWebhook(t, event))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d", w.Code)
	}

	stored, _ := store.GetCall(ctx, call.ID)
	if stored.State != StateConnected {
		t.Errorf("state = %s, want connected", stored.State)
	}
	if stored.ConnectedAt == nil {
		t.Error("ConnectedAt nil")
	}
}

func TestWebhook_ParticipantLeft_EndsCall(t *testing.T) {
	h, svc, _, store, _, _ := newTestWebhookHandler(t)
	ctx := context.Background()
	caller, callee := uuid.New(), uuid.New()
	call, _ := svc.InitiateCall(ctx, caller, callee, CallTypeVideo)
	_, _ = svc.AcceptCall(ctx, call.ID, callee)
	roomName := "call-" + call.ID.String()

	event := &livekitproto.WebhookEvent{
		Event: WebhookEventParticipantLeft,
		Room:  &livekitproto.Room{Name: roomName},
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, signLiveKitWebhook(t, event))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d", w.Code)
	}

	stored, _ := store.GetCall(ctx, call.ID)
	if stored.State != StateEnded {
		t.Errorf("state = %s", stored.State)
	}
	if stored.EndReason != "participant_left" {
		t.Errorf("end_reason = %q", stored.EndReason)
	}
}

func TestWebhook_RoomFinished_MeetingSafetyNet(t *testing.T) {
	h, svc, _, store, _, _ := newTestWebhookHandler(t)
	ctx := context.Background()
	creator := uuid.New()
	m, _ := svc.CreateMeeting(ctx, CreateMeetingInput{Title: "T", CreatorID: creator})
	if _, err := svc.JoinMeeting(ctx, m.ID, creator); err != nil {
		t.Fatal(err)
	}
	roomName := "meeting-" + m.ID.String()

	event := &livekitproto.WebhookEvent{
		Event: WebhookEventRoomFinished,
		Room:  &livekitproto.Room{Name: roomName},
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, signLiveKitWebhook(t, event))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d", w.Code)
	}
	stored, _ := store.GetMeeting(ctx, m.ID)
	if stored.State != MeetingStateEnded {
		t.Errorf("state = %s", stored.State)
	}
}

func TestWebhook_EgressEnded_FinalizesRecording(t *testing.T) {
	h, _, rs, store, _, _ := newTestWebhookHandler(t)
	ctx := context.Background()

	rec, _ := rs.StartRecording(ctx, uuid.New(), "meeting-test")

	event := &livekitproto.WebhookEvent{
		Event: WebhookEventEgressEnded,
		Room:  &livekitproto.Room{Name: "meeting-test"},
		EgressInfo: &livekitproto.EgressInfo{
			EgressId: rec.EgressID,
			Status:   livekitproto.EgressStatus_EGRESS_COMPLETE,
			FileResults: []*livekitproto.FileInfo{
				{
					Filename: "s3://recordings/test.mp4",
					Size:     1024,
					Duration: int64(2 * time.Minute),
				},
			},
		},
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, signLiveKitWebhook(t, event))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d", w.Code)
	}
	stored, _ := store.GetRecording(ctx, rec.ID)
	if stored.State != RecordingStateCompleted {
		t.Errorf("state = %s", stored.State)
	}
	if stored.FilePath != "s3://recordings/test.mp4" {
		t.Errorf("filepath = %q", stored.FilePath)
	}
	if stored.Duration != 2*time.Minute {
		t.Errorf("duration = %v", stored.Duration)
	}
}

func TestWebhook_Subscribe_FiresAfterBuiltin(t *testing.T) {
	h, svc, _, _, _, _ := newTestWebhookHandler(t)
	ctx := context.Background()
	caller, callee := uuid.New(), uuid.New()
	call, _ := svc.InitiateCall(ctx, caller, callee, CallTypeVideo)
	_, _ = svc.AcceptCall(ctx, call.ID, callee)

	var calls atomic.Int32
	h.Subscribe(WebhookEventParticipantJoined, func(_ context.Context, _ *livekitproto.WebhookEvent) {
		calls.Add(1)
	})

	event := &livekitproto.WebhookEvent{
		Event: WebhookEventParticipantJoined,
		Room:  &livekitproto.Room{Name: "call-" + call.ID.String(), NumParticipants: 2},
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, signLiveKitWebhook(t, event))
	if w.Code != http.StatusOK {
		t.Errorf("status = %d", w.Code)
	}
	if calls.Load() != 1 {
		t.Errorf("subscriber called %d times, want 1", calls.Load())
	}

	// Different event type should not fire this subscriber.
	w = httptest.NewRecorder()
	h.ServeHTTP(w, signLiveKitWebhook(t, &livekitproto.WebhookEvent{
		Event: WebhookEventRoomFinished,
		Room:  &livekitproto.Room{Name: "call-" + call.ID.String()},
	}))
	if calls.Load() != 1 {
		t.Errorf("subscriber should not fire for unrelated event")
	}
}

func TestNewWebhookHandler_Validates(t *testing.T) {
	kp := auth.NewSimpleKeyProvider(webhookTestKey, webhookTestSecret)
	if _, err := NewWebhookHandler(WebhookConfig{KeyProvider: kp}); err == nil {
		t.Error("missing service should fail")
	}
	store := NewInMemoryStore()
	svc, _ := NewService(ServiceConfig{
		Store:      store,
		Rooms:      &fakeRooms{},
		Tokens:     &fakeTokens{},
		LiveKitURL: "wss://lk",
	})
	if _, err := NewWebhookHandler(WebhookConfig{Service: svc}); err == nil {
		t.Error("missing key provider should fail")
	}
}
