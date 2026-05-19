package jwks

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
)

// RFC7638Thumbprint computes the JWK thumbprint per RFC 7638 §3.1: SHA-256
// over the canonical JSON of the REQUIRED JWK members in lexicographic
// (codepoint-ascending) field order, encoded as base64url-no-pad.
//
// Required members per RFC 7638 §3.2:
//
//   EC : crv, kty, x, y       (lexicographic order)
//   RSA: e, kty, n            (lexicographic order)
//
// Only these members participate in the thumbprint — kid, alg, use, and any
// optional members are ignored. So mutating kid/alg/use on the same key
// material yields the same thumbprint (asserted by tests).
//
// The canonical JSON is HAND-BUILT here, not produced by encoding/json,
// because Go's json package orders struct fields in declaration order
// (not lexicographically) and RFC 7638 §3.3 mandates lexicographic order in
// the input to SHA-256. fmt.Sprintf %q produces the same string escaping as
// JSON for the subset of values that appear here (alphanumeric + "_-" in
// base64url-no-pad plus the fixed lowercase ASCII strings "EC", "P-256",
// "RSA"), and the RSA example vector test pins this exactly.
func RFC7638Thumbprint(j JWK) (string, error) {
	var canonical string
	switch j.Kty {
	case "EC":
		if j.Crv == "" {
			return "", errors.New("jwks: EC thumbprint requires crv")
		}
		if j.X == "" {
			return "", errors.New("jwks: EC thumbprint requires x")
		}
		if j.Y == "" {
			return "", errors.New("jwks: EC thumbprint requires y")
		}
		// Lexicographic order: crv, kty, x, y.
		canonical = fmt.Sprintf(`{"crv":%q,"kty":"EC","x":%q,"y":%q}`, j.Crv, j.X, j.Y)
	case "RSA":
		if j.N == "" {
			return "", errors.New("jwks: RSA thumbprint requires n")
		}
		if j.E == "" {
			return "", errors.New("jwks: RSA thumbprint requires e")
		}
		// Lexicographic order: e, kty, n.
		canonical = fmt.Sprintf(`{"e":%q,"kty":"RSA","n":%q}`, j.E, j.N)
	default:
		return "", fmt.Errorf("jwks: unsupported kty %q for thumbprint (want EC or RSA)", j.Kty)
	}
	h := sha256.Sum256([]byte(canonical))
	return base64.RawURLEncoding.EncodeToString(h[:]), nil
}
