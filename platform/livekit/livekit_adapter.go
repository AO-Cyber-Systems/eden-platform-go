package livekit

import (
	"context"
	"fmt"
	"time"

	"github.com/livekit/protocol/auth"
	livekitproto "github.com/livekit/protocol/livekit"
	lksdk "github.com/livekit/server-sdk-go/v2"
)

// LiveKitConfig holds the connection params for the LiveKit server SDK.
type LiveKitConfig struct {
	URL       string // wss://lk.example.com or http(s):// for SDK calls
	APIKey    string
	APISecret string
}

// EgressClient is the subset of the LiveKit SDK egress surface the platform
// uses. The SDK's *lksdk.EgressClient satisfies it. Tests substitute a fake.
type EgressClient interface {
	StartRoomCompositeEgress(ctx context.Context, req *livekitproto.RoomCompositeEgressRequest) (*livekitproto.EgressInfo, error)
	StopEgress(ctx context.Context, req *livekitproto.StopEgressRequest) (*livekitproto.EgressInfo, error)
}

// LiveKitAdapter wires the LiveKit server SDK into the platform's Rooms /
// Tokens / EgressClient interfaces. It is the only file in this package that
// imports github.com/livekit/* — all other code talks to LiveKit through the
// interfaces defined in rooms.go.
type LiveKitAdapter struct {
	cfg          LiveKitConfig
	roomClient   *lksdk.RoomServiceClient
	egressClient *lksdk.EgressClient
}

// NewLiveKitAdapter validates cfg and constructs an adapter wired to the
// LiveKit server SDK. The SDK's room/egress clients are created eagerly so
// configuration errors surface at startup, not on first use.
func NewLiveKitAdapter(cfg LiveKitConfig) (*LiveKitAdapter, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("%w: URL is required", ErrConfig)
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("%w: APIKey is required", ErrConfig)
	}
	if cfg.APISecret == "" {
		return nil, fmt.Errorf("%w: APISecret is required", ErrConfig)
	}
	return &LiveKitAdapter{
		cfg:          cfg,
		roomClient:   lksdk.NewRoomServiceClient(cfg.URL, cfg.APIKey, cfg.APISecret),
		egressClient: lksdk.NewEgressClient(cfg.URL, cfg.APIKey, cfg.APISecret),
	}, nil
}

// URL returns the configured LiveKit server URL.
func (a *LiveKitAdapter) URL() string { return a.cfg.URL }

// CreateCallRoom creates a 1:1 call room. Per the eden-circle convention the
// name is "call-{callID}", with a 30-second empty timeout and 2-participant
// cap.
func (a *LiveKitAdapter) CreateCallRoom(ctx context.Context, callID string) (RoomInfo, error) {
	const emptyTimeoutSecs uint32 = 30
	const maxParticipants uint32 = 2
	room, err := a.roomClient.CreateRoom(ctx, &livekitproto.CreateRoomRequest{
		Name:            "call-" + callID,
		EmptyTimeout:    emptyTimeoutSecs,
		MaxParticipants: maxParticipants,
	})
	if err != nil {
		return RoomInfo{}, fmt.Errorf("create call room: %w", err)
	}
	return roomInfoFromProto(room), nil
}

// CreateMeetingRoom creates a multi-party meeting room ("meeting-{name}",
// 5-minute empty timeout, configurable max participants).
func (a *LiveKitAdapter) CreateMeetingRoom(ctx context.Context, name string, maxParticipants int) (RoomInfo, error) {
	const emptyTimeoutSecs uint32 = 300
	if maxParticipants <= 0 {
		maxParticipants = DefaultMaxMeetingParticipants
	}
	room, err := a.roomClient.CreateRoom(ctx, &livekitproto.CreateRoomRequest{
		Name:            "meeting-" + name,
		EmptyTimeout:    emptyTimeoutSecs,
		MaxParticipants: uint32(maxParticipants),
	})
	if err != nil {
		return RoomInfo{}, fmt.Errorf("create meeting room: %w", err)
	}
	return roomInfoFromProto(room), nil
}

// DeleteRoom force-closes a room.
func (a *LiveKitAdapter) DeleteRoom(ctx context.Context, roomName string) error {
	_, err := a.roomClient.DeleteRoom(ctx, &livekitproto.DeleteRoomRequest{Room: roomName})
	if err != nil {
		return fmt.Errorf("delete room %q: %w", roomName, err)
	}
	return nil
}

// IssueJoinToken signs a JWT granting identity permission to join roomName
// for ttl. Empty ttl falls back to one hour. The displayName populates the
// token's "name" claim, which LiveKit surfaces to other participants.
func (a *LiveKitAdapter) IssueJoinToken(roomName, identity, displayName string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = time.Hour
	}
	token := auth.NewAccessToken(a.cfg.APIKey, a.cfg.APISecret)
	token.SetVideoGrant(&auth.VideoGrant{
		RoomJoin: true,
		Room:     roomName,
	}).
		SetIdentity(identity).
		SetName(displayName).
		SetValidFor(ttl)
	jwt, err := token.ToJWT()
	if err != nil {
		return "", fmt.Errorf("sign join token: %w", err)
	}
	return jwt, nil
}

// EgressClient returns the LiveKit egress client suitable for use with
// RecordingService. The returned interface is the subset the platform uses;
// callers needing the full SDK can call EgressSDK.
func (a *LiveKitAdapter) EgressClient() EgressClient { return a.egressClient }

// EgressSDK returns the underlying SDK egress client for advanced use cases
// not covered by the EgressClient interface.
func (a *LiveKitAdapter) EgressSDK() *lksdk.EgressClient { return a.egressClient }

// KeyProvider returns a key provider suitable for verifying LiveKit webhook
// signatures (consumed by WebhookHandler in 28-03).
func (a *LiveKitAdapter) KeyProvider() *auth.SimpleKeyProvider {
	return auth.NewSimpleKeyProvider(a.cfg.APIKey, a.cfg.APISecret)
}

// Compile-time interface satisfaction.
var (
	_ Rooms  = (*LiveKitAdapter)(nil)
	_ Tokens = (*LiveKitAdapter)(nil)
)

func roomInfoFromProto(r *livekitproto.Room) RoomInfo {
	if r == nil {
		return RoomInfo{}
	}
	return RoomInfo{
		Name:             r.GetName(),
		MaxParticipants:  r.GetMaxParticipants(),
		NumParticipants:  r.GetNumParticipants(),
		EmptyTimeoutSecs: r.GetEmptyTimeout(),
	}
}
