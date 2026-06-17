package social

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/aocybersystems/eden-platform-go/platform/auth"
	"github.com/aocybersystems/eden-platform-go/platform/auth/oidc"
)

// oidcPresets maps a consumer OIDC provider to its discovery issuer. Microsoft
// uses the multi-tenant /common endpoint so personal (MSA) accounts are
// accepted (Pitfall 7); Google uses its standard issuer.
var oidcPresets = map[string]string{
	"google":    "https://accounts.google.com",
	"microsoft": "https://login.microsoftonline.com/common/v2.0",
}

// oidcScopes are the scopes requested for both Google and Microsoft. "openid"
// yields the id_token; "email" + "profile" populate the Identity.
var oidcScopes = []string{"openid", "email", "profile"}

// RegisterOIDCProvider registers a Google/Microsoft OIDC provider in the
// registry with its client credentials. The issuer is resolved from the preset
// table; callers supply only the client_id/secret (read from env in main.go).
// Registering an unknown provider is a no-op-with-config: the entry is keyed but
// InitiateOIDC still validates the preset issuer exists.
func (s *SocialAuthService) RegisterOIDCProvider(provider, clientID, clientSecret string) {
	s.providers[provider] = ProviderConfig{
		Provider:     provider,
		IssuerURL:    oidcPresets[provider],
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Scopes:       oidcScopes,
	}
}

// oidcConfig builds the oidc.Config for a registered provider. The RedirectURI
// is ALWAYS the server's own callback (baseURL + /auth/social/callback) — the
// app deep-link travels separately in the state JWT.
func (s *SocialAuthService) oidcConfig(provider string) (oidc.Config, error) {
	pc, ok := s.providers[provider]
	if !ok {
		return oidc.Config{}, fmt.Errorf("provider %q not registered", provider)
	}
	issuer := oidcPresets[provider]
	if issuer == "" {
		return oidc.Config{}, fmt.Errorf("provider %q is not an OIDC provider", provider)
	}
	return oidc.Config{
		Issuer:       issuer,
		ClientID:     pc.ClientID,
		ClientSecret: pc.ClientSecret,
		RedirectURI:  s.baseURL + "/auth/social/callback",
		Scopes:       oidcScopes,
	}, nil
}

// InitiateOIDC starts a Google/Microsoft OIDC authorization-code flow. It
// validates the provider and redirect_uri (allowlist — Pitfall 4), then builds
// the provider authorization URL with a signed state JWT carrying the
// provider, the app redirect_uri, and a fresh nonce.
func (s *SocialAuthService) InitiateOIDC(ctx context.Context, provider, redirectURI string) (authURL, state string, err error) {
	if _, ok := oidcPresets[provider]; !ok {
		return "", "", fmt.Errorf("unknown OIDC provider %q", provider)
	}
	if !s.isAllowedRedirectURI(redirectURI) {
		return "", "", fmt.Errorf("redirect_uri not allowed")
	}

	cfg, err := s.oidcConfig(provider)
	if err != nil {
		return "", "", err
	}

	nonce, err := randomNonce()
	if err != nil {
		return "", "", fmt.Errorf("generate nonce: %w", err)
	}
	state, err = s.createStateJWT(provider, redirectURI, "", nonce)
	if err != nil {
		return "", "", fmt.Errorf("create state: %w", err)
	}

	authURL, err = oidc.BuildAuthURL(ctx, cfg, state, nonce)
	if err != nil {
		return "", "", fmt.Errorf("build auth URL: %w", err)
	}
	return authURL, state, nil
}

// HandleCallback completes a social-login flow for ALL five providers: it parses
// the state JWT, re-checks the redirect allowlist (defense-in-depth — Pitfall 4),
// then dispatches by provider. OIDC providers (google/microsoft) exchange the
// code and verify the id_token via JWKS; the custom providers (apple/facebook/x)
// run their own exchange — Apple verifies its id_token against Apple's JWKS, X
// completes its PKCE exchange with the verifier carried in the state JWT, and
// Facebook calls the Graph userinfo endpoint. The resulting Identity flows through
// the shared Provision pipeline. formUserField carries Apple's one-time `user`
// (name) form POST; it is "" for every other provider. It returns the issued
// token pair plus the app redirect_uri for the HTTP callback to use.
func (s *SocialAuthService) HandleCallback(ctx context.Context, code, stateJWT, formUserField string) (*auth.AuthResponse, string, error) {
	provider, redirectURI, pkceVerifier, _, err := s.parseStateJWT(stateJWT)
	if err != nil {
		return nil, "", fmt.Errorf("parse state: %w", err)
	}
	// Re-validate BEFORE any token exchange so a forged/expired allowlist never
	// leaks tokens to an attacker-controlled redirect.
	if !s.isAllowedRedirectURI(redirectURI) {
		return nil, "", fmt.Errorf("redirect_uri not allowed")
	}

	var identity Identity
	switch {
	case oidcPresets[provider] != "":
		cfg, err := s.oidcConfig(provider)
		if err != nil {
			return nil, "", err
		}
		tokens, err := oidc.ExchangeCode(ctx, cfg, code)
		if err != nil {
			return nil, "", fmt.Errorf("exchange code: %w", err)
		}
		claims, err := oidc.VerifyIDToken(ctx, cfg, tokens.IDToken)
		if err != nil {
			return nil, "", fmt.Errorf("verify id_token: %w", err)
		}
		identity = claimsToIdentity(provider, claims)

	case provider == "x":
		identity, err = s.handleXCallback(ctx, code, pkceVerifier)
		if err != nil {
			return nil, "", err
		}

	case customProviders[provider]:
		identity, err = s.handleCustomCallback(ctx, provider, code, formUserField)
		if err != nil {
			return nil, "", err
		}

	default:
		return nil, "", fmt.Errorf("unknown social provider %q", provider)
	}

	resp, err := s.Provision(ctx, identity)
	if err != nil {
		return nil, "", fmt.Errorf("provision: %w", err)
	}
	return resp, redirectURI, nil
}

// claimsToIdentity maps verified OIDC id_token claims into a provider-agnostic
// Identity. Per-provider EmailVerified rules are SECURITY-CRITICAL:
//   - google: trust the email_verified claim (bool or stringified "true").
//   - microsoft: /common has NO email_verified claim → EmailVerified=false so
//     Provision never auto-links to an existing account (Pitfall 7).
func claimsToIdentity(provider string, claims map[string]any) Identity {
	id := Identity{
		Provider:    provider,
		ProviderSub: claimString(claims, "sub"),
		Email:       strings.ToLower(strings.TrimSpace(claimString(claims, "email"))),
		DisplayName: claimString(claims, "name"),
		AvatarURL:   claimString(claims, "picture"),
	}
	if provider == "google" {
		id.EmailVerified = claimBool(claims, "email_verified")
	}
	// microsoft (and any other provider here): EmailVerified stays false.
	return id
}

// claimString reads a string claim, tolerating absent keys.
func claimString(claims map[string]any, key string) string {
	if v, ok := claims[key].(string); ok {
		return v
	}
	return ""
}

// claimBool reads a boolean claim that providers may encode as a real bool or as
// the string "true" (some IdPs stringify JWT booleans).
func claimBool(claims map[string]any, key string) bool {
	switch v := claims[key].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true")
	default:
		return false
	}
}

// randomNonce returns a 32-hex-char random nonce for OIDC replay protection.
func randomNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
