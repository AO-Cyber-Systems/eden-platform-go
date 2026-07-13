package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/cloudflare/circl/sign/mldsa/mldsa65"
	"github.com/golang-jwt/jwt/v5"
)

// Claims represents the JWT claims included in access tokens.
//
// The Claims struct hosts two orthogonal identity axes:
//
//  1. B2B axis (UserID + CompanyID/CompanyIDs + Role + RoleLevel): used by
//     AODex / eden-biz / AOSentry-style consumers where principals belong to
//     companies and have role-based access levels.
//  2. Household axis (HouseholdID + ChildID + ChildMode): used by AOFamily /
//     Eden Family-style consumers where principals belong to households and
//     parental controls gate restricted-mode sessions.
//
// Household fields are tagged `omitempty` so B2B tokens carry an identical
// wire format to the pre-24a era — adding them did NOT break any existing
// consumer. Populate them only via JWTManager.CreateHouseholdAccessToken.
type Claims struct {
	jwt.RegisteredClaims
	UserID     string   `json:"uid"`
	CompanyID  string   `json:"cid"`
	CompanyIDs []string `json:"cids,omitempty"`
	Role       string   `json:"role"`
	RoleLevel  int      `json:"rlvl"`

	// Scopes lists capability scopes carried by this token. Used by per-product
	// consumers to gate route access along an axis orthogonal to Role. The
	// first consumer is politihub (ADR-0003 — "volunteer:field" gates /v1/me/*
	// and is rejected by staff endpoints when it is the SOLE scope; politihub
	// combines OnlyHasScope("volunteer:field") with claims.Role == "" to
	// distinguish production Navigators tokens from dual-axis dev tokens).
	//
	// omitempty preserves wire-format compatibility — staff issuance paths
	// (CreateAccessToken) leave Scopes nil, so existing tokens carry no
	// "scopes" key.
	Scopes []string `json:"scopes,omitempty"`

	// Household-aware claims (Obj 24a). All optional so B2B consumers see
	// no wire-format change. Populated only via CreateHouseholdAccessToken
	// when a household-shaped product (AOFamily, Eden Family) issues a
	// token. HouseholdID links to a row in platform/household;
	// ChildID/ChildMode together control parental-control middleware
	// enforcement via RequireParentMode / RequireChildMode.
	HouseholdID string `json:"hid,omitempty"`
	ChildID     string `json:"child_id,omitempty"`
	ChildMode   bool   `json:"child_mode,omitempty"`

	// Entitlements (`ent`) carries the plan/entitlement scope resolved at
	// issuance by the token minter (AOID under AOFamily Path B). Wire-identical
	// to AOID's `ent` tag so household-product backends unmarshal it directly
	// via idt.Claims(&claims) with no per-backend decode change — the same
	// capture-by-tag mechanism that already picks up hid/child_mode. omitempty
	// keeps B2B/tnt-only tokens byte-compatible. eden is a passive carrier: the
	// plan→entitlement policy lives in the issuing product's billing service,
	// never here (see HasEntitlement / RequirePlan in entitlements.go).
	Entitlements []string `json:"ent,omitempty"`
}

