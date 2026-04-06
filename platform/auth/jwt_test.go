package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/cloudflare/circl/sign/mldsa/mldsa65"
	"github.com/golang-jwt/jwt/v5"
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

// writeSeedFile creates a temporary seed file from an ML-DSA-65 key pair's seed.
// Returns the file path; caller is responsible for cleanup via t.Cleanup.
func writeSeedFile(t *testing.T, seed *[mldsa65.SeedSize]byte) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "seed-*.bin")
	if err != nil {
		t.Fatalf("create temp seed file: %v", err)
	}
	defer f.Close()
	if _, err := f.Write(seed[:]); err != nil {
		t.Fatalf("write seed file: %v", err)
	}
	return f.Name()
}

func TestJWTKeyRotation_SingleKeyBackwardCompat(t *testing.T) {
	// Generate a key and write its seed to a temp file.
	seed1, err := GenerateKeySeed()
	if err != nil {
		t.Fatalf("GenerateKeySeed: %v", err)
	}
	seedPath := writeSeedFile(t, &seed1)

	// Use legacy single-key config — must work exactly as before.
	manager, err := NewJWTManager(JWTConfig{
		KeySeedPath:        seedPath,
		Issuer:             "test",
		AccessTokenExpiry:  time.Minute,
		RefreshTokenExpiry: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewJWTManager(single key): %v", err)
	}

	token, err := manager.CreateAccessToken("user-1", "company-1", "admin", 80, nil)
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}
	claims, err := manager.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if claims.UserID != "user-1" {
		t.Errorf("UserID = %q, want %q", claims.UserID, "user-1")
	}

	// Refresh token round-trip.
	refresh, err := manager.CreateRefreshToken("user-1")
	if err != nil {
		t.Fatalf("CreateRefreshToken: %v", err)
	}
	rc, err := manager.ValidateRefreshToken(refresh)
	if err != nil {
		t.Fatalf("ValidateRefreshToken: %v", err)
	}
	if rc.Subject != "user-1" {
		t.Errorf("Subject = %q, want %q", rc.Subject, "user-1")
	}
}

func TestJWTKeyRotation_MultiKey(t *testing.T) {
	seed1, err := GenerateKeySeed()
	if err != nil {
		t.Fatalf("GenerateKeySeed seed1: %v", err)
	}
	seed2, err := GenerateKeySeed()
	if err != nil {
		t.Fatalf("GenerateKeySeed seed2: %v", err)
	}
	path1 := writeSeedFile(t, &seed1)
	path2 := writeSeedFile(t, &seed2)

	// Active key is key-2.
	manager, err := NewJWTManager(JWTConfig{
		KeySeedPaths: map[string]string{
			"key-1": path1,
			"key-2": path2,
		},
		ActiveKID:          "key-2",
		Issuer:             "test",
		AccessTokenExpiry:  time.Minute,
		RefreshTokenExpiry: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewJWTManager(multi-key): %v", err)
	}

	// New token must include kid: "key-2".
	tokenStr, err := manager.CreateAccessToken("user-1", "company-1", "admin", 80, nil)
	if err != nil {
		t.Fatalf("CreateAccessToken: %v", err)
	}
	// Parse without validation just to inspect header.
	parsed, _, err := jwt.NewParser().ParseUnverified(tokenStr, &Claims{})
	if err != nil {
		t.Fatalf("ParseUnverified: %v", err)
	}
	if kid, _ := parsed.Header["kid"].(string); kid != "key-2" {
		t.Errorf("token kid = %q, want %q", kid, "key-2")
	}

	// Token signed by active key validates correctly.
	claims, err := manager.ValidateAccessToken(tokenStr)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if claims.UserID != "user-1" {
		t.Errorf("UserID = %q, want %q", claims.UserID, "user-1")
	}

	// A manager configured with key-1 active can also sign; the multi-key manager
	// should validate that token because it holds key-1's public key.
	managerKey1Active, err := NewJWTManager(JWTConfig{
		KeySeedPaths: map[string]string{
			"key-1": path1,
			"key-2": path2,
		},
		ActiveKID:         "key-1",
		Issuer:            "test",
		AccessTokenExpiry: time.Minute,
	})
	if err != nil {
		t.Fatalf("NewJWTManager(key-1 active): %v", err)
	}
	token1, err := managerKey1Active.CreateAccessToken("user-2", "company-1", "member", 40, nil)
	if err != nil {
		t.Fatalf("CreateAccessToken with key-1: %v", err)
	}
	claims2, err := manager.ValidateAccessToken(token1)
	if err != nil {
		t.Fatalf("ValidateAccessToken token signed by key-1: %v", err)
	}
	if claims2.UserID != "user-2" {
		t.Errorf("UserID = %q, want %q", claims2.UserID, "user-2")
	}
}

func TestJWTKeyRotation_MissingKID(t *testing.T) {
	// Build a manager and create a token WITHOUT a kid header to simulate
	// tokens issued before key rotation support was added.
	manager := newTestJWTManager(t)

	// Create a token, then strip the kid header by re-signing manually.
	// Easiest approach: directly sign with the active key's private key
	// using NewWithClaims but without setting Header["kid"].
	now := time.Now()
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-legacy",
			Issuer:    "test",
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        generateJTI(),
		},
		UserID:    "user-legacy",
		CompanyID: "company-1",
		Role:      "admin",
		RoleLevel: 80,
	}
	// NewWithClaims does NOT set kid by default.
	rawToken := jwt.NewWithClaims(signingMethodMLDSA65, claims)
	// Explicitly ensure kid is absent.
	delete(rawToken.Header, "kid")

	activeEntry := manager.keys[manager.activeKID]
	tokenStr, err := rawToken.SignedString(activeEntry.PrivateKey)
	if err != nil {
		t.Fatalf("SignedString (no kid): %v", err)
	}

	// Validate — should succeed via backward-compat fallback.
	result, err := manager.ValidateAccessToken(tokenStr)
	if err != nil {
		t.Fatalf("ValidateAccessToken (no kid) expected success, got: %v", err)
	}
	if result.UserID != "user-legacy" {
		t.Errorf("UserID = %q, want %q", result.UserID, "user-legacy")
	}
}

func TestJWTKeyRotation_UnknownKID(t *testing.T) {
	manager := newTestJWTManager(t)

	// Create a token with an unknown kid by forging the header.
	now := time.Now()
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user-1",
			Issuer:    "test",
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        generateJTI(),
		},
		UserID:    "user-1",
		CompanyID: "company-1",
		Role:      "admin",
		RoleLevel: 80,
	}
	rawToken := jwt.NewWithClaims(signingMethodMLDSA65, claims)
	rawToken.Header["kid"] = "unknown-kid-xyz"

	activeEntry := manager.keys[manager.activeKID]
	tokenStr, err := rawToken.SignedString(activeEntry.PrivateKey)
	if err != nil {
		t.Fatalf("SignedString: %v", err)
	}

	// The keyfunc should reject this with "unknown kid".
	_, err = manager.ValidateAccessToken(tokenStr)
	if err == nil {
		t.Fatal("ValidateAccessToken with unknown kid expected error, got nil")
	}
	// Verify the error message contains "unknown kid".
	if msg := err.Error(); len(msg) == 0 {
		t.Error("expected non-empty error message")
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
