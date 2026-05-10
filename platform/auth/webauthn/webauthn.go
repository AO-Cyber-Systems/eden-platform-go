// Package webauthn wraps the go-webauthn/webauthn library with a small
// platform-friendly surface: a Config struct, a constructor, and a User type
// that implements webauthn.User for registration and login ceremonies.
//
// Promoted from aodex-go/internal/auth/webauthn.go. The donor relied on
// aodex-go/internal/config for RP settings; this package takes an explicit
// Config so it can be reused across AODex, eden-biz, AO ID, and AOSentry.
package webauthn

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"

	gowebauthn "github.com/go-webauthn/webauthn/webauthn"
)

// randSource is the random source used by GenerateUserID. Tests may replace
// it with a deterministic reader.
var randSource io.Reader = rand.Reader

// Config holds the Relying Party settings for WebAuthn ceremonies.
type Config struct {
	// RPDisplayName is the human-readable RP name shown in browser UI.
	RPDisplayName string

	// RPID is the registrable domain (e.g. "aodex.ai"). It must match the
	// origin's effective domain.
	RPID string

	// RPOrigins is the list of allowed origins (scheme + host[:port]).
	RPOrigins []string
}

// New creates a configured *webauthn.WebAuthn for registration/auth ceremonies.
func New(cfg Config) (*gowebauthn.WebAuthn, error) {
	displayName := cfg.RPDisplayName
	if displayName == "" {
		displayName = "AOC Platform"
	}
	w, err := gowebauthn.New(&gowebauthn.Config{
		RPDisplayName: displayName,
		RPID:          cfg.RPID,
		RPOrigins:     cfg.RPOrigins,
	})
	if err != nil {
		return nil, fmt.Errorf("webauthn.New: %w", err)
	}
	return w, nil
}

// User implements webauthn.User for registration/login ceremonies. It wraps
// a stable user handle (the WebAuthn user ID, NOT the database UUID), the
// account's email (used as the WebAuthn name), a display name, and any
// previously-registered credentials.
type User struct {
	id          []byte
	name        string
	displayName string
	credentials []gowebauthn.Credential
}

// NewUser creates a User from the persisted webauthn_id (base64url-encoded),
// the account's email, display name, and the user's existing credentials
// (may be nil).
func NewUser(webauthnID, email, displayName string, credentials []gowebauthn.Credential) *User {
	idBytes, err := base64.RawURLEncoding.DecodeString(webauthnID)
	if err != nil {
		// Fall back to raw bytes — preserves donor behavior on legacy data.
		idBytes = []byte(webauthnID)
	}
	if credentials == nil {
		credentials = []gowebauthn.Credential{}
	}
	return &User{
		id:          idBytes,
		name:        email,
		displayName: displayName,
		credentials: credentials,
	}
}

// WebAuthnID returns the user handle (opaque byte sequence).
func (u *User) WebAuthnID() []byte { return u.id }

// WebAuthnName returns the user's email for display during registration.
func (u *User) WebAuthnName() string { return u.name }

// WebAuthnDisplayName returns the user's display name.
func (u *User) WebAuthnDisplayName() string { return u.displayName }

// WebAuthnCredentials returns the user's existing credentials. Used by
// BeginRegistration for the exclude list and by BeginLogin for the allowed
// credentials list.
func (u *User) WebAuthnCredentials() []gowebauthn.Credential { return u.credentials }

// GenerateUserID creates a new random WebAuthn user handle suitable for
// persistence. Returns a base64url-encoded string.
func GenerateUserID() string {
	id := make([]byte, 64)
	if _, err := io.ReadFull(randSource, id); err != nil {
		panic(fmt.Sprintf("webauthn: failed to generate random ID: %v", err))
	}
	return base64.RawURLEncoding.EncodeToString(id)
}