// HasScope returns true if the token's scope list contains the given scope.
// Pure scope-axis — does NOT inspect Role/RoleLevel. Callers that need a
// dual-axis check must combine HasScope with their own Role check.
func (c *Claims) HasScope(scope string) bool {
	for _, s := range c.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// OnlyHasScope returns true iff the token's scope list is exactly [scope]
// (length 1, single value). Pure scope-axis — does NOT inspect Role/RoleLevel.
// Callers needing a "production-Navigators token" check combine OnlyHasScope
// with their own Role check (e.g. politihub: OnlyHasScope("volunteer:field")
// && claims.Role == "").
//
// This contract is locked: a dual-axis token (staff Role + volunteer:field
// scope) returns true from OnlyHasScope("volunteer:field") because the
// scope-list is still exactly one value. The Role check belongs to the
// caller's middleware, where the staff/volunteer model is known.
func (c *Claims) OnlyHasScope(scope string) bool {
	return len(c.Scopes) == 1 && c.Scopes[0] == scope
}

// KeyEntry holds an ML-DSA-65 key pair identified by a kid (Key ID).
type KeyEntry struct {
	PrivateKey *mldsa65.PrivateKey
	PublicKey  *mldsa65.PublicKey
}

// JWTConfig holds configuration for the JWT manager.
type JWTConfig struct {
	// KeySeedPath is the path to a 32-byte raw seed file. ML-DSA-65 derives
	// both the private and public key deterministically from this seed.
	// If empty, an ephemeral key pair is generated (dev only).
	// Single-key backward-compat mode — used when KeySeedPaths is empty.
	KeySeedPath string

	// KeySeedPaths is an optional map of kid -> seed file path for multi-key
	// rotation support. When non-empty, KeySeedPath is ignored.
	KeySeedPaths map[string]string

	// ActiveKID specifies which kid from KeySeedPaths to use when signing new
	// tokens. Required when KeySeedPaths is non-empty.
	ActiveKID string

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
// It supports multiple signing keys identified by kid (Key ID) headers for zero-downtime
// key rotation.
type JWTManager struct {
	keys      map[string]*KeyEntry // kid -> key pair
	activeKID string               // kid used for signing new tokens
	config    JWTConfig
}

// NewJWTManager creates a JWTManager from key seed files or auto-generates for dev.
//
// Priority:
//  1. KeySeedPaths (multi-key mode) — loads each (kid, path) pair, uses ActiveKID for signing.
//  2. KeySeedPath (single-key backward-compat) — loads one key with kid "default".
//  3. No paths — generates an ephemeral key pair with kid "ephemeral" (dev only).
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

	m := &JWTManager{
		keys:   make(map[string]*KeyEntry),
		config: cfg,
	}

	// Multi-key mode: KeySeedPaths takes priority.
	if len(cfg.KeySeedPaths) > 0 {
		for kid, path := range cfg.KeySeedPaths {
			pk, sk, err := loadKeySeed(path)
			if err != nil {
				return nil, fmt.Errorf("load key seed for kid %q: %w", kid, err)
			}
			m.keys[kid] = &KeyEntry{PrivateKey: sk, PublicKey: pk}
		}
		if cfg.ActiveKID == "" {
			return nil, fmt.Errorf("ActiveKID must be set when KeySeedPaths is non-empty")
		}
		if _, ok := m.keys[cfg.ActiveKID]; !ok {
			return nil, fmt.Errorf("ActiveKID %q not found in KeySeedPaths", cfg.ActiveKID)
		}
		m.activeKID = cfg.ActiveKID
		slog.Info("loaded ML-DSA-65 key pairs from seed files", "count", len(m.keys), "active_kid", m.activeKID)
		return m, nil
	}

	// Single-key backward-compat mode.
	if cfg.KeySeedPath != "" {
		pk, sk, err := loadKeySeed(cfg.KeySeedPath)
		if err == nil {
			slog.Info("loaded ML-DSA-65 key pair from seed file")
			m.keys["default"] = &KeyEntry{PrivateKey: sk, PublicKey: pk}
			m.activeKID = "default"
			return m, nil
		}
		slog.Warn("failed to load JWT key seed, generating ephemeral keys", "error", err)
	}

	// Ephemeral fallback (dev only).
	slog.Warn("using auto-generated ML-DSA-65 key pair (dev only)")
	pk, sk, err := GenerateKeyPair()
	if err != nil {
		return nil, fmt.Errorf("generate key pair: %w", err)
	}
	m.keys["ephemeral"] = &KeyEntry{PrivateKey: sk, PublicKey: pk}
	m.activeKID = "ephemeral"
	return m, nil
}

// keyfunc returns a jwt.Keyfunc that selects the correct public key by kid.
// Tokens without a kid header fall back to the active key for backward compatibility.
func (m *JWTManager) keyfunc(t *jwt.Token) (interface{}, error) {
	if t.Method.Alg() != "ML-DSA-65" {
		return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
	}
	kid, ok := t.Header["kid"].(string)
	if !ok || kid == "" {
		// Backward compat: tokens issued before kid support fall back to active key.
		return m.keys[m.activeKID].PublicKey, nil
	}
	entry, ok := m.keys[kid]
	if !ok {
		return nil, fmt.Errorf("unknown kid: %s", kid)
	}
	return entry.PublicKey, nil
}

// CreateAccessTokenWithScopes is CreateAccessToken with an explicit scopes
// parameter. Used by issuance flows (e.g. SignInNavigators) that need to set
// per-product capability scopes alongside the standard B2B claims.
//
// Backward compat: the existing CreateAccessToken is preserved as a thin
// delegate that passes scopes=nil. Tokens issued via CreateAccessToken are
// wire-format identical to pre-Scopes-field behavior because the
// `omitempty` tag drops nil/empty slices from the JSON payload.
func (m *JWTManager) CreateAccessTokenWithScopes(
	userID, companyID, role string,
	roleLevel int,
	companyIDs, scopes []string,
) (string, error) {
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
		Scopes:     scopes,
	}
	token := jwt.NewWithClaims(signingMethodMLDSA65, claims)
	token.Header["kid"] = m.activeKID
	return token.SignedString(m.keys[m.activeKID].PrivateKey)
}

