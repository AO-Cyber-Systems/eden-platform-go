// Package clients owns the AO ID OIDC client registry — the set of
// relying parties allowed to perform authorization-code + refresh
// exchanges with the issuer.
//
// During Phase A of objective 30 the registry is in-memory and seeded at
// boot from config (the AODex pilot client is the only entry). The
// pgstore-backed registry lands in Obj 31 alongside the admin surface
// for client CRUD; the interface here is shaped so that swap is a one-
// line change in composition.go.
package clients

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// AODexClientID is the conventional client_id for the pilot AODex
// integration. Kept as an exported constant so AODex's own config can
// reference it without copy-pasting a magic string.
const AODexClientID = "aodex-pilot"

// ClientType differentiates confidential clients (which can keep a
// secret) from public clients (browser/mobile apps that authenticate via
// PKCE only).
type ClientType string

const (
	ClientTypeConfidential ClientType = "confidential"
	ClientTypePublic       ClientType = "public"
)

// Client represents a registered OIDC client. SecretHash is the
// sha256-hex of the client_secret; the plaintext is never stored.
type Client struct {
	ID            string
	SecretHash    string
	Name          string
	Type          ClientType
	RedirectURIs  []string
	AllowedScopes []string
	AllowedGrants []string
	AuthMethod    string // "client_secret_basic" | "client_secret_post" | "none"
	CreatedAt     time.Time
}

// HasScope reports whether s is in c.AllowedScopes.
func (c *Client) HasScope(s string) bool {
	for _, allowed := range c.AllowedScopes {
		if allowed == s {
			return true
		}
	}
	return false
}

// HasGrant reports whether g is in c.AllowedGrants.
func (c *Client) HasGrant(g string) bool {
	for _, allowed := range c.AllowedGrants {
		if allowed == g {
			return true
		}
	}
	return false
}

// Errors returned by the registry.
var (
	ErrClientNotFound  = errors.New("client not found")
	ErrDuplicateClient = errors.New("client already registered")
	ErrInvalidSecret   = errors.New("invalid client secret")
	ErrInvalidRedirect = errors.New("redirect_uri does not match a registered URI")
)

// Registry is the read/write surface for OIDC clients. The in-memory
// implementation suffices for Phase A; pgstore is dropped in later.
type Registry interface {
	Lookup(ctx context.Context, clientID string) (*Client, error)
	Register(ctx context.Context, client Client) error
	Authenticate(ctx context.Context, clientID, secret string) (*Client, error)
	ValidateRedirect(client *Client, redirectURI string) error
}

// MemoryRegistry is a thread-safe in-memory Registry. Callers should
// share a single instance across the issuer; per-request copies break
// state. The zero value is unsafe — always use NewMemoryRegistry.
type MemoryRegistry struct {
	mu      sync.RWMutex
	clients map[string]*Client
}

// NewMemoryRegistry constructs an empty MemoryRegistry.
func NewMemoryRegistry() *MemoryRegistry {
	return &MemoryRegistry{clients: make(map[string]*Client)}
}

// Lookup returns the client by its public ID. ErrClientNotFound when
// the ID is unknown.
func (r *MemoryRegistry) Lookup(_ context.Context, clientID string) (*Client, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.clients[clientID]
	if !ok {
		return nil, ErrClientNotFound
	}
	// Return a copy so callers can't mutate the stored entry.
	cp := *c
	cp.RedirectURIs = append([]string(nil), c.RedirectURIs...)
	cp.AllowedScopes = append([]string(nil), c.AllowedScopes...)
	cp.AllowedGrants = append([]string(nil), c.AllowedGrants...)
	return &cp, nil
}

// Register inserts a client. ErrDuplicateClient if the ID already
// exists. The client's CreatedAt is stamped if zero.
func (r *MemoryRegistry) Register(_ context.Context, client Client) error {
	if client.ID == "" {
		return fmt.Errorf("clients: client ID required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.clients[client.ID]; exists {
		return ErrDuplicateClient
	}
	if client.CreatedAt.IsZero() {
		client.CreatedAt = time.Now().UTC()
	}
	c := client
	c.RedirectURIs = append([]string(nil), client.RedirectURIs...)
	c.AllowedScopes = append([]string(nil), client.AllowedScopes...)
	c.AllowedGrants = append([]string(nil), client.AllowedGrants...)
	r.clients[client.ID] = &c
	return nil
}

// Authenticate verifies the client_id + client_secret combo using
// constant-time comparison on the SHA-256 hash of the supplied secret.
//
// Public clients (no SecretHash) are rejected here; the token endpoint
// must skip Authenticate entirely for `none` auth methods.
func (r *MemoryRegistry) Authenticate(ctx context.Context, clientID, secret string) (*Client, error) {
	c, err := r.Lookup(ctx, clientID)
	if err != nil {
		return nil, err
	}
	if c.SecretHash == "" {
		return nil, ErrInvalidSecret
	}
	want, err := hex.DecodeString(c.SecretHash)
	if err != nil {
		return nil, ErrInvalidSecret
	}
	got := sha256.Sum256([]byte(secret))
	if subtle.ConstantTimeCompare(want, got[:]) != 1 {
		return nil, ErrInvalidSecret
	}
	return c, nil
}

// ValidateRedirect performs an exact-string match against the client's
// registered redirect URIs. Per OIDC, partial / scheme-only / path-only
// matching is forbidden; an attacker who can register a redirect MUST
// only redirect through a URI the legitimate operator approved.
func (r *MemoryRegistry) ValidateRedirect(client *Client, redirectURI string) error {
	if client == nil {
		return ErrClientNotFound
	}
	for _, ru := range client.RedirectURIs {
		if ru == redirectURI {
			return nil
		}
	}
	return ErrInvalidRedirect
}

// HashSecret returns the hex-encoded SHA-256 of the secret. Useful for
// callers (e.g. seed scripts) that need to feed a SecretHash directly.
func HashSecret(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(h[:])
}

// SeedAODex registers the AODex pilot client with conventional dev
// settings. Returns nil if AODex is already registered (idempotent).
func SeedAODex(ctx context.Context, reg Registry, clientSecret string, redirectURIs []string) error {
	if reg == nil {
		return fmt.Errorf("clients: SeedAODex: nil registry")
	}
	if clientSecret == "" {
		return fmt.Errorf("clients: SeedAODex: empty client secret")
	}
	if len(redirectURIs) == 0 {
		return fmt.Errorf("clients: SeedAODex: redirect URIs required")
	}
	err := reg.Register(ctx, Client{
		ID:            AODexClientID,
		SecretHash:    HashSecret(clientSecret),
		Name:          "AODex (pilot)",
		Type:          ClientTypeConfidential,
		RedirectURIs:  redirectURIs,
		AllowedScopes: []string{"openid", "profile", "email", "offline_access"},
		AllowedGrants: []string{"authorization_code", "refresh_token"},
		AuthMethod:    "client_secret_basic",
	})
	if errors.Is(err, ErrDuplicateClient) {
		return nil
	}
	return err
}
