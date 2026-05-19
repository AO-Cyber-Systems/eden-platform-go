package oidcrp

import (
	"context"
	"fmt"

	oidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// minPKCEVerifierLen is the RFC 7636 §4.1 lower bound on the code_verifier
// length. oauth2.GenerateVerifier produces 43 bytes (256 bits of entropy
// base64url-no-pad encoded), which is the minimum permitted value.
const minPKCEVerifierLen = 43

// BuildAuthURL composes the OIDC authorization-code request URL with PKCE
// (S256) and nonce mandatorily attached. PKCE is REQUIRED per RFC 9700
// (OAuth 2.0 Security BCP) and OAuth 2.1; this helper refuses to skip it.
//
// Inputs:
//   - state: opaque, caller-managed CSRF/binding token (typically a signed
//     state from state.go).
//   - nonce: opaque, caller-managed replay token; MUST match the nonce
//     claim returned in the ID token (enforced by ExchangeAndVerify).
//   - pkceVerifier: the secret half of the PKCE pair, ≥ 43 chars (see
//     oauth2.GenerateVerifier). The corresponding S256 challenge is
//     transmitted in the auth URL; the verifier itself is sent later at
//     /token via oauth2.VerifierOption.
//   - extra: caller-supplied AuthCodeOptions appended after the nonce and
//     PKCE options. Useful for prompt=login, login_hint, id_token_hint,
//     etc.
//
// Returns the absolute authorization URL ready for HTTP 302 redirect.
func BuildAuthURL(cfg *oauth2.Config, state, nonce, pkceVerifier string, extra []oauth2.AuthCodeOption) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("oidcrp: BuildAuthURL: nil cfg")
	}
	if state == "" {
		return "", fmt.Errorf("oidcrp: BuildAuthURL: empty state")
	}
	if nonce == "" {
		return "", fmt.Errorf("oidcrp: BuildAuthURL: empty nonce")
	}
	if len(pkceVerifier) < minPKCEVerifierLen {
		return "", fmt.Errorf("oidcrp: BuildAuthURL: pkceVerifier length %d < %d (RFC 7636 §4.1)", len(pkceVerifier), minPKCEVerifierLen)
	}

	opts := make([]oauth2.AuthCodeOption, 0, 2+len(extra))
	opts = append(opts, oidc.Nonce(nonce))
	opts = append(opts, oauth2.S256ChallengeOption(pkceVerifier))
	opts = append(opts, extra...)

	return cfg.AuthCodeURL(state, opts...), nil
}

// ExchangeAndVerify completes the authorization-code half of an OIDC RP
// flow: exchange code for token, pull id_token off the token response,
// verify signature + issuer + audience + expiry via the supplied verifier,
// and enforce nonce binding against the storedNonce.
//
// Returns:
//   - idt: the verified *oidc.IDToken (header + claims access).
//   - tok: the raw *oauth2.Token (access_token, refresh_token, expiry).
//   - claims: id_token claims unmarshalled into a generic map for caller-
//     side mapping via ApplyClaimMap or direct field access.
//   - err: ErrNonceMismatch on nonce mismatch, ErrMissingIDToken on absent
//     id_token, otherwise the underlying oauth2 / go-oidc error wrapped
//     with %w for caller inspection via errors.Is/As.
//
// Pre-requisites:
//   - cfg must have ClientID + ClientSecret (or use private_key_jwt via a
//     custom http.Client on ctx) and TokenURL set.
//   - verifier MUST be the one cached by VerifierCache for this (tenant,
//     idp) pair — SkipIssuerCheck and SkipClientIDCheck MUST be false.
//   - pkceVerifier MUST be the original code_verifier the caller passed to
//     BuildAuthURL (typically retrieved from the InFlightStore by nonce).
//   - storedNonce is the nonce the caller used when calling BuildAuthURL.
func ExchangeAndVerify(
	ctx context.Context,
	cfg *oauth2.Config,
	verifier *oidc.IDTokenVerifier,
	code, pkceVerifier, storedNonce string,
) (*oidc.IDToken, *oauth2.Token, map[string]any, error) {
	if cfg == nil {
		return nil, nil, nil, fmt.Errorf("oidcrp: ExchangeAndVerify: nil cfg")
	}
	tok, err := cfg.Exchange(ctx, code, oauth2.VerifierOption(pkceVerifier))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("oidcrp: token exchange: %w", err)
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok || rawID == "" {
		return nil, tok, nil, ErrMissingIDToken
	}
	if verifier == nil {
		return nil, tok, nil, fmt.Errorf("oidcrp: ExchangeAndVerify: nil verifier")
	}
	idt, err := verifier.Verify(ctx, rawID)
	if err != nil {
		return nil, tok, nil, fmt.Errorf("oidcrp: verify id_token: %w", err)
	}
	if idt.Nonce != storedNonce {
		return nil, tok, nil, ErrNonceMismatch
	}
	var claims map[string]any
	if err := idt.Claims(&claims); err != nil {
		return idt, tok, nil, fmt.Errorf("oidcrp: decode id_token claims: %w", err)
	}
	return idt, tok, claims, nil
}
