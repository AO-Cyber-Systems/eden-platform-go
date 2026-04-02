package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/cloudflare/circl/sign/mldsa/mldsa65"
)

func newTestJWTManager(t *testing.T) *JWTManager {
	t.Helper()
	manager, err := NewJWTManager(JWTConfig{
		Issuer:             "test",
		AccessTokenExpiry:  time.Minute,
		RefreshTokenExpiry: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewJWTManager() error = %v", err)
	}
	return manager
}

func TestJWTManager_CreateAndValidateAccessToken(t *testing.T) {
	manager := newTestJWTManager(t)

	token, err := manager.CreateAccessToken("user-1", "company-1", "admin", 80, []string{"company-1", "company-2"})
	if err != nil {
		t.Fatalf("CreateAccessToken() error = %v", err)
	}

	claims, err := manager.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}

	if claims.UserID != "user-1" {
		t.Errorf("UserID = %q, want %q", claims.UserID, "user-1")
	}
	if claims.CompanyID != "company-1" {
		t.Errorf("CompanyID = %q, want %q", claims.CompanyID, "company-1")
	}
	if claims.Role != "admin" {
		t.Errorf("Role = %q, want %q", claims.Role, "admin")
	}
	if claims.RoleLevel != 80 {
		t.Errorf("RoleLevel = %d, want %d", claims.RoleLevel, 80)
	}
	if len(claims.CompanyIDs) != 2 {
		t.Errorf("CompanyIDs length = %d, want 2", len(claims.CompanyIDs))
	}
	if claims.Subject != "user-1" {
		t.Errorf("Subject = %q, want %q", claims.Subject, "user-1")
	}
	if claims.Issuer != "test" {
		t.Errorf("Issuer = %q, want %q", claims.Issuer, "test")
	}
}

func TestJWTManager_CreateAndValidateRefreshToken(t *testing.T) {
	manager := newTestJWTManager(t)

	token, err := manager.CreateRefreshToken("user-1")
	if err != nil {
		t.Fatalf("CreateRefreshToken() error = %v", err)
	}

	claims, err := manager.ValidateRefreshToken(token)
	if err != nil {
		t.Fatalf("ValidateRefreshToken() error = %v", err)
	}

	if claims.Subject != "user-1" {
		t.Errorf("Subject = %q, want %q", claims.Subject, "user-1")
	}
	if claims.Issuer != "test" {
		t.Errorf("Issuer = %q, want %q", claims.Issuer, "test")
	}
}

func TestJWTManager_ExpiredAccessToken(t *testing.T) {
	manager, err := NewJWTManager(JWTConfig{
		Issuer:             "test",
		AccessTokenExpiry:  time.Millisecond,
		RefreshTokenExpiry: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewJWTManager() error = %v", err)
	}

	token, err := manager.CreateAccessToken("user-1", "company-1", "admin", 80, nil)
	if err != nil {
		t.Fatalf("CreateAccessToken() error = %v", err)
	}

	time.Sleep(5 * time.Millisecond)

	_, err = manager.ValidateAccessToken(token)
	if err == nil {
		t.Errorf("ValidateAccessToken() expected error for expired token, got nil")
	}
}

func TestJWTManager_WrongSigningKey(t *testing.T) {
	manager1 := newTestJWTManager(t)
	manager2 := newTestJWTManager(t)

	token, err := manager1.CreateAccessToken("user-1", "company-1", "admin", 80, nil)
	if err != nil {
		t.Fatalf("CreateAccessToken() error = %v", err)
	}

	_, err = manager2.ValidateAccessToken(token)
	if err == nil {
		t.Errorf("ValidateAccessToken() expected error for wrong signing key, got nil")
	}
}

func TestJWTManager_EphemeralKeyGeneration(t *testing.T) {
	manager, err := NewJWTManager(JWTConfig{
		Issuer:             "test-ephemeral",
		AccessTokenExpiry:  time.Minute,
		RefreshTokenExpiry: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewJWTManager() with empty paths error = %v", err)
	}

	token, err := manager.CreateAccessToken("user-1", "company-1", "member", 40, nil)
	if err != nil {
		t.Fatalf("CreateAccessToken() error = %v", err)
	}

	claims, err := manager.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}
	if claims.UserID != "user-1" {
		t.Errorf("UserID = %q, want %q", claims.UserID, "user-1")
	}
}

func TestJWTManager_MLDSA65SignatureSize(t *testing.T) {
	manager := newTestJWTManager(t)

	token, err := manager.CreateAccessToken("user-1", "company-1", "admin", 80, nil)
	if err != nil {
		t.Fatalf("CreateAccessToken() error = %v", err)
	}

	// ML-DSA-65 tokens are significantly larger than ES256 due to ~3309 byte signatures.
	// Base64-encoded, the token should be roughly 4-5KB.
	if len(token) < 3000 {
		t.Errorf("Token unexpectedly small (%d bytes) — expected ML-DSA-65 signature overhead", len(token))
	}
	// But still well under HTTP header limits.
	if len(token) > 8000 {
		t.Errorf("Token unexpectedly large (%d bytes) — may hit HTTP header limits", len(token))
	}
	t.Logf("ML-DSA-65 access token size: %d bytes", len(token))
}

func TestJWTManager_SeedDeterminism(t *testing.T) {
	seed, err := GenerateKeySeed()
	if err != nil {
		t.Fatalf("GenerateKeySeed() error = %v", err)
	}

	pk1, sk1 := mldsa65.NewKeyFromSeed(&seed)
	pk2, sk2 := mldsa65.NewKeyFromSeed(&seed)

	// Same seed must produce identical keys.
	if pk1.Bytes() == nil || string(pk1.Bytes()) != string(pk2.Bytes()) {
		t.Errorf("NewKeyFromSeed() produced different public keys for same seed")
	}
	_ = sk1
	_ = sk2
}

func TestJWTManager_ShortLivedToken(t *testing.T) {
	manager := newTestJWTManager(t)

	token, err := manager.CreateShortLivedToken("test-subject", 5*time.Minute)
	if err != nil {
		t.Fatalf("CreateShortLivedToken() error = %v", err)
	}

	subject, err := manager.ValidateShortLivedToken(token)
	if err != nil {
		t.Fatalf("ValidateShortLivedToken() error = %v", err)
	}
	if subject != "test-subject" {
		t.Errorf("Subject = %q, want %q", subject, "test-subject")
	}
}

func TestHashToken(t *testing.T) {
	token := "test-token-value"
	hash1 := HashToken(token)
	hash2 := HashToken(token)

	if hash1 != hash2 {
		t.Errorf("HashToken() not deterministic: %q != %q", hash1, hash2)
	}

	expected := sha256.Sum256([]byte(token))
	expectedHex := hex.EncodeToString(expected[:])
	if hash1 != expectedHex {
		t.Errorf("HashToken() = %q, want SHA-256 hex %q", hash1, expectedHex)
	}

	hash3 := HashToken("different-token")
	if hash1 == hash3 {
		t.Errorf("HashToken() same for different inputs")
	}
}
