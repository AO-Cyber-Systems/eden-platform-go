package oidcrp

import "errors"

// Exported sentinel errors. Callers branch on these via errors.Is.
//
// ErrNonceMismatch: ID token's nonce claim does not match the storedNonce
// passed to ExchangeAndVerify. Indicates an OIDC replay attack OR a state
// mix-up between concurrent in-flight flows. Always treat as fatal — do
// NOT issue a session.
//
// ErrIssuerMismatch: Reserved sentinel. go-oidc's Verify rejects mismatched
// issuers internally and returns its own error, but we expose this sentinel
// so callers writing higher-level switches can branch on it consistently.
//
// ErrMissingIDToken: token endpoint returned a token response without an
// id_token field. Either the OP isn't configured for OIDC (only OAuth 2.0)
// or the requested scope didn't include "openid".
//
// ErrInFlightNotFound / ErrInFlightExpired: see in_flight.go.
//
// ErrMissingRequiredClaim: ApplyClaimMap could not resolve a REQUIRED
// claim (email or sub). The error message names the missing field.
var (
	ErrNonceMismatch        = errors.New("oidcrp: nonce mismatch")
	ErrIssuerMismatch       = errors.New("oidcrp: issuer mismatch")
	ErrMissingIDToken       = errors.New("oidcrp: missing id_token in token response")
	ErrInFlightNotFound     = errors.New("oidcrp: in-flight record not found")
	ErrInFlightExpired      = errors.New("oidcrp: in-flight record expired")
	ErrMissingRequiredClaim = errors.New("oidcrp: required claim missing")
	ErrStateInvalid         = errors.New("oidcrp: signed state invalid")
	ErrStateExpired         = errors.New("oidcrp: signed state expired")
)
