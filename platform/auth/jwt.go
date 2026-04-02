package auth

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/cloudflare/circl/sign/mldsa/mldsa65"
	"github.com/golang-jwt/jwt/v5"
)

// Claims represents the JWT claims included in access tokens.
type Claims struct {
	jwt.RegisteredClaims
	UserID     string   `json:"uid"`
	CompanyID  string   `json:"cid"`
	CompanyIDs []string `json:"cids,omitempty"`
	Role       string   `json:"role"`
	RoleLevel  int      `json:"rlvl"`
}

// JWTConfig holds configuration for the JWT manager.
type JWTConfig struct {
	// KeySeedPath is the path to a 32-byte raw seed file. ML-DSA-65 derives
	// both the private and public key deterministically from this seed.
	// If empty, an ephemeral key pair is generated (dev only).
	KeySeedPath        string
	Issuer             string
	AccessTokenExpiry  time.Duration
	RefreshTokenExpiry time.Duration
}

// DefaultJWTConfig returns sensible defaults.
func DefaultJWTConfig() JWTConfig {
	return JWTConfig{
		Issuer:             "eden-platform",
		AccessTokenExpiry:  15 * time.Minute,
		RefreshTokenExpiry: 7 * 24 * time.Hour,
	}
}

// JWTManager handles creation and validation of ML-DSA-65 (post-quantum) JWT tokens.
type JWTManager struct {
	privateKey *mldsa65.PrivateKey
	publicKey  *mldsa65.PublicKey
	config     JWTConfig
}

// NewJWTManager creates a JWTManager from a key seed file or auto-generates for dev.
func NewJWTManager(cfg JWTConfig) (*JWTManager, error) {
	if cfg.Issuer == "" {
		cfg.Issuer = "eden-platform"
	}
	if cfg.AccessTokenExpiry == 0 {
		cfg.AccessTokenExpiry = 15 * time.Minute
	}
	if cfg.RefreshTokenExpiry == 0 {
		cfg.RefreshTokenExpiry = 7 * 24 * time.Hour
	}

	if cfg.KeySeedPath != "" {
		pk, sk, err := loadKeySeed(cfg.KeySeedPath)
		if err == nil {
			slog.Info("loaded ML-DSA-65 key pair from seed file")
			return &JWTManager{privateKey: sk, publicKey: pk, config: cfg}, nil
		}
		slog.Warn("failed to load JWT key seed, generating ephemeral keys", "error", err)
	}

	slog.Warn("using auto-generated ML-DSA-65 key pair (dev only)")
	pk, sk, err := GenerateKeyPair()
	if err != nil {
		return nil, fmt.Errorf("generate key pair: %w", err)
	}
	return &JWTManager{privateKey: sk, publicKey: pk, config: cfg}, nil
}

// CreateAccessToken creates a signed ML-DSA-65 access token.
func (m *JWTManager) CreateAccessToken(userID, companyID, role string, roleLevel int, companyIDs []string) (string, error) {
	now := time.Now()
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Issuer:    m.config.Issuer,
			ExpiresAt: jwt.NewNumericDate(now.Add(m.config.AccessTokenExpiry)),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        generateJTI(),
		},
		UserID:     userID,
		CompanyID:  companyID,
		CompanyIDs: companyIDs,
		Role:       role,
		RoleLevel:  roleLevel,
	}
	token := jwt.NewWithClaims(signingMethodMLDSA65, claims)
	return token.SignedString(m.privateKey)
}

// CreateRefreshToken creates a signed ML-DSA-65 refresh token with minimal claims.
func (m *JWTManager) CreateRefreshToken(userID string) (string, error) {
	now := time.Now()
	claims := &jwt.RegisteredClaims{
		Subject:   userID,
		Issuer:    m.config.Issuer,
		ExpiresAt: jwt.NewNumericDate(now.Add(m.config.RefreshTokenExpiry)),
		IssuedAt:  jwt.NewNumericDate(now),
		ID:        generateJTI(),
	}
	token := jwt.NewWithClaims(signingMethodMLDSA65, claims)
	return token.SignedString(m.privateKey)
}

// ValidateAccessToken parses and validates an access token, returning the claims.
func (m *JWTManager) ValidateAccessToken(tokenStr string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		if t.Method.Alg() != "ML-DSA-65" {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.publicKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse access token: %w", err)
	}
	if !token.Valid {
		return nil, fmt.Errorf("invalid access token")
	}
	return claims, nil
}

// ValidateRefreshToken parses and validates a refresh token.
func (m *JWTManager) ValidateRefreshToken(tokenStr string) (*jwt.RegisteredClaims, error) {
	claims := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		if t.Method.Alg() != "ML-DSA-65" {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.publicKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse refresh token: %w", err)
	}
	if !token.Valid {
		return nil, fmt.Errorf("invalid refresh token")
	}
	return claims, nil
}

// CreateShortLivedToken creates a signed JWT with the given subject and expiry.
// Used for SSO state parameters, email verification, etc.
func (m *JWTManager) CreateShortLivedToken(subject string, expiry time.Duration) (string, error) {
	now := time.Now()
	claims := &jwt.RegisteredClaims{
		Subject:   subject,
		Issuer:    m.config.Issuer,
		ExpiresAt: jwt.NewNumericDate(now.Add(expiry)),
		IssuedAt:  jwt.NewNumericDate(now),
		ID:        generateJTI(),
	}
	token := jwt.NewWithClaims(signingMethodMLDSA65, claims)
	return token.SignedString(m.privateKey)
}

// ValidateShortLivedToken parses and validates a short-lived token, returning the subject.
func (m *JWTManager) ValidateShortLivedToken(tokenStr string) (string, error) {
	claims := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		if t.Method.Alg() != "ML-DSA-65" {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.publicKey, nil
	})
	if err != nil {
		return "", fmt.Errorf("parse short-lived token: %w", err)
	}
	if !token.Valid {
		return "", fmt.Errorf("invalid token")
	}
	return claims.Subject, nil
}

// GenerateKeyPair generates a new ML-DSA-65 key pair.
func GenerateKeyPair() (*mldsa65.PublicKey, *mldsa65.PrivateKey, error) {
	return mldsa65.GenerateKey(rand.Reader)
}

// GenerateKeySeed generates a 32-byte random seed suitable for ML-DSA-65 key
// derivation. Store this seed securely — it is equivalent to the private key.
func GenerateKeySeed() ([mldsa65.SeedSize]byte, error) {
	var seed [mldsa65.SeedSize]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return seed, fmt.Errorf("generate seed: %w", err)
	}
	return seed, nil
}

func generateJTI() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// loadKeySeed reads a 32-byte seed file and derives the ML-DSA-65 key pair.
func loadKeySeed(path string) (*mldsa65.PublicKey, *mldsa65.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read key seed: %w", err)
	}
	if len(data) != mldsa65.SeedSize {
		return nil, nil, fmt.Errorf("key seed must be exactly %d bytes, got %d", mldsa65.SeedSize, len(data))
	}
	var seed [mldsa65.SeedSize]byte
	copy(seed[:], data)
	pk, sk := mldsa65.NewKeyFromSeed(&seed)
	return pk, sk, nil
}