// CreateAccessToken creates a signed ML-DSA-65 access token with a kid header.
//
// This signature is preserved for backward compatibility — all existing
// staff issuance callers (auth.Service.Login, auth.Service.SignUp,
// sso.go) keep compiling. The body delegates to CreateAccessTokenWithScopes
// with scopes=nil; omitempty drops the field from the wire format.
func (m *JWTManager) CreateAccessToken(userID, companyID, role string, roleLevel int, companyIDs []string) (string, error) {
	return m.CreateAccessTokenWithScopes(userID, companyID, role, roleLevel, companyIDs, nil)
}

// CreateHouseholdAccessToken creates a signed ML-DSA-65 access token scoped
// to a household. childID and childMode are optional — pass "" / false for
// a parent-mode token.
//
// B2B claims (CompanyID, Role, RoleLevel, CompanyIDs) are left zero-valued
// — they are orthogonal axes. Consumers that need to mix the two axes (rare)
// can construct a *Claims directly and sign via the lower-level jwt library,
// but the supported path is one or the other.
//
// Use this method from AOFamily-AI / AOFamily-Browser / AOFamily-Connect /
// Eden Family backends when issuing tokens for a family session. Use
// CreateAccessToken for the existing B2B issuance path.
func (m *JWTManager) CreateHouseholdAccessToken(userID, householdID, childID string, childMode bool) (string, error) {
	now := time.Now()
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Issuer:    m.config.Issuer,
			ExpiresAt: jwt.NewNumericDate(now.Add(m.config.AccessTokenExpiry)),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        generateJTI(),
		},
		UserID:      userID,
		HouseholdID: householdID,
		ChildID:     childID,
		ChildMode:   childMode,
	}
	token := jwt.NewWithClaims(signingMethodMLDSA65, claims)
	token.Header["kid"] = m.activeKID
	return token.SignedString(m.keys[m.activeKID].PrivateKey)
}

// CreateRefreshToken creates a signed ML-DSA-65 refresh token with a kid header.
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
	token.Header["kid"] = m.activeKID
	return token.SignedString(m.keys[m.activeKID].PrivateKey)
}

// ValidateAccessToken parses and validates an access token, returning the claims.
func (m *JWTManager) ValidateAccessToken(tokenStr string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, m.keyfunc)
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
	token, err := jwt.ParseWithClaims(tokenStr, claims, m.keyfunc)
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
	token.Header["kid"] = m.activeKID
	return token.SignedString(m.keys[m.activeKID].PrivateKey)
}

