package encryption

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
)

// KeyFromHex parses a 32-byte AES key encoded as hex (case-insensitive,
// 64 hex characters). Returns an error on invalid encoding or wrong length.
func KeyFromHex(s string) ([]byte, error) {
	key, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("encryption: KeyFromHex: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("encryption: KeyFromHex: key length = %d, want 32", len(key))
	}
	return key, nil
}

// KeyFromBase64 parses a 32-byte AES key encoded as standard or URL-safe
// base64 (with or without padding). Returns an error on invalid encoding or
// wrong length.
func KeyFromBase64(s string) ([]byte, error) {
	// Try the most-permissive base64 decoders in order.
	decoders := []*base64.Encoding{
		base64.StdEncoding,
		base64.URLEncoding,
		base64.RawStdEncoding,
		base64.RawURLEncoding,
	}
	var lastErr error
	for _, dec := range decoders {
		key, err := dec.DecodeString(s)
		if err == nil {
			if len(key) != 32 {
				return nil, fmt.Errorf("encryption: KeyFromBase64: key length = %d, want 32", len(key))
			}
			return key, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("encryption: KeyFromBase64: %w", lastErr)
}

// KeyFromEnv reads the env var named `name` and parses its value as either
// hex or base64 — whichever decodes to 32 bytes. Empty env value returns an
// error so callers can fail fast at boot.
//
// Detection rule: if the value is exactly 64 characters of [0-9a-fA-F], it is
// treated as hex; otherwise it is treated as base64. Hex falls back to
// base64 on parse failure (and vice versa) so misclassification is recovered
// transparently.
func KeyFromEnv(name string) ([]byte, error) {
	v := os.Getenv(name)
	if v == "" {
		return nil, fmt.Errorf("encryption: KeyFromEnv: %s not set", name)
	}
	if looksLikeHex(v) {
		if key, err := KeyFromHex(v); err == nil {
			return key, nil
		}
	}
	if key, err := KeyFromBase64(v); err == nil {
		return key, nil
	}
	// Last resort: try hex even if it didn't look hex.
	if key, err := KeyFromHex(v); err == nil {
		return key, nil
	}
	return nil, fmt.Errorf("encryption: KeyFromEnv: %s is not a valid 32-byte hex or base64 key", name)
}

func looksLikeHex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		case c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}
