package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"
	"os"
	"time"

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
	PrivateKeyPath     string
	PublicKeyPath      string
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

// JWTManager handles creation and validation of ES256 JWT tokens.
type JWTManager struct {
	privateKey *ecdsa.PrivateKey
	publicKey  *ecdsa.PublicKey
	config     JWTConfig
}

// NewJWTManager creates a JWTManager from PEM key file paths or auto-generates for dev.
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

	if cfg.PrivateKeyPath != "" && cfg.PublicKeyPath != "" {
		privKey, pubKey, err := loadKeyPair(cfg.PrivateKeyPath, cfg.PublicKeyPath)
		if err == nil {
			return &JWTManager{privateKey: privKey, publicKey: pubKey, config: cfg}, nil
		}
		slog.Warn("failed to load JWT key pair, generating ephemeral keys", "error", err)
	}

	slog.Warn("using auto-generated ES256 key pair (dev only)")
	privKey, err := GenerateKeyPair()
	if err != nil {
		return nil, fmt.Errorf("generate key pair: %w", err)
	}
	return &JWTManager{privateKey: privKey, publicKey: &privKey.PublicKey, config: cfg}, nil
}

// CreateAccessToken creates a signed ES256 access token.
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
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	return token.SignedString(m.privateKey)
}

// CreateRefreshToken creates a signed ES256 refresh token with minimal claims.
func (m *JWTManager) CreateRefreshToken(userID string) (string, error) {
	now := time.Now()
	claims := &jwt.RegisteredClaims{
		Subject:   userID,
		Issuer:    m.config.Issuer,
		ExpiresAt: jwt.NewNumericDate(now.Add(m.config.RefreshTokenExpiry)),
		IssuedAt:  jwt.NewNumericDate(now),
		ID:        generateJTI(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	return token.SignedString(m.privateKey)
}

// ValidateAccessToken parses and validates an access token, returning the claims.
func (m *JWTManager) ValidateAccessToken(tokenStr string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodECDSA); !ok {
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
		if _, ok := t.Method.(*jwt.SigningMethodECDSA); !ok {
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
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	return token.SignedString(m.privateKey)
}

// ValidateShortLivedToken parses and validates a short-lived token, returning the subject.
func (m *JWTManager) ValidateShortLivedToken(tokenStr string) (string, error) {
	claims := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodECDSA); !ok {
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

// GenerateKeyPair generates a new ECDSA P-256 key pair.
func GenerateKeyPair() (*ecdsa.PrivateKey, error) {
	return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
}

func generateJTI() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func loadKeyPair(privatePath, publicPath string) (*ecdsa.PrivateKey, *ecdsa.PublicKey, error) {
	privBytes, err := os.ReadFile(privatePath)
	if err != nil {
		return nil, nil, fmt.Errorf("read private key: %w", err)
	}
	privBlock, _ := pem.Decode(privBytes)
	if privBlock == nil {
		return nil, nil, fmt.Errorf("no PEM block found in private key file")
	}
	privKey, err := x509.ParseECPrivateKey(privBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse EC private key: %w", err)
	}

	pubBytes, err := os.ReadFile(publicPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read public key: %w", err)
	}
	pubBlock, _ := pem.Decode(pubBytes)
	if pubBlock == nil {
		return nil, nil, fmt.Errorf("no PEM block found in public key file")
	}
	pubIface, err := x509.ParsePKIXPublicKey(pubBlock.Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("parse public key: %w", err)
	}
	pubKey, ok := pubIface.(*ecdsa.PublicKey)
	if !ok {
		return nil, nil, fmt.Errorf("public key is not ECDSA")
	}
	return privKey, pubKey, nil
}
