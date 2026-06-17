// Package social implements user-scoped consumer social login (Google, Apple,
// Microsoft, Facebook, X). It is deliberately separate from the company-scoped
// SSOService: there is NO company_id anywhere in this package. On success it
// issues the same ML-DSA-65 access+refresh token pair as password login,
// reusing the platform JWTManager + refresh_tokens rotation.
package social

import (
	"encoding/json"

	"github.com/aocybersystems/eden-platform-go/platform/auth"
)

// SocialStore is re-exported from the auth package so consumers of this
// package can refer to the user-scoped identity store without importing
// auth directly. It defines the user_identities operations.
type SocialStore = auth.SocialStore

// UserIdentity is re-exported from the auth package — the stored social
// identity domain type (user-scoped; no company_id).
type UserIdentity = auth.UserIdentity

// Identity is the provider-agnostic input the provision step consumes. The
// OIDC (09-02) and custom-OAuth2 (09-03) provider TRDs construct an Identity
// from each provider's verified claims and call Provision.
//
// EmailVerified codifies the account-linking RULE: an account may be linked to
// an existing user by email ONLY when the provider has verified the email.
// Per-provider semantics (Google email_verified, Apple always-true, Microsoft
// none, Facebook none, X no email) are SET by the provider TRDs.
//
// RawClaims holds the provider's identity claims for audit — callers MUST strip
// provider access/refresh tokens before populating it.
type Identity struct {
	Provider      string // "google"|"apple"|"microsoft"|"facebook"|"x"
	ProviderSub   string // provider's stable user identifier (sub/id)
	Email         string // may be "" (X always; Facebook when declined)
	EmailVerified bool   // did the provider verify the email
	DisplayName   string
	AvatarURL     string
	RawClaims     json.RawMessage
}
