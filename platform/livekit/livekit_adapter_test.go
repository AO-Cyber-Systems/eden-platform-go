package livekit

import (
	"errors"
	"testing"
	"time"

	"github.com/livekit/protocol/auth"
)

func TestNewLiveKitAdapter_RejectsEmptyConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  LiveKitConfig
	}{
		{"empty url", LiveKitConfig{APIKey: "k", APISecret: "s"}},
		{"empty key", LiveKitConfig{URL: "wss://lk", APISecret: "s"}},
		{"empty secret", LiveKitConfig{URL: "wss://lk", APIKey: "k"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := NewLiveKitAdapter(c.cfg)
			if !errors.Is(err, ErrConfig) {
				t.Errorf("want ErrConfig, got %v", err)
			}
		})
	}
}

func TestNewLiveKitAdapter_AcceptsValidConfig(t *testing.T) {
	a, err := NewLiveKitAdapter(LiveKitConfig{
		URL:       "wss://lk.example.com",
		APIKey:    "APIxxxxxxxxxxxxx",
		APISecret: "secret-must-be-at-least-32-characters-long",
	})
	if err != nil {
		t.Fatalf("NewLiveKitAdapter: %v", err)
	}
	if a.URL() != "wss://lk.example.com" {
		t.Errorf("URL() = %q", a.URL())
	}
}

func TestIssueJoinToken_SignedAndVerifiable(t *testing.T) {
	const apiKey = "APItestkey1234567"
	// LiveKit's auth library requires HS256-friendly secret length.
	const apiSecret = "test-secret-with-plenty-of-entropy-1234567890"

	a, err := NewLiveKitAdapter(LiveKitConfig{URL: "wss://lk.test", APIKey: apiKey, APISecret: apiSecret})
	if err != nil {
		t.Fatal(err)
	}

	jwt, err := a.IssueJoinToken("call-abc", "user-123", "Alice", 0)
	if err != nil {
		t.Fatalf("IssueJoinToken: %v", err)
	}
	if jwt == "" {
		t.Fatal("empty JWT")
	}

	v, err := auth.ParseAPIToken(jwt)
	if err != nil {
		t.Fatalf("ParseAPIToken: %v", err)
	}
	if v.APIKey() != apiKey {
		t.Errorf("APIKey = %q, want %q", v.APIKey(), apiKey)
	}
	if v.Identity() != "user-123" {
		t.Errorf("Identity = %q", v.Identity())
	}

	claims, grants, err := v.Verify(apiSecret)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Issuer != apiKey {
		t.Errorf("issuer = %q", claims.Issuer)
	}
	if grants.Video == nil {
		t.Fatal("VideoGrant nil")
	}
	if grants.Video.Room != "call-abc" {
		t.Errorf("Room = %q", grants.Video.Room)
	}
	if !grants.Video.RoomJoin {
		t.Errorf("RoomJoin should be true")
	}
}

func TestIssueJoinToken_TTL(t *testing.T) {
	const apiKey = "APItestkey1234567"
	const apiSecret = "test-secret-with-plenty-of-entropy-1234567890"
	a, _ := NewLiveKitAdapter(LiveKitConfig{URL: "wss://lk.test", APIKey: apiKey, APISecret: apiSecret})

	// Default TTL (1h)
	jwt, err := a.IssueJoinToken("call-x", "user-x", "User X", 0)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := auth.ParseAPIToken(jwt)
	claims, _, err := v.Verify(apiSecret)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	expiresIn := claims.Expiry.Time().Sub(time.Now())
	if expiresIn < 55*time.Minute || expiresIn > 65*time.Minute {
		t.Errorf("default TTL ≈ 1h, got %v", expiresIn)
	}

	// Custom TTL (5min)
	jwt, _ = a.IssueJoinToken("call-y", "user-y", "User Y", 5*time.Minute)
	v, _ = auth.ParseAPIToken(jwt)
	claims, _, _ = v.Verify(apiSecret)
	expiresIn = claims.Expiry.Time().Sub(time.Now())
	if expiresIn < 4*time.Minute || expiresIn > 6*time.Minute {
		t.Errorf("custom TTL ≈ 5m, got %v", expiresIn)
	}
}

func TestLiveKitAdapter_KeyProvider(t *testing.T) {
	a, _ := NewLiveKitAdapter(LiveKitConfig{URL: "wss://lk", APIKey: "key", APISecret: "secret-with-entropy-and-some-length"})
	kp := a.KeyProvider()
	if kp == nil {
		t.Fatal("KeyProvider nil")
	}
	if got := kp.GetSecret("key"); got != "secret-with-entropy-and-some-length" {
		t.Errorf("KeyProvider lookup wrong, got %q", got)
	}
}

// Compile-time interface assertions live alongside the production code, but
// double-check here for clarity.
func TestLiveKitAdapter_ImplementsRoomsAndTokens(t *testing.T) {
	a, _ := NewLiveKitAdapter(LiveKitConfig{URL: "wss://lk", APIKey: "k", APISecret: "secret-with-entropy-and-some-length"})
	var _ Rooms = a
	var _ Tokens = a
}
