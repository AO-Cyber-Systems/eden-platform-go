// Package apikey provides API key authentication primitives: bcrypt-hashed
// keys with prefix lookup, scope storage as JSON, and constant-time match.
//
// Promoted from aodex-go/internal/auth/apikey.go. The donor coupled directly
// to *pgxpool.Pool; this package introduces a small Store interface so
// callers can plug in any storage backend (sqlc, pgxpool, in-memory tests).
package apikey

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// DefaultPrefix is the default API key prefix. Override via Authenticator.
const DefaultPrefix = "aoc_"

// PrefixLookupLength is the number of leading characters used for prefix
// lookups in the database.
const PrefixLookupLength = 8

var (
	// ErrInvalidKeyFormat indicates the key doesn't have the required prefix
	// or is too short to extract a lookup prefix.
	ErrInvalidKeyFormat = errors.New("invalid API key format")

	// ErrKeyNotFound indicates no matching active key was found.
	ErrKeyNotFound = errors.New("API key not found")
)

// AuthenticatedKey contains the user info resolved from a successful API
// key authentication.
type AuthenticatedKey struct {
	UserID string
	KeyID  string
	Scopes []string
}

// Candidate represents a single active API-key row returned by Store.
// KeyDigest is a bcrypt hash of the raw key (entire key, not just suffix).
type Candidate struct {
	ID        string
	KeyDigest string
	UserID    string
	Scopes    []string
}

// Store is the storage abstraction the Authenticator depends on. Consumers
// implement this with their sqlc-generated code or any other backend.
type Store interface {
	// LookupByPrefix returns all currently-active candidates whose
	// key_prefix column equals the supplied 8-char prefix. The store is
	// responsible for filtering by is_active=true and (expires_at IS NULL
	// OR expires_at > now).
	LookupByPrefix(ctx context.Context, prefix string) ([]Candidate, error)

	// MarkUsed updates last_used_at on the matching key row. Best-effort —
	// errors should be logged by the implementation but not propagated.
	MarkUsed(ctx context.Context, keyID string, at time.Time) error
}

// Authenticator validates raw API keys against a Store using bcrypt.
type Authenticator struct {
	Store  Store
	Prefix string // optional; defaults to DefaultPrefix
}

// New builds an Authenticator with the default prefix.
func New(store Store) *Authenticator {
	return &Authenticator{Store: store, Prefix: DefaultPrefix}
}

// Authenticate validates a raw API key string. It extracts the lookup
// prefix, asks the Store for active candidates, then bcrypt-verifies the
// full raw key against each candidate's KeyDigest. On success it best-
// effort updates last-used-at and returns the authenticated identity.
func (a *Authenticator) Authenticate(ctx context.Context, rawKey string) (*AuthenticatedKey, error) {
	prefix := a.Prefix
	if prefix == "" {
		prefix = DefaultPrefix
	}
	if !strings.HasPrefix(rawKey, prefix) {
		return nil, ErrInvalidKeyFormat
	}
	if len(rawKey) < PrefixLookupLength {
		return nil, ErrInvalidKeyFormat
	}
	lookup := rawKey[:PrefixLookupLength]

	candidates, err := a.Store.LookupByPrefix(ctx, lookup)
	if err != nil {
		return nil, fmt.Errorf("looking up API keys: %w", err)
	}
	if len(candidates) == 0 {
		return nil, ErrKeyNotFound
	}

	for _, c := range candidates {
		if err := bcrypt.CompareHashAndPassword([]byte(c.KeyDigest), []byte(rawKey)); err == nil {
			_ = a.Store.MarkUsed(ctx, c.ID, time.Now())
			return &AuthenticatedKey{
				UserID: c.UserID,
				KeyID:  c.ID,
				Scopes: c.Scopes,
			}, nil
		}
	}
	return nil, ErrKeyNotFound
}

// ExtractFromHeaders returns the API key extracted from the standard
// header pattern: X-API-Key first, then "Authorization: Bearer <key>" when
// the bearer token starts with the configured prefix, then a query string
// "api_key" parameter as last resort. Returns the empty string when no key
// is found.
func ExtractFromHeaders(prefix, xAPIKey, authHeader, queryParam string) string {
	if prefix == "" {
		prefix = DefaultPrefix
	}
	if xAPIKey != "" {
		return xAPIKey
	}
	if strings.HasPrefix(authHeader, "Bearer ") {
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if strings.HasPrefix(token, prefix) {
			return token
		}
	}
	return queryParam
}

// HashKey bcrypt-hashes a raw API key for persistence. The resulting digest
// is the value the Store should return as Candidate.KeyDigest.
func HashKey(rawKey string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(rawKey), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hashing API key: %w", err)
	}
	return string(hash), nil
}

// GenerateKey returns a new random API key with the given prefix. The
// returned key has the form "<prefix><32 hex chars>" — compatible with the
// donor's "aodex_..." pattern when prefix == "aodex_". Callers persist
// HashKey(key) and the first PrefixLookupLength characters of key.
func GenerateKey(prefix string, randomBytes []byte) string {
	if prefix == "" {
		prefix = DefaultPrefix
	}
	return prefix + hex.EncodeToString(randomBytes)
}