// ValidateShortLivedToken parses and validates a short-lived token, returning the subject.
func (m *JWTManager) ValidateShortLivedToken(tokenStr string) (string, error) {
	claims := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, m.keyfunc)
	if err != nil {
		return "", fmt.Errorf("parse short-lived token: %w", err)
	}
	if !token.Valid {
		return "", fmt.Errorf("invalid token")
	}
	return claims.Subject, nil
}

// stateHMACKey derives a stable 32-byte HMAC key from the active ML-DSA-65
// signing key. It exists so short-lived, SELF-VERIFIED tokens (SSO state, email
// verification) can carry a COMPACT HS256 signature (~32 bytes) instead of the
// ~3.3KB ML-DSA-65 one. That size matters when an upstream party embeds the
// token somewhere bounded — e.g. AO ID stuffs the SSO `state` into a cookie
// whose TOTAL must stay under the browser's ~4KB per-cookie limit; an ML-DSA
// state overflows it and the browser silently drops the cookie.
//
// Derivation is deterministic from the active key material, so every replica
// computes the same key with NO extra config/secret. Domain-separated so it can
// never collide with any other use of the key bytes.
func (m *JWTManager) stateHMACKey() ([]byte, error) {
	entry, ok := m.keys[m.activeKID]
	if !ok || entry.PrivateKey == nil {
		return nil, fmt.Errorf("no active signing key")
	}
	h := sha256.New()
	h.Write([]byte("eden-platform/short-lived-hs256/v1\x00"))
	h.Write(entry.PrivateKey.Bytes())
	return h.Sum(nil), nil
}

// CreateCompactShortLivedToken mints a short-lived token like
// CreateShortLivedToken but with a compact HS256 signature (see stateHMACKey),
// so the token stays small. Use ONLY for tokens this same service both signs and
// verifies (SSO state, email/verify links) — NEVER for tokens verified by other
// parties through the ML-DSA JWKS.
func (m *JWTManager) CreateCompactShortLivedToken(subject string, expiry time.Duration) (string, error) {
	key, err := m.stateHMACKey()
	if err != nil {
		return "", err
	}
	now := time.Now()
	claims := &jwt.RegisteredClaims{
		Subject:   subject,
		Issuer:    m.config.Issuer,
		ExpiresAt: jwt.NewNumericDate(now.Add(expiry)),
		IssuedAt:  jwt.NewNumericDate(now),
		ID:        generateJTI(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(key)
}

// ValidateCompactShortLivedToken parses and validates a token minted by
// CreateCompactShortLivedToken, returning the subject. Rejects any non-HS256 alg
// (defence against alg-confusion with the ML-DSA path).
func (m *JWTManager) ValidateCompactShortLivedToken(tokenStr string) (string, error) {
	key, err := m.stateHMACKey()
	if err != nil {
		return "", err
	}
	claims := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return key, nil
	})
	if err != nil {
		return "", fmt.Errorf("parse compact short-lived token: %w", err)
	}
	if !token.Valid {
		return "", fmt.Errorf("invalid token")
	}
	return claims.Subject, nil
}

// PublicKeys returns a snapshot of the kid → public-key map. The returned
// map is a fresh copy so callers can iterate it without holding internal
// JWTManager state. Used by the AO ID JWKS endpoint to enumerate keys
// for federation.
func (m *JWTManager) PublicKeys() map[string]*mldsa65.PublicKey {
	out := make(map[string]*mldsa65.PublicKey, len(m.keys))
	for kid, entry := range m.keys {
		out[kid] = entry.PublicKey
	}
	return out
}

// ActiveKID returns the kid currently used for signing new tokens. JWKS
// consumers may flag this kid as the "preferred" verification key.
func (m *JWTManager) ActiveKID() string {
	return m.activeKID
}

// ActivePrivateKey returns the private key currently used for signing
// new tokens. Used by the AO ID issuer to sign custom claim sets (ID
// tokens) without re-implementing the mldsa key plumbing. Returns nil
// when no active key is configured.
func (m *JWTManager) ActivePrivateKey() *mldsa65.PrivateKey {
	entry, ok := m.keys[m.activeKID]
	if !ok {
		return nil
	}
	return entry.PrivateKey
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
