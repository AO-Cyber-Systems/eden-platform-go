package logingov

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"
)

// verifyJWTRS256 validates an RS256-signed JWT against the given RSA
// public key. Returns nil on valid signature, otherwise an error.
//
// This intentionally re-implements verification using the stdlib so the
// tests don't depend on the same library that produced the signature.
// (If a future bug accidentally produces a "good-looking" JWT that the
// hashicorp/cap library can self-verify but a different verifier
// rejects, this catches it.)
func verifyJWTRS256(jwt string, pub *rsa.PublicKey) error {
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		return fmt.Errorf("jwt: want 3 segments, got %d", len(parts))
	}
	signed := parts[0] + "." + parts[1]
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return fmt.Errorf("jwt: decode signature: %w", err)
	}
	h := sha256.Sum256([]byte(signed))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, h[:], sig); err != nil {
		return fmt.Errorf("jwt: verify PKCS1v15: %w", err)
	}
	return nil
}
