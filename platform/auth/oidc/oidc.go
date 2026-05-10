// Package oidc provides standalone OIDC Service Provider primitives for use
// in custom flows. The platform's higher-level auth.SSOService also performs
// OIDC against tenant-configured providers; this package exposes the
// underlying building blocks for callers that need direct control (e.g.
// federated apps embedding their own state machinery).
//
// IdP-side OIDC issuance is intentionally not in this package — that lives
// in platform/auth.JWTManager (post-quantum signed JWTs) and the SSOService
// wrappers.
//
// Promoted from aodex-go/internal/auth/oidc.go.
package oidc

import (
	"context"
	"fmt"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// Config holds the configuration for an OIDC Service Provider, mapped from
// an sso_configurations row.
type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURI  string
	Scopes       []string
}

// Tokens holds the tokens returned from an OIDC code exchange.
type Tokens struct {
	IDToken     string
	AccessToken string
}

// DefaultScopes returns the standard OIDC scopes (openid, email, profile).
func DefaultScopes() []string {
	return []string{gooidc.ScopeOpenID, "email", "profile"}
}

// BuildAuthURL constructs the authorization URL for OIDC login. It discovers
// the provider's endpoints, then builds the auth URL with the given state
// and nonce parameters.
//
// The state parameter prevents CSRF attacks. The nonce parameter prevents
// token replay attacks.
func BuildAuthURL(ctx context.Context, cfg Config, state, nonce string) (string, error) {
	provider, err := gooidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return "", fmt.Errorf("discovering OIDC provider: %w", err)
	}

	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = DefaultScopes()
	}

	oauth2Cfg := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURI,
		Endpoint:     provider.Endpoint(),
		Scopes:       scopes,
	}

	return oauth2Cfg.AuthCodeURL(state, gooidc.Nonce(nonce)), nil
}

// ExchangeCode exchanges an authorization code for tokens. Returns the ID
// token and access token from the token response.
func ExchangeCode(ctx context.Context, cfg Config, code string) (*Tokens, error) {
	provider, err := gooidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("discovering OIDC provider: %w", err)
	}

	oauth2Cfg := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURI,
		Endpoint:     provider.Endpoint(),
	}

	token, err := oauth2Cfg.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("exchanging code for tokens: %w", err)
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("no id_token in token response")
	}

	return &Tokens{
		IDToken:     rawIDToken,
		AccessToken: token.AccessToken,
	}, nil
}

// VerifyIDToken verifies and decodes an OIDC ID token using the provider's
// JWKS endpoint (key rotation handled automatically).
func VerifyIDToken(ctx context.Context, cfg Config, rawIDToken string) (map[string]any, error) {
	provider, err := gooidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("discovering OIDC provider: %w", err)
	}

	verifier := provider.Verifier(&gooidc.Config{ClientID: cfg.ClientID})

	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("verifying ID token: %w", err)
	}

	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("extracting claims: %w", err)
	}

	return claims, nil
}

// BuildAuthURLStatic builds the authorization URL without OIDC discovery.
// Useful for testing or when the authorization endpoint is known and
// discovery is undesirable.
func BuildAuthURLStatic(authEndpoint string, cfg Config, state, nonce string) string {
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = DefaultScopes()
	}
	oauth2Cfg := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURI,
		Endpoint:     oauth2.Endpoint{AuthURL: authEndpoint},
		Scopes:       scopes,
	}
	return oauth2Cfg.AuthCodeURL(state, gooidc.Nonce(nonce))
}
