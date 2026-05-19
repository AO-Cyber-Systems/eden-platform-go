package oidcrp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// State is the caller-bound payload of a signed OIDC state parameter. Tenant,
// Idp, and Nonce are REQUIRED — Verify rejects payloads that decode with any
// of these empty (defense against zero-value attacks where an attacker
// constructs a payload with empty tenant binding).
//
// ReturnURL is optional caller-side metadata; it is verified by signature
// but not parsed as a URL — callers MUST allowlist before issuing the
// 302 to prevent open-redirect.
//
// CreatedAt is unix seconds; VerifyState enforces (now - CreatedAt) <= maxAge.
type State struct {
	Tenant    string `json:"tenant"`
	Idp       string `json:"idp"`
	ReturnURL string `json:"return_url,omitempty"`
	Nonce     string `json:"nonce"`
	CreatedAt int64  `json:"created_at"`
}

// SignState produces a `base64url(json).base64url(hmac-sha256)` token. The
// MAC covers the base64url-encoded JSON payload (not the raw JSON) so
// verifiers don't need to re-canonicalize before checking.
func SignState(key []byte, s State) string {
	payload, _ := json.Marshal(s)
	encPayload := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(encPayload))
	encMAC := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encPayload + "." + encMAC
}

// VerifyState validates a SignState output: HMAC compare in constant time,
// decode JSON, then enforce business invariants (non-empty Tenant/Idp/Nonce
// and CreatedAt within maxAge of now).
//
// Returns:
//   - ErrStateInvalid for any structural / MAC / JSON / missing-field error.
//   - ErrStateExpired when the payload is well-formed but older than maxAge.
func VerifyState(key []byte, raw string, maxAge time.Duration) (State, error) {
	var zero State
	parts := strings.SplitN(raw, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return zero, fmt.Errorf("%w: malformed", ErrStateInvalid)
	}
	encPayload, encMAC := parts[0], parts[1]

	gotMAC, err := base64.RawURLEncoding.DecodeString(encMAC)
	if err != nil {
		return zero, fmt.Errorf("%w: bad mac encoding: %v", ErrStateInvalid, err)
	}
	expected := hmac.New(sha256.New, key)
	expected.Write([]byte(encPayload))
	if !hmac.Equal(gotMAC, expected.Sum(nil)) {
		return zero, fmt.Errorf("%w: mac mismatch", ErrStateInvalid)
	}

	payload, err := base64.RawURLEncoding.DecodeString(encPayload)
	if err != nil {
		return zero, fmt.Errorf("%w: bad payload encoding: %v", ErrStateInvalid, err)
	}
	var s State
	if err := json.Unmarshal(payload, &s); err != nil {
		return zero, fmt.Errorf("%w: bad payload json: %v", ErrStateInvalid, err)
	}
	if s.Tenant == "" || s.Idp == "" || s.Nonce == "" {
		return zero, fmt.Errorf("%w: missing required field (tenant/idp/nonce)", ErrStateInvalid)
	}
	if maxAge > 0 {
		now := time.Now().Unix()
		if now-s.CreatedAt > int64(maxAge.Seconds()) {
			return zero, fmt.Errorf("%w: created_at=%d age=%ds maxAge=%s", ErrStateExpired, s.CreatedAt, now-s.CreatedAt, maxAge)
		}
	}
	return s, nil
}
